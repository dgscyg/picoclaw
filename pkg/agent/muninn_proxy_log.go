package agent

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/logger"
)

const muninnProxyLogPreviewLimit = 96

var muninnProxySensitiveKV = regexp.MustCompile(
	`(?i)\b(api[_-]?key|password|token|secret)\b(?:\s*[:=]\s*|\s+)([^\s,;]+)`,
)

type MuninnProxyLogEvent string

const (
	MuninnProxyLogEventAutoRecallStart      MuninnProxyLogEvent = "muninn_auto_recall_start"
	MuninnProxyLogEventAutoRecallHit        MuninnProxyLogEvent = "muninn_auto_recall_hit"
	MuninnProxyLogEventAutoRecallMiss       MuninnProxyLogEvent = "muninn_auto_recall_miss"
	MuninnProxyLogEventAutoRecallTimeout    MuninnProxyLogEvent = "muninn_auto_recall_timeout"
	MuninnProxyLogEventAutoRecallError      MuninnProxyLogEvent = "muninn_auto_recall_error"
	MuninnProxyLogEventAutoCaptureCandidate MuninnProxyLogEvent = "muninn_auto_capture_candidate"
	MuninnProxyLogEventAutoCaptureWritten   MuninnProxyLogEvent = "muninn_auto_capture_written"
	MuninnProxyLogEventAutoCaptureDropped   MuninnProxyLogEvent = "muninn_auto_capture_dropped"
	MuninnProxyLogEventAutoCaptureError     MuninnProxyLogEvent = "muninn_auto_capture_error"
	MuninnProxyLogEventCacheHit             MuninnProxyLogEvent = "muninn_consolidation_cache_hit"
	MuninnProxyLogEventCacheMiss            MuninnProxyLogEvent = "muninn_consolidation_cache_miss"
	MuninnProxyLogEventConflict             MuninnProxyLogEvent = "muninn_consolidation_conflict"
)

func muninnProxyLogFields(
	event MuninnProxyLogEvent,
	plan MuninnProxyPlan,
	result *MuninnProxyResult,
	err error,
	extra map[string]any,
) map[string]any {
	plan = plan.Normalized()
	fields := map[string]any{"event": string(event)}

	if plan.Operation != "" {
		fields["operation"] = string(plan.Operation)
	}
	if plan.RoundID != "" {
		fields["round_id"] = plan.RoundID
	}
	if plan.SessionKey != "" {
		fields["session_key"] = plan.SessionKey
	}
	if plan.Channel != "" {
		fields["channel"] = plan.Channel
	}
	if plan.Vault != "" {
		fields["vault"] = plan.Vault
	}
	if plan.MaxItems > 0 {
		fields["max_items"] = plan.MaxItems
	}
	if plan.Timeout > 0 {
		fields["timeout_ms"] = plan.Timeout.Milliseconds()
	}
	if preview := muninnProxyPreview(plan.Query); preview != "" {
		fields["query_preview"] = preview
	}
	fields["dry_run"] = plan.DryRun

	if result != nil {
		if result.Operation != "" {
			fields["operation"] = string(result.Operation)
		}
		if result.Status != "" {
			fields["status"] = string(result.Status)
		}
		if vault := strings.TrimSpace(result.Vault); vault != "" {
			fields["vault"] = vault
		}
		if reason := muninnProxyPreview(result.Reason); reason != "" {
			fields["reason"] = reason
		}
		if result.Duration > 0 {
			fields["duration_ms"] = result.Duration.Milliseconds()
		}
		if result.CandidateCount > 0 {
			fields["candidate_count"] = result.CandidateCount
		}
		if result.InjectedCount > 0 {
			fields["injected_count"] = result.InjectedCount
		}
		if len(result.Items) > 0 {
			fields["item_count"] = len(result.Items)
		}
		if result.PromptBlock.Enabled() {
			fields["prompt_title"] = strings.TrimSpace(result.PromptBlock.Title)
			fields["prompt_persist"] = result.PromptBlock.Persist
		}
	}

	if err != nil {
		fields["error"] = muninnProxyPreview(err.Error())
	}

	for key, value := range extra {
		if _, exists := fields[key]; exists {
			continue
		}
		fields[key] = value
	}

	return fields
}

func LogMuninnProxyDebug(
	event MuninnProxyLogEvent,
	plan MuninnProxyPlan,
	result *MuninnProxyResult,
	err error,
	extra map[string]any,
) {
	logger.DebugCF("agent", string(event), muninnProxyLogFields(event, plan, result, err, extra))
}

func LogMuninnProxyInfo(
	event MuninnProxyLogEvent,
	plan MuninnProxyPlan,
	result *MuninnProxyResult,
	err error,
	extra map[string]any,
) {
	logger.InfoCF("agent", string(event), muninnProxyLogFields(event, plan, result, err, extra))
}

func LogMuninnProxyWarn(
	event MuninnProxyLogEvent,
	plan MuninnProxyPlan,
	result *MuninnProxyResult,
	err error,
	extra map[string]any,
) {
	logger.WarnCF("agent", string(event), muninnProxyLogFields(event, plan, result, err, extra))
}

func LogMuninnProxyError(
	event MuninnProxyLogEvent,
	plan MuninnProxyPlan,
	result *MuninnProxyResult,
	err error,
	extra map[string]any,
) {
	logger.ErrorCF("agent", string(event), muninnProxyLogFields(event, plan, result, err, extra))
}

func muninnProxyPreview(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = muninnProxySensitiveKV.ReplaceAllString(value, `$1=[redacted]`)
	return truncateRunes(value, muninnProxyLogPreviewLimit)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}
