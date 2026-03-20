package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/muninndb"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type captureTestProvider struct {
	response string
	lastMsgs []providers.Message
}

func (p *captureTestProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	p.lastMsgs = append([]providers.Message(nil), messages...)
	return &providers.LLMResponse{Content: p.response}, nil
}

func (p *captureTestProvider) GetDefaultModel() string { return "capture-test-model" }

func TestProcessDirectWithChannel_MuninnAutoCaptureWritesDurablePreference(t *testing.T) {
	tmpDir := t.TempDir()
	activateResponses := []muninndb.ActivateResponse{
		{QueryID: "recall-1", TotalFound: 0, Activations: []muninndb.ActivationItem{}},
		{QueryID: "dedupe-1", TotalFound: 0, Activations: []muninndb.ActivationItem{}},
	}
	writeCh := make(chan muninndb.WriteRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activate":
			defer r.Body.Close()
			var req muninndb.ActivateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode activate request: %v", err)
			}
			if len(activateResponses) == 0 {
				t.Fatalf("unexpected extra activate call: %+v", req)
			}
			resp := activateResponses[0]
			activateResponses = activateResponses[1:]
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/engrams":
			defer r.Body.Close()
			var req muninndb.WriteRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode write request: %v", err)
			}
			writeCh <- req
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"eng-1","created_at":123}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := &captureTestProvider{response: "Understood."}
	al := NewAgentLoop(testMuninnConfig(tmpDir, server.URL), bus.NewMessageBus(), provider)

	response, err := al.ProcessDirectWithChannel(
		context.Background(),
		"I prefer dark mode for every project we work on.",
		"session-capture-1",
		"claweb",
		"room-1",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel() error = %v", err)
	}
	if response != "Understood." {
		t.Fatalf("response = %q", response)
	}

	select {
	case req := <-writeCh:
		if req.Vault != config.DefaultMemoryVault {
			t.Fatalf("write vault = %q, want %q", req.Vault, config.DefaultMemoryVault)
		}
		if req.Concept != "user_preference" {
			t.Fatalf("write concept = %q", req.Concept)
		}
		if req.TypeLabel != "preference" {
			t.Fatalf("write type_label = %q", req.TypeLabel)
		}
		if req.Confidence <= 0 || req.Stability <= 0 {
			t.Fatalf("expected confidence/stability, got %+v", req)
		}
		if !strings.Contains(strings.ToLower(req.Content), "prefer dark mode") {
			t.Fatalf("write content = %q", req.Content)
		}
		if !containsString(req.Tags, "auto-capture") || !containsString(req.Tags, "preference") {
			t.Fatalf("write tags = %v", req.Tags)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for capture write")
	}
	if len(activateResponses) != 0 {
		t.Fatalf("unused activate responses: %d", len(activateResponses))
	}
}

func TestProcessDirectWithChannel_MuninnAutoCaptureFailureDoesNotBlockReply(t *testing.T) {
	tmpDir := t.TempDir()
	activateCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activate":
			activateCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"query_id":"q","total_found":0,"activations":[]}`))
		case "/api/engrams":
			http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := &captureTestProvider{response: "Reply still delivered."}
	al := NewAgentLoop(testMuninnConfig(tmpDir, server.URL), bus.NewMessageBus(), provider)

	response, err := al.ProcessDirectWithChannel(
		context.Background(),
		"Please only use pnpm in this repository from now on.",
		"session-capture-2",
		"claweb",
		"room-2",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel() error = %v", err)
	}
	if response != "Reply still delivered." {
		t.Fatalf("response = %q", response)
	}
	if activateCalls < 1 {
		t.Fatalf("expected at least the recall activate call, got %d", activateCalls)
	}
}

func TestExtractMuninnAutoCaptureCandidate_ClassifiesStableSemantics(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		concept   string
		typeLabel string
	}{
		{
			name:      "preference",
			message:   "I prefer concise commit messages for this repo.",
			concept:   "user_preference",
			typeLabel: "preference",
		},
		{
			name:      "constraint",
			message:   "Please do not use yarn in this repository from now on.",
			concept:   "explicit_constraint",
			typeLabel: "constraint",
		},
		{
			name:      "decision",
			message:   "We decided to standardize on SQLite for local development.",
			concept:   "project_decision",
			typeLabel: "decision",
		},
		{
			name:      "full sentence decision",
			message:   "For release builds, the project will use SQLite as the local metadata store.",
			concept:   "project_decision",
			typeLabel: "decision",
		},
		{
			name:      "full sentence constraint",
			message:   "During validation runs, please only use claweb and avoid every other channel.",
			concept:   "explicit_constraint",
			typeLabel: "constraint",
		},
		{
			name:      "explicit preference statement",
			message:   "My preferred code review style is concise summaries with bullet points.",
			concept:   "user_preference",
			typeLabel: "preference",
		},
		{
			name:      "contact",
			message:   "Call me Alex in project coordination messages going forward.",
			concept:   "contact_mapping",
			typeLabel: "contact_mapping",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate, reason := extractMuninnAutoCaptureCandidate(
				processOptions{
					UserMessage: tt.message,
					Channel:     "claweb",
					SenderID:    "user-1",
					SessionKey:  "s1",
				},
			)
			if reason != "" {
				t.Fatalf("unexpected drop reason: %s", reason)
			}
			if candidate == nil {
				t.Fatal("candidate = nil")
			}
			if candidate.Concept != tt.concept || candidate.TypeLabel != tt.typeLabel {
				t.Fatalf("candidate = %+v", candidate)
			}
			if candidate.Summary == "" || len(candidate.Tags) == 0 {
				t.Fatalf("expected summary/tags, got %+v", candidate)
			}
		})
	}
}

func TestExtractMuninnAutoCaptureCandidate_DropsLowConfidenceNoise(t *testing.T) {
	t.Run("short noisy utterance", func(t *testing.T) {
		candidate, reason := extractMuninnAutoCaptureCandidate(
			processOptions{UserMessage: "maybe later", Channel: "claweb"},
		)
		if candidate != nil {
			t.Fatalf("expected nil candidate, got %+v", candidate)
		}
		if reason == "" {
			t.Fatal("expected drop reason")
		}
	})

	t.Run("ambiguous preference phrasing", func(t *testing.T) {
		candidate, reason := extractMuninnAutoCaptureCandidate(
			processOptions{UserMessage: "I maybe prefer dark mode for now.", Channel: "claweb"},
		)
		if candidate != nil {
			t.Fatalf("expected nil candidate, got %+v", candidate)
		}
		if reason == "" {
			t.Fatalf("reason = %q", reason)
		}
	})
}

func TestMuninnCaptureLooksDuplicate_FuzzyMatchPreventsNearDuplicates(t *testing.T) {
	candidate := muninnAutoCaptureCandidate{
		Concept: "user_preference",
		Content: "I prefer dark mode for every project we work on.",
	}
	item := muninndb.ActivationItem{
		Concept: "user_preference",
		Content: "I prefer dark mode for every project that we work on.",
		Score:   0.95,
	}
	if !muninnCaptureLooksDuplicate(item, candidate) {
		t.Fatal("expected fuzzy duplicate match")
	}
}

func TestBuildMuninnCaptureDedupeQuery_UsesStableFactFingerprint(t *testing.T) {
	candidate := muninnAutoCaptureCandidate{
		Concept:     "explicit_constraint",
		Content:     "Please only use claweb for validation runs.",
		Fingerprint: "explicit_constraint|please only use claweb for validation runs.",
	}
	query := buildMuninnCaptureDedupeQuery(candidate)
	if !strings.Contains(query, "explicit_constraint|please only use claweb for validation runs.") {
		t.Fatalf("query = %q", query)
	}
	if !strings.Contains(query, "please only use claweb for validation runs.") {
		t.Fatalf("query = %q", query)
	}
}

func TestProcessDirectWithChannel_MuninnAutoCaptureSkipsDuplicateWrite(t *testing.T) {
	tmpDir := t.TempDir()
	activateResponses := []muninndb.ActivateResponse{
		{QueryID: "recall-1", TotalFound: 0, Activations: []muninndb.ActivationItem{}},
		{QueryID: "dedupe-1", TotalFound: 1, Activations: []muninndb.ActivationItem{{
			Concept: "user_preference",
			Content: "I prefer dark mode for every project that we work on.",
			Score:   0.88,
		}}},
	}
	writeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activate":
			defer r.Body.Close()
			if len(activateResponses) == 0 {
				t.Fatalf("unexpected extra activate call")
			}
			resp := activateResponses[0]
			activateResponses = activateResponses[1:]
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/engrams":
			writeCalls++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"eng-should-not-write","created_at":789}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := &captureTestProvider{response: "Noted."}
	al := NewAgentLoop(testMuninnConfig(tmpDir, server.URL), bus.NewMessageBus(), provider)

	response, err := al.ProcessDirectWithChannel(
		context.Background(),
		"I prefer dark mode for every project we work on.",
		"session-capture-dedupe",
		"claweb",
		"room-dedupe",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel() error = %v", err)
	}
	if response != "Noted." {
		t.Fatalf("response = %q", response)
	}

	time.Sleep(200 * time.Millisecond)
	if writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0", writeCalls)
	}
	if len(activateResponses) != 0 {
		t.Fatalf("unused activate responses: %d", len(activateResponses))
	}
}

func TestMuninnClientWriteEngramRequest_PreservesStructuredFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/engrams" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		defer r.Body.Close()
		var req muninndb.WriteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Vault != "structured-vault" || req.TypeLabel != "constraint" ||
			req.Summary != "summary" ||
			req.Confidence != 0.9 ||
			req.Stability != 0.8 {
			t.Fatalf("unexpected request: %+v", req)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"eng-structured","created_at":456}`))
	}))
	defer server.Close()

	client := muninndb.NewClientWithHTTPClient(
		server.Client(),
		server.URL,
		"structured-vault",
		"secret",
	)
	resp, err := client.WriteEngramRequest(
		context.Background(),
		muninndb.WriteRequest{
			Concept:    "explicit_constraint",
			Content:    "Use pnpm.",
			TypeLabel:  "constraint",
			Summary:    "summary",
			Confidence: 0.9,
			Stability:  0.8,
		},
	)
	if err != nil {
		t.Fatalf("WriteEngramRequest() error = %v", err)
	}
	if resp == nil || resp.ID != "eng-structured" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func testMuninnConfig(workspace, restURL string) *config.Config {
	return &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         workspace,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 5,
			},
		},
		Memory: config.MemoryConfig{
			Provider: config.MemoryProviderMuninnDB,
			MuninnDB: &config.MuninnDBConfig{
				MCPEndpoint:  "http://127.0.0.1:8750",
				RESTEndpoint: restURL,
				Vault:        config.DefaultMemoryVault,
				Timeout:      "2s",
			},
		},
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
