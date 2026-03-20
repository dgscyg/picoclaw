package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/muninndb"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type muninnAwareProvider struct {
	lastMessages []providers.Message
}

func (p *muninnAwareProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	p.lastMessages = append([]providers.Message(nil), messages...)
	systemPrompt := ""
	if len(messages) > 0 {
		systemPrompt = strings.ToLower(messages[0].Content)
	}
	response := "I don't know your favorite editor."
	if strings.Contains(systemPrompt, strings.ToLower(MuninnAutoRecallBlockTitle)) &&
		strings.Contains(systemPrompt, "helix") {
		response = "Your favorite editor is helix."
	}
	return &providers.LLMResponse{Content: response}, nil
}

func (p *muninnAwareProvider) GetDefaultModel() string {
	return "mock-muninn-model"
}

func TestProcessDirectWithChannel_MuninnAutoRecallInjectsRelevantMemoryWithoutPersistingHistory(t *testing.T) {
	tmpDir := t.TempDir()

	var activateReq muninndb.ActivateRequest
	activateCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activate":
			activateCalls++
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&activateReq); err != nil {
				t.Fatalf("decode activate request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(muninndb.ActivateResponse{
				QueryID:    "query-1",
				TotalFound: 1,
				Activations: []muninndb.ActivationItem{{
					ID:      "eng-1",
					Concept: "preference",
					Content: "Favorite editor: helix. The user explicitly asked to keep using helix for edits.",
					Score:   0.98,
				}},
			})
		case "/api/engrams":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"eng-write","created_at":123}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 5,
			},
		},
		Memory: config.MemoryConfig{
			Provider: config.MemoryProviderMuninnDB,
			MuninnDB: &config.MuninnDBConfig{
				MCPEndpoint:  "http://127.0.0.1:8750",
				RESTEndpoint: server.URL,
				Vault:        MuninnValidationVault,
				Timeout:      "2s",
			},
		},
	}

	provider := &muninnAwareProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)

	response, err := al.ProcessDirectWithChannel(
		context.Background(),
		"What's my favorite editor? Reply with exactly one sentence.",
		"session-1",
		"claweb",
		"room-1",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel() error = %v", err)
	}
	if response != "Your favorite editor is helix." {
		t.Fatalf("response = %q", response)
	}
	if activateReq.Vault != MuninnValidationVault {
		t.Fatalf("activate vault = %q, want %q", activateReq.Vault, MuninnValidationVault)
	}
	if activateReq.MaxResults != muninnAutoRecallMaxItems {
		t.Fatalf("activate max_results = %d, want %d", activateReq.MaxResults, muninnAutoRecallMaxItems)
	}
	if activateCalls == 0 {
		t.Fatal("expected at least one activate call")
	}
	if len(activateReq.Context) == 0 || !strings.Contains(
		strings.ToLower(strings.Join(activateReq.Context, "\n")),
		"favorite editor",
	) {
		t.Fatalf("activate context = %#v, want query mention", activateReq.Context)
	}
	if len(provider.lastMessages) == 0 {
		t.Fatal("provider did not receive any messages")
	}
	systemPrompt := provider.lastMessages[0].Content
	if !strings.Contains(systemPrompt, MuninnAutoRecallBlockTitle) {
		t.Fatalf("system prompt missing auto recall block:\n%s", systemPrompt)
	}
	if !strings.Contains(strings.ToLower(systemPrompt), "helix") {
		t.Fatalf("system prompt missing recalled fact:\n%s", systemPrompt)
	}

	history := al.registry.GetDefaultAgent().Sessions.GetHistory("session-1")
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	for _, msg := range history {
		if strings.Contains(msg.Content, MuninnAutoRecallBlockTitle) {
			t.Fatalf("history leaked auto recall block: %+v", msg)
		}
	}

	err = filepath.WalkDir(filepath.Join(tmpDir, "sessions"), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), MuninnAutoRecallBlockTitle) {
			t.Fatalf("persisted session leaked auto recall block in %s: %s", path, string(data))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(sessions) error = %v", err)
	}
}

func TestProcessDirectWithChannel_MuninnAutoRecallReusesCacheForCloselyRelatedTurns(t *testing.T) {
	tmpDir := t.TempDir()
	activateCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activate":
			activateCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(muninndb.ActivateResponse{
				QueryID:    "query-cache-1",
				TotalFound: 1,
				Activations: []muninndb.ActivationItem{{
					ID:      "eng-cache-1",
					Concept: "preference",
					Content: "Favorite editor: helix.",
					Score:   0.98,
				}},
			})
		case "/api/engrams":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"eng-write","created_at":123}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	provider := &muninnAwareProvider{}
	al := NewAgentLoop(testMuninnConfig(tmpDir, server.URL), bus.NewMessageBus(), provider)

	response1, err := al.ProcessDirectWithChannel(
		context.Background(),
		"What's my favorite editor?",
		"session-cache",
		"claweb",
		"room-1",
	)
	if err != nil {
		t.Fatalf("first ProcessDirectWithChannel() error = %v", err)
	}
	response2, err := al.ProcessDirectWithChannel(
		context.Background(),
		"Remind me again which editor I prefer for code edits.",
		"session-cache",
		"claweb",
		"room-1",
	)
	if err != nil {
		t.Fatalf("second ProcessDirectWithChannel() error = %v", err)
	}
	if response1 != "Your favorite editor is helix." || response2 != "Your favorite editor is helix." {
		t.Fatalf("unexpected responses: %q / %q", response1, response2)
	}
	if activateCalls != 2 {
		t.Fatalf("activateCalls = %d, want 2", activateCalls)
	}
	plan, ok := al.buildMuninnAutoRecallPlan(processOptions{
		SessionKey:  "session-cache",
		Channel:     "claweb",
		ChatID:      "room-1",
		SenderID:    "cron",
		UserMessage: "Remind me again which editor I prefer for code edits.",
	})
	if !ok {
		t.Fatal("expected test follow-up plan to be created")
	}
	if _, _, ok := al.lookupMuninnRecallCache(plan); !ok {
		t.Fatal("expected closely related follow-up query to be cached after refresh")
	}
}

func TestProcessDirectWithChannel_MuninnAutoRecallRefreshesCacheOnMaterialContextChange(t *testing.T) {
	tmpDir := t.TempDir()
	activateCalls := 0
	queries := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activate":
			activateCalls++
			defer r.Body.Close()
			var req muninndb.ActivateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode activate request: %v", err)
			}
			queries = append(queries, strings.Join(req.Context, "\n"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(muninndb.ActivateResponse{
				QueryID:    "query-refresh",
				TotalFound: 1,
				Activations: []muninndb.ActivationItem{{
					ID:      "eng-cache-2",
					Concept: "preference",
					Content: "Favorite editor: helix.",
					Score:   0.98,
				}},
			})
		case "/api/engrams":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"eng-write","created_at":123}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	provider := &muninnAwareProvider{}
	al := NewAgentLoop(testMuninnConfig(tmpDir, server.URL), bus.NewMessageBus(), provider)

	if _, err := al.ProcessDirectWithChannel(context.Background(), "What's my favorite editor?", "session-cache-refresh", "claweb", "room-1"); err != nil {
		t.Fatalf("first ProcessDirectWithChannel() error = %v", err)
	}
	if _, err := al.ProcessDirectWithChannel(context.Background(), "What's my deployment region?", "session-cache-refresh", "claweb", "room-1"); err != nil {
		t.Fatalf("second ProcessDirectWithChannel() error = %v", err)
	}
	if activateCalls != 2 {
		t.Fatalf("activateCalls = %d, want 2", activateCalls)
	}
	if len(queries) != 2 {
		t.Fatalf("queries = %d, want 2", len(queries))
	}
	if queries[0] == queries[1] {
		t.Fatalf("expected refreshed activate query, got identical queries: %q", queries[0])
	}
}

func TestFormatMuninnAutoRecallPromptBlock_BoundsAndSummarizesMemories(t *testing.T) {
	marker := "SHOULD_NOT_APPEAR_AFTER_TRUNCATION"
	items := []MuninnProxyMemoryItem{
		{
			Concept: "first",
			Content: "First memory explains the stable preference in detail. " +
				strings.Repeat("alpha ", 40),
			Why: "Because the user confirmed it last week.",
		},
		{
			Concept: "second",
			Content: "Second memory keeps the routing alias on file. " +
				strings.Repeat("beta ", 40),
			Why: "Needed for direct replies.",
		},
		{
			Concept: "third",
			Content: "Third memory stores the project constraint. " +
				strings.Repeat("gamma ", 40),
			Why: "Required for validation.",
		},
		{
			Concept: "fourth",
			Content: "Fourth memory should be omitted entirely. " + marker,
			Why:     "Too many items.",
		},
	}

	block := formatMuninnAutoRecallPromptBlock(items)
	if !block.Enabled() {
		t.Fatal("prompt block should be enabled")
	}
	if block.Title != MuninnAutoRecallBlockTitle {
		t.Fatalf("block title = %q, want %q", block.Title, MuninnAutoRecallBlockTitle)
	}
	if block.Persist {
		t.Fatal("prompt block should not be persistent")
	}
	if got := strings.Count(block.Body, "- "); got != muninnAutoRecallMaxItems {
		t.Fatalf("bullet count = %d, want %d\n%s", got, muninnAutoRecallMaxItems, block.Body)
	}
	if strings.Contains(block.Body, "fourth") || strings.Contains(block.Body, marker) {
		t.Fatalf("prompt block should omit overflow memories:\n%s", block.Body)
	}
	if !strings.Contains(block.Body, "Why: Because the user confirmed it last week.") {
		t.Fatalf("prompt block missing why summary:\n%s", block.Body)
	}
	for _, line := range strings.Split(block.Body, "\n") {
		if strings.HasPrefix(line, "- ") && len(line) > muninnAutoRecallLineLimit+16 {
			t.Fatalf("prompt line not truncated: %q", line)
		}
	}
}
