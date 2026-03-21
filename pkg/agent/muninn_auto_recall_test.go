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
	"time"

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
		case "/api/contradictions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[]}`))
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
	queries := make([]string, 0, 1)
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
				QueryID:    "query-cache-1",
				TotalFound: 1,
				Activations: []muninndb.ActivationItem{{
					ID:      "eng-cache-1",
					Concept: "preference",
					Content: "Favorite editor: helix.",
					Score:   0.98,
				}},
			})
		case "/api/contradictions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[]}`))
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
	if activateCalls != 1 {
		t.Fatalf("activateCalls = %d, want 1", activateCalls)
	}
	if len(queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queries))
	}
	if !strings.Contains(strings.ToLower(queries[0]), "favorite editor") {
		t.Fatalf("unexpected activate query contents: %q", queries[0])
	}
	if got, want := muninnRecallQueryTokens(queries[0]), muninnRecallQueryTokens(strings.Join([]string{
		"User request: Remind me again which editor I prefer for code edits.",
		"Conversation: channel=claweb chat=room-1",
		"Current sender: cron",
		"Session key: session-cache",
	}, "\n")); !muninnRecallQueriesRelated(want, got) {
		t.Fatalf("expected cached queries to be related: first=%v second=%v", got, want)
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
	result, reason, ok := al.lookupMuninnRecallCache(plan)
	if !ok {
		t.Fatal("expected closely related follow-up query to be cached after refresh")
	}
	if result.Status != MuninnProxyStatusCacheHit {
		t.Fatalf("cache status = %q, want %q", result.Status, MuninnProxyStatusCacheHit)
	}
	if reason != "closely related turn" {
		t.Fatalf("cache reason = %q, want closely related turn", reason)
	}
}

func TestMuninnRecallQueriesRelated_AllowsParaphrasedFollowUps(t *testing.T) {
	previous := muninnRecallQueryTokens(strings.Join([]string{
		"User request: What's my favorite editor?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))
	current := muninnRecallQueryTokens(strings.Join([]string{
		"User request: Which editor do I prefer for coding?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))

	if !muninnRecallQueriesRelated(current, previous) {
		t.Fatalf("expected paraphrased follow-up tokens to be related: current=%v previous=%v", current, previous)
	}
}

func TestMuninnRecallQueriesRelated_AllowsParaphrasedFollowUpsWithSharedStrongSignals(t *testing.T) {
	previous := muninnRecallQueryTokens(strings.Join([]string{
		"User request: What's my favorite editor for repo work?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))
	current := muninnRecallQueryTokens(strings.Join([]string{
		"User request: Which coding tool do I prefer in this repo?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))

	if !muninnRecallQueriesRelated(current, previous) {
		t.Fatalf(
			"expected strong shared signals to keep paraphrased follow-up related: current=%v previous=%v",
			current,
			previous,
		)
	}
}

func TestMuninnRecallQueriesRelated_RejectsTopicShiftsDespiteGenericOverlap(t *testing.T) {
	previous := muninnRecallQueryTokens(strings.Join([]string{
		"User request: What's my favorite editor?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))
	current := muninnRecallQueryTokens(strings.Join([]string{
		"User request: What's my deployment region?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))

	if muninnRecallQueriesRelated(current, previous) {
		t.Fatalf("expected topic shift tokens to be unrelated: current=%v previous=%v", current, previous)
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
		case "/api/contradictions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[]}`))
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
		t.Fatalf("activateCalls = %d, want 2 after material context change refresh", activateCalls)
	}
	plan, ok := al.buildMuninnAutoRecallPlan(processOptions{
		SessionKey:  "session-cache-refresh",
		Channel:     "claweb",
		ChatID:      "room-1",
		SenderID:    "cron",
		UserMessage: "What's my deployment region?",
	})
	if !ok {
		t.Fatal("expected changed-context plan to be created")
	}
	if len(queries) != 2 {
		t.Fatalf("queries = %d, want 2 after refresh", len(queries))
	}
	if !strings.Contains(strings.ToLower(queries[0]), "favorite editor") {
		t.Fatalf("unexpected first activate query contents: %q", queries[0])
	}
	if !strings.Contains(strings.ToLower(queries[1]), "deployment region") {
		t.Fatalf("unexpected refreshed activate query contents: %q", queries[1])
	}
	result, reason, ok := al.lookupMuninnRecallCache(plan)
	if !ok {
		t.Fatal("expected refreshed recall result to be cached after material context change")
	}
	if result.Status != MuninnProxyStatusCacheHit {
		t.Fatalf("cache status = %q, want %q after refresh", result.Status, MuninnProxyStatusCacheHit)
	}
	if reason != "closely related turn" {
		t.Fatalf("cache reason = %q, want closely related turn after refresh", reason)
	}
	if !strings.Contains(strings.ToLower(result.PromptBlock.Body), "helix") {
		t.Fatalf("cached prompt block missing refreshed recall content: %s", result.PromptBlock.Body)
	}
}

func TestMuninnRecallQueriesRelated_RejectsEditorToDeploymentTopicShift(t *testing.T) {
	previous := muninnRecallQueryTokens(strings.Join([]string{
		"User request: What's my favorite editor for repo work?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))
	current := muninnRecallQueryTokens(strings.Join([]string{
		"User request: Which deployment region should we use for this repo?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))

	if muninnRecallQueriesRelated(current, previous) {
		t.Fatalf("expected editor-to-deployment shift to be unrelated: current=%v previous=%v", current, previous)
	}
}

func TestMuninnRecallQueryTokens_NormalizesParaphrasedRepoFollowUps(t *testing.T) {
	tokens := muninnRecallQueryTokens(strings.Join([]string{
		"User request: Which editor setup do I prefer when pairing on this repository?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))

	for _, want := range []string{"editor", "favorite", "repository"} {
		if _, ok := tokens[want]; !ok {
			t.Fatalf("expected normalized token %q in %v", want, tokens)
		}
	}
}

func TestMuninnRecallQueriesRelated_AllowsParaphrasedRepositorySetupFollowUps(t *testing.T) {
	previous := muninnRecallQueryTokens(strings.Join([]string{
		"User request: What's my favorite editor for repo work?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))
	current := muninnRecallQueryTokens(strings.Join([]string{
		"User request: Which editor setup do I prefer for this repository?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))

	if !muninnRecallQueriesRelated(current, previous) {
		t.Fatalf(
			"expected paraphrased repository setup follow-up to stay related: current=%v previous=%v",
			current,
			previous,
		)
	}
}

func TestProcessDirectWithChannel_MuninnAutoRecallReusesCacheForParaphrasedRepoFollowUp(t *testing.T) {
	tmpDir := t.TempDir()
	activateCalls := 0
	queries := make([]string, 0, 1)
	server := newRecallResponseServer(t, &activateCalls, &queries, "query-cache-paraphrase")
	defer server.Close()

	provider := &muninnAwareProvider{}
	al := NewAgentLoop(testMuninnConfig(tmpDir, server.URL), bus.NewMessageBus(), provider)

	response1, err := al.ProcessDirectWithChannel(
		context.Background(),
		"What's my favorite editor for repo work?",
		"session-cache-paraphrase",
		"claweb",
		"room-1",
	)
	if err != nil {
		t.Fatalf("first ProcessDirectWithChannel() error = %v", err)
	}
	response2, err := al.ProcessDirectWithChannel(
		context.Background(),
		"Which coding tool do I prefer in this repo?",
		"session-cache-paraphrase",
		"claweb",
		"room-1",
	)
	if err != nil {
		t.Fatalf("second ProcessDirectWithChannel() error = %v", err)
	}
	if response1 != "Your favorite editor is helix." || response2 != "Your favorite editor is helix." {
		t.Fatalf("unexpected responses: %q / %q", response1, response2)
	}
	if activateCalls != 1 {
		t.Fatalf("activateCalls = %d, want 1", activateCalls)
	}
	if len(queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queries))
	}
	plan, ok := al.buildMuninnAutoRecallPlan(processOptions{
		SessionKey:  "session-cache-paraphrase",
		Channel:     "claweb",
		ChatID:      "room-1",
		SenderID:    "cron",
		UserMessage: "Which coding tool do I prefer in this repo?",
	})
	if !ok {
		t.Fatal("expected paraphrased follow-up plan to be created")
	}
	result, reason, ok := al.lookupMuninnRecallCache(plan)
	if !ok {
		t.Fatal("expected paraphrased repo follow-up to reuse cached recall")
	}
	if result.Status != MuninnProxyStatusCacheHit {
		t.Fatalf("cache status = %q, want %q", result.Status, MuninnProxyStatusCacheHit)
	}
	if reason != "closely related turn" {
		t.Fatalf("cache reason = %q, want closely related turn", reason)
	}
	if len(provider.lastMessages) == 0 ||
		!strings.Contains(strings.ToLower(provider.lastMessages[0].Content), "helix") {
		t.Fatalf("provider prompt missing recalled fact after cache reuse: %+v", provider.lastMessages)
	}
}

func TestMuninnRecallQueriesRelated_AllowsParaphrasedFollowUpsWithMinorDetailShift(t *testing.T) {
	previous := muninnRecallQueryTokens(strings.Join([]string{
		"User request: What's my favorite editor for repo work?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))
	current := muninnRecallQueryTokens(strings.Join([]string{
		"User request: Which coding tool do I prefer for teammate handoffs in this repo?",
		"Conversation: channel=claweb chat=room-1",
		"Session key: session-cache",
	}, "\n"))

	if !muninnRecallQueriesRelated(current, previous) {
		t.Fatalf("expected minor detail shift to stay related: current=%v previous=%v", current, previous)
	}
}

func newRecallResponseServer(
	t *testing.T,
	activateCalls *int,
	queries *[]string,
	queryID string,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activate":
			*activateCalls++
			defer r.Body.Close()
			var req muninndb.ActivateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode activate request: %v", err)
			}
			*queries = append(*queries, strings.Join(req.Context, "\n"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(muninndb.ActivateResponse{
				QueryID:    queryID,
				TotalFound: 1,
				Activations: []muninndb.ActivationItem{{
					ID:      "eng-cache-shared",
					Concept: "preference",
					Content: "Favorite editor: helix.",
					Score:   0.98,
				}},
			})
		case "/api/contradictions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/api/engrams":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"eng-write","created_at":123}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
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

func TestProcessDirectWithChannel_MuninnAutoRecallHandlesConflictsWithoutReinforcingStaleFacts(t *testing.T) {
	tmpDir := t.TempDir()
	activateCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activate":
			activateCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(muninndb.ActivateResponse{
				QueryID:    "query-conflict",
				TotalFound: 2,
				Activations: []muninndb.ActivationItem{{
					ID:      "eng-stable",
					Concept: "deployment_region",
					Content: "Primary deployment region: us-east-1.",
					Score:   0.99,
				}, {
					ID:      "eng-stale",
					Concept: "deployment_region",
					Content: "Primary deployment region: eu-west-1.",
					Score:   0.74,
				}},
			})
		case "/api/contradictions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(
				`{"items":[{"left_id":"eng-stable","right_id":"eng-stale","reason":"conflicting deployment regions","resolution":"Prefer the newer stable region unless the user restates otherwise."}]}`,
			))
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

	response, err := al.ProcessDirectWithChannel(
		context.Background(),
		"What's our primary deployment region?",
		"session-conflict",
		"claweb",
		"room-1",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel() error = %v", err)
	}
	if response == "" {
		t.Fatal("expected a reply during conflicted recall")
	}
	if activateCalls != 1 {
		t.Fatalf("activateCalls = %d, want 1", activateCalls)
	}
	if len(provider.lastMessages) == 0 {
		t.Fatal("provider did not receive any messages")
	}
	systemPrompt := provider.lastMessages[0].Content
	if !strings.Contains(systemPrompt, "us-east-1") {
		t.Fatalf("system prompt missing stable region: %s", systemPrompt)
	}
	if !strings.Contains(
		systemPrompt,
		"Conflict note: Prefer the newer stable region unless the user restates otherwise.",
	) {
		t.Fatalf("system prompt missing conflict note: %s", systemPrompt)
	}
	if strings.Contains(systemPrompt, "eu-west-1") {
		t.Fatalf("system prompt should not reinforce stale conflicted fact: %s", systemPrompt)
	}
	plan, ok := al.buildMuninnAutoRecallPlan(processOptions{
		SessionKey:  "session-conflict",
		Channel:     "claweb",
		ChatID:      "room-1",
		UserMessage: "What's our primary deployment region?",
	})
	if !ok {
		t.Fatal("expected plan to be created")
	}
	result, _, ok := al.lookupMuninnRecallCache(plan)
	if !ok {
		t.Fatal("expected conflicted recall result to be cached")
	}
	if result.Status != MuninnProxyStatusCacheHit {
		t.Fatalf("cached result status = %q, want %q", result.Status, MuninnProxyStatusCacheHit)
	}
	if !strings.Contains(result.PromptBlock.Body, "Conflict note") {
		t.Fatalf("cached prompt block missing conflict note: %s", result.PromptBlock.Body)
	}
	if strings.Contains(result.PromptBlock.Body, "eu-west-1") {
		t.Fatalf("cached prompt block retained stale fact: %s", result.PromptBlock.Body)
	}
}

func TestProcessDirectWithChannel_MuninnAutoCaptureRefreshesLaterTurnsWithoutRestart(t *testing.T) {
	tmpDir := t.TempDir()
	activateCalls := 0
	writeCalls := 0
	storedPreference := ""
	provider := &captureTestProvider{response: "Okay."}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activate":
			activateCalls++
			w.Header().Set("Content-Type", "application/json")
			activations := []muninndb.ActivationItem{}
			if storedPreference != "" {
				activations = append(activations, muninndb.ActivationItem{
					ID:      "eng-pref",
					Concept: "user_preference",
					Content: storedPreference,
					Score:   0.98,
				})
			}
			_ = json.NewEncoder(w).Encode(muninndb.ActivateResponse{
				QueryID:     "query-refresh-after-write",
				TotalFound:  len(activations),
				Activations: activations,
			})
		case "/api/contradictions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/api/engrams":
			writeCalls++
			defer r.Body.Close()
			var req muninndb.WriteRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode write request: %v", err)
			}
			storedPreference = req.Content
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"eng-new-pref","created_at":123}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	al := NewAgentLoop(testMuninnConfig(tmpDir, server.URL), bus.NewMessageBus(), provider)

	firstReply, err := al.ProcessDirectWithChannel(
		context.Background(),
		"I prefer dark mode for this repo.",
		"session-refresh",
		"claweb",
		"room-1",
	)
	if err != nil {
		t.Fatalf("first ProcessDirectWithChannel() error = %v", err)
	}
	if firstReply != "Okay." {
		t.Fatalf("first reply = %q", firstReply)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && writeCalls == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writeCalls)
	}

	plan, ok := al.buildMuninnAutoRecallPlan(processOptions{
		SessionKey:  "session-refresh",
		Channel:     "claweb",
		ChatID:      "room-1",
		UserMessage: "What's my preference again?",
	})
	if !ok {
		t.Fatal("expected plan to be created")
	}
	if _, _, ok := al.lookupMuninnRecallCache(plan); ok {
		t.Fatal("expected cache to be invalidated after capture write")
	}

	provider.response = "Your preference is dark mode."
	secondReply, err := al.ProcessDirectWithChannel(
		context.Background(),
		"What's my preference again?",
		"session-refresh",
		"claweb",
		"room-1",
	)
	if err != nil {
		t.Fatalf("second ProcessDirectWithChannel() error = %v", err)
	}
	if secondReply != "Your preference is dark mode." {
		t.Fatalf("second reply = %q", secondReply)
	}
	if activateCalls < 3 {
		t.Fatalf("activateCalls = %d, want at least 3 to prove refresh after write", activateCalls)
	}
	if len(provider.lastMsgs) == 0 {
		t.Fatal("provider did not receive messages on second turn")
	}
	if !strings.Contains(strings.ToLower(provider.lastMsgs[0].Content), "dark mode") {
		t.Fatalf("second-turn system prompt missing refreshed recall: %s", provider.lastMsgs[0].Content)
	}
}
