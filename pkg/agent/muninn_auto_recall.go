package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/muninndb"
)

const (
	muninnAutoRecallMaxItems   = 3
	muninnAutoRecallLineLimit  = 220
	muninnAutoRecallQueryLimit = 600
	muninnAutoRecallTimeout    = 30 * time.Second
)

func (al *AgentLoop) buildMuninnAutoRecallPlan(opts processOptions) (MuninnProxyPlan, bool) {
	cfg := al.GetConfig()
	if cfg == nil ||
		strings.TrimSpace(cfg.Memory.Provider) != config.MemoryProviderMuninnDB ||
		cfg.Memory.MuninnDB == nil {
		return MuninnProxyPlan{}, false
	}
	if strings.TrimSpace(cfg.Memory.MuninnDB.ResolvedRESTEndpoint()) == "" {
		return MuninnProxyPlan{}, false
	}
	query := buildMuninnAutoRecallQuery(opts)
	if query == "" {
		return MuninnProxyPlan{}, false
	}
	vault := strings.TrimSpace(cfg.Memory.MuninnDB.Vault)
	if vault == "" {
		vault = config.DefaultMemoryVault
	}
	return MuninnProxyPlan{
		Operation:  MuninnProxyOperationRecall,
		RoundID:    strings.TrimSpace(opts.RoundID),
		SessionKey: strings.TrimSpace(opts.SessionKey),
		Channel:    strings.TrimSpace(opts.Channel),
		Vault:      vault,
		Query:      query,
		MaxItems:   muninnAutoRecallMaxItems,
		Timeout:    parseMuninnTimeout(cfg.Memory.MuninnDB),
	}, true
}

func buildMuninnAutoRecallQuery(opts processOptions) string {
	parts := make([]string, 0, 4)
	if userMessage := strings.TrimSpace(opts.UserMessage); userMessage != "" {
		parts = append(parts, "User request: "+userMessage)
	}
	if opts.Channel != "" || opts.ChatID != "" {
		parts = append(parts, fmt.Sprintf(
			"Conversation: channel=%s chat=%s",
			strings.TrimSpace(opts.Channel),
			strings.TrimSpace(opts.ChatID),
		))
	}
	if senderLine := formatCurrentSenderLine(opts.SenderID, opts.SenderDisplayName); senderLine != "" {
		parts = append(parts, senderLine)
	}
	if sessionKey := strings.TrimSpace(opts.SessionKey); sessionKey != "" {
		parts = append(parts, "Session key: "+sessionKey)
	}
	return truncateRunes(strings.Join(parts, "\n"), muninnAutoRecallQueryLimit)
}

func parseMuninnTimeout(cfg *config.MuninnDBConfig) time.Duration {
	if cfg == nil {
		return muninnAutoRecallTimeout
	}
	timeout := strings.TrimSpace(cfg.Timeout)
	if timeout == "" {
		return muninnAutoRecallTimeout
	}
	duration, err := time.ParseDuration(timeout)
	if err != nil || duration <= 0 {
		return muninnAutoRecallTimeout
	}
	return duration
}

func (al *AgentLoop) runMuninnAutoRecall(ctx context.Context, opts processOptions) MuninnProxyResult {
	plan, ok := al.buildMuninnAutoRecallPlan(opts)
	if !ok {
		return MuninnProxyResult{Operation: MuninnProxyOperationRecall, Status: MuninnProxyStatusSkipped}
	}
	if cached, cacheReason, ok := al.lookupMuninnRecallCache(plan); ok {
		LogMuninnProxyInfo(MuninnProxyLogEventCacheHit, plan, &cached, nil, map[string]any{"cache_reason": cacheReason})
		return cached
	}
	LogMuninnProxyDebug(MuninnProxyLogEventCacheMiss, plan, nil, nil, nil)

	LogMuninnProxyDebug(MuninnProxyLogEventAutoRecallStart, plan, nil, nil, nil)
	start := time.Now()
	client := newMuninnAutoRecallClient(al.GetConfig().Memory.MuninnDB)
	recallCtx, cancel := context.WithTimeout(ctx, plan.Timeout)
	defer cancel()

	resp, err := client.Activate(recallCtx, plan.Query, plan.MaxItems)
	if err != nil {
		result := MuninnProxyResult{
			Operation: MuninnProxyOperationRecall,
			Vault:     plan.Vault,
			Duration:  time.Since(start),
			Reason:    err.Error(),
		}
		if isMuninnAutoRecallTimeout(err) {
			result.Status = MuninnProxyStatusTimeout
			al.invalidateMuninnRecallCache(plan.SessionKey)
			LogMuninnProxyWarn(MuninnProxyLogEventAutoRecallTimeout, plan, &result, err, nil)
			return result
		}
		result.Status = MuninnProxyStatusError
		al.invalidateMuninnRecallCache(plan.SessionKey)
		LogMuninnProxyWarn(MuninnProxyLogEventAutoRecallError, plan, &result, err, nil)
		return result
	}

	result := buildMuninnAutoRecallResult(plan, resp, time.Since(start))
	if result.Status == MuninnProxyStatusMiss {
		al.invalidateMuninnRecallCache(plan.SessionKey)
		LogMuninnProxyInfo(MuninnProxyLogEventAutoRecallMiss, plan, &result, nil, nil)
		return result
	}
	al.storeMuninnRecallCache(plan, result)
	LogMuninnProxyInfo(MuninnProxyLogEventAutoRecallHit, plan, &result, nil, nil)
	return result
}

func (al *AgentLoop) lookupMuninnRecallCache(plan MuninnProxyPlan) (MuninnProxyResult, string, bool) {
	if al == nil || al.recallCache == nil {
		return MuninnProxyResult{}, "", false
	}
	return al.recallCache.lookup(plan)
}

func (al *AgentLoop) storeMuninnRecallCache(plan MuninnProxyPlan, result MuninnProxyResult) {
	if al == nil || al.recallCache == nil {
		return
	}
	al.recallCache.store(plan, result)
}

func (al *AgentLoop) invalidateMuninnRecallCache(sessionKey string) {
	if al == nil || al.recallCache == nil {
		return
	}
	al.recallCache.invalidate(sessionKey)
}

func newMuninnAutoRecallClient(cfg *config.MuninnDBConfig) *muninndb.Client {
	httpClient := &http.Client{Timeout: parseMuninnTimeout(cfg)}
	vault := config.DefaultMemoryVault
	endpoint := ""
	apiKey := ""
	if cfg != nil {
		if trimmedVault := strings.TrimSpace(cfg.Vault); trimmedVault != "" {
			vault = trimmedVault
		}
		endpoint = cfg.ResolvedRESTEndpoint()
		apiKey = strings.TrimSpace(cfg.APIKey)
	}
	return muninndb.NewClientWithHTTPClient(httpClient, endpoint, vault, apiKey)
}

func isMuninnAutoRecallTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded")
}

func buildMuninnAutoRecallResult(
	plan MuninnProxyPlan,
	resp *muninndb.ActivateResponse,
	duration time.Duration,
) MuninnProxyResult {
	result := MuninnProxyResult{
		Operation: MuninnProxyOperationRecall,
		Vault:     plan.Vault,
		Duration:  duration,
	}
	if resp == nil {
		result.Status = MuninnProxyStatusMiss
		result.Reason = "activate returned no response"
		return result
	}
	result.CandidateCount = len(resp.Activations)
	if resp.TotalFound > result.CandidateCount {
		result.CandidateCount = resp.TotalFound
	}
	items := muninnAutoRecallItems(resp.Activations, plan.MaxItems)
	if len(items) == 0 {
		result.Status = MuninnProxyStatusMiss
		if result.CandidateCount > 0 {
			result.Reason = "activate returned no bounded memory items"
		} else {
			result.Reason = "activate returned no relevant memories"
		}
		return result
	}
	result.Status = MuninnProxyStatusHit
	result.Items = items
	result.InjectedCount = len(items)
	result.PromptBlock = formatMuninnAutoRecallPromptBlock(items)
	result.Reason = "activate returned relevant memories"
	return result
}

func muninnAutoRecallItems(activations []muninndb.ActivationItem, maxItems int) []MuninnProxyMemoryItem {
	if maxItems <= 0 {
		maxItems = muninnAutoRecallMaxItems
	}
	items := make([]MuninnProxyMemoryItem, 0, minInt(len(activations), maxItems))
	for _, activation := range activations {
		item := MuninnProxyMemoryItem{
			ID:      strings.TrimSpace(activation.ID),
			Concept: strings.TrimSpace(activation.Concept),
			Content: normalizeWhitespace(activation.Content),
			Score:   activation.Score,
		}
		if activation.Why != nil {
			item.Why = normalizeWhitespace(*activation.Why)
		}
		if item.Content == "" && item.Why == "" {
			continue
		}
		items = append(items, item)
		if len(items) >= maxItems {
			break
		}
	}
	return items
}

func formatMuninnAutoRecallPromptBlock(items []MuninnProxyMemoryItem) MuninnProxyPromptBlock {
	if len(items) == 0 {
		return MuninnProxyPromptBlock{}
	}
	limit := minInt(len(items), muninnAutoRecallMaxItems)
	lines := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		line := formatMuninnAutoRecallLine(items[i])
		if line != "" {
			lines = append(lines, "- "+line)
		}
	}
	if len(lines) == 0 {
		return MuninnProxyPromptBlock{}
	}
	return MuninnProxyPromptBlock{
		Title:   MuninnAutoRecallBlockTitle,
		Body:    strings.Join(lines, "\n"),
		Persist: false,
	}
}

func formatMuninnAutoRecallLine(item MuninnProxyMemoryItem) string {
	concept := normalizeWhitespace(item.Concept)
	content := truncateRunes(normalizeWhitespace(item.Content), 96)
	var summary string
	if concept != "" && content != "" {
		summary = concept + ": " + content
	} else if content != "" {
		summary = content
	} else {
		summary = concept
	}
	if why := normalizeWhitespace(item.Why); why != "" {
		why = truncateRunes(why, 84)
		if summary != "" {
			summary += " Why: " + why
		} else {
			summary = "Why: " + why
		}
	}
	return truncateRunes(summary, muninnAutoRecallLineLimit)
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
