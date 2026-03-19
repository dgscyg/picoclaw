package agent

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/muninndb"
)

const (
	muninnAutoCaptureTimeout         = 30 * time.Second
	muninnAutoCaptureActivateLimit   = 5
	muninnAutoCaptureDuplicateCutoff = 0.92
)

var (
	muninnExplicitPreferenceRe = regexp.MustCompile(
		`(?i)\b(?:i\s+(?:really\s+)?(?:like|prefer|love)|my\s+(?:favorite|preferred))\b`,
	)
	muninnConstraintLeadRe = regexp.MustCompile(
		`(?i)^\s*(?:please\s+)?(?:do not|don't|never|always|only|must|avoid|use)\b`,
	)
	muninnDecisionLeadRe = regexp.MustCompile(
		`(?i)^\s*(?:we|let'?s|the project|for this project)\b.*\b(?:will|won't|should|must|decided|decision|standardize|use)\b`,
	)
	muninnContactLeadRe = regexp.MustCompile(
		`(?i)\b(?:call me|my name is|you can reach|contact(?: mapping)?|alias is)\b`,
	)
)

type muninnCaptureCategory string

const (
	muninnCaptureCategoryPreference muninnCaptureCategory = "preference"
	muninnCaptureCategoryConstraint muninnCaptureCategory = "constraint"
	muninnCaptureCategoryDecision   muninnCaptureCategory = "project_decision"
	muninnCaptureCategoryContact    muninnCaptureCategory = "contact_mapping"
)

type muninnAutoCaptureCandidate struct {
	Category    muninnCaptureCategory
	Content     string
	Concept     string
	Summary     string
	Tags        []string
	TypeLabel   string
	Confidence  float64
	Stability   float64
	SessionKey  string
	SenderID    string
	Channel     string
	Fingerprint string
}

func (al *AgentLoop) buildMuninnAutoCapturePlan(opts processOptions) (MuninnProxyPlan, bool) {
	cfg := al.GetConfig()
	if cfg == nil || strings.TrimSpace(cfg.Memory.Provider) != config.MemoryProviderMuninnDB ||
		cfg.Memory.MuninnDB == nil {
		return MuninnProxyPlan{}, false
	}
	if strings.TrimSpace(cfg.Memory.MuninnDB.ResolvedRESTEndpoint()) == "" {
		return MuninnProxyPlan{}, false
	}
	vault := strings.TrimSpace(cfg.Memory.MuninnDB.Vault)
	if vault == "" {
		vault = config.DefaultMemoryVault
	}
	query := buildMuninnAutoRecallQuery(opts)
	if query == "" {
		query = strings.TrimSpace(opts.UserMessage)
	}
	if query == "" {
		return MuninnProxyPlan{}, false
	}
	return MuninnProxyPlan{
		Operation:  MuninnProxyOperationCapture,
		RoundID:    strings.TrimSpace(opts.RoundID),
		SessionKey: strings.TrimSpace(opts.SessionKey),
		Channel:    strings.TrimSpace(opts.Channel),
		Vault:      vault,
		Query:      query,
		MaxItems:   muninnAutoCaptureActivateLimit,
		Timeout:    parseMuninnTimeout(cfg.Memory.MuninnDB),
	}, true
}

func (al *AgentLoop) runMuninnAutoCapture(ctx context.Context, opts processOptions) {
	plan, ok := al.buildMuninnAutoCapturePlan(opts)
	if !ok {
		return
	}
	candidate, dropReason := extractMuninnAutoCaptureCandidate(opts)
	if candidate == nil {
		result := MuninnProxyResult{
			Operation: MuninnProxyOperationCapture,
			Status:    MuninnProxyStatusDropped,
			Vault:     plan.Vault,
			Reason:    dropReason,
		}
		LogMuninnProxyInfo(MuninnProxyLogEventAutoCaptureDropped, plan, &result, nil, nil)
		return
	}

	result := MuninnProxyResult{
		Operation:      MuninnProxyOperationCapture,
		Status:         MuninnProxyStatusPending,
		Vault:          plan.Vault,
		CandidateCount: 1,
		Items: []MuninnProxyMemoryItem{{
			Concept: candidate.Concept,
			Content: candidate.Content,
			Score:   candidate.Confidence,
			Tags:    append([]string(nil), candidate.Tags...),
		}},
	}
	LogMuninnProxyInfo(
		MuninnProxyLogEventAutoCaptureCandidate,
		plan,
		&result,
		nil,
		map[string]any{
			"category":   string(candidate.Category),
			"concept":    candidate.Concept,
			"type_label": candidate.TypeLabel,
		},
	)

	go al.executeMuninnAutoCapture(context.WithoutCancel(ctx), plan, *candidate)
}

func (al *AgentLoop) executeMuninnAutoCapture(
	ctx context.Context,
	plan MuninnProxyPlan,
	candidate muninnAutoCaptureCandidate,
) {
	start := time.Now()
	client := newMuninnAutoCaptureClient(al.GetConfig().Memory.MuninnDB)
	ctx, cancel := context.WithTimeout(ctx, plan.Timeout)
	defer cancel()

	if duplicate, err := muninnAutoCaptureDuplicateExists(ctx, client, plan, candidate); err != nil {
		result := MuninnProxyResult{
			Operation:      MuninnProxyOperationCapture,
			Status:         MuninnProxyStatusError,
			Vault:          plan.Vault,
			Reason:         err.Error(),
			Duration:       time.Since(start),
			CandidateCount: 1,
		}
		LogMuninnProxyWarn(
			MuninnProxyLogEventAutoCaptureError,
			plan,
			&result,
			err,
			map[string]any{"category": string(candidate.Category), "concept": candidate.Concept},
		)
		return
	} else if duplicate {
		result := MuninnProxyResult{Operation: MuninnProxyOperationCapture, Status: MuninnProxyStatusDropped, Vault: plan.Vault, Reason: "duplicate durable signal skipped", Duration: time.Since(start), CandidateCount: 1}
		LogMuninnProxyInfo(MuninnProxyLogEventAutoCaptureDropped, plan, &result, nil, map[string]any{"category": string(candidate.Category), "concept": candidate.Concept, "dedupe": true})
		return
	}

	_, err := client.WriteEngramRequest(ctx, muninndb.WriteRequest{
		Concept:    candidate.Concept,
		Content:    candidate.Content,
		Tags:       append([]string(nil), candidate.Tags...),
		Confidence: candidate.Confidence,
		Stability:  candidate.Stability,
		TypeLabel:  candidate.TypeLabel,
		Summary:    candidate.Summary,
	})
	if err != nil {
		result := MuninnProxyResult{
			Operation:      MuninnProxyOperationCapture,
			Status:         MuninnProxyStatusError,
			Vault:          plan.Vault,
			Reason:         err.Error(),
			Duration:       time.Since(start),
			CandidateCount: 1,
		}
		LogMuninnProxyWarn(
			MuninnProxyLogEventAutoCaptureError,
			plan,
			&result,
			err,
			map[string]any{"category": string(candidate.Category), "concept": candidate.Concept},
		)
		return
	}

	result := MuninnProxyResult{
		Operation:      MuninnProxyOperationCapture,
		Status:         MuninnProxyStatusWritten,
		Vault:          plan.Vault,
		Reason:         "captured durable signal",
		Duration:       time.Since(start),
		CandidateCount: 1,
		Items: []MuninnProxyMemoryItem{
			{
				Concept: candidate.Concept,
				Content: candidate.Content,
				Score:   candidate.Confidence,
				Tags:    append([]string(nil), candidate.Tags...),
			},
		},
	}
	LogMuninnProxyInfo(
		MuninnProxyLogEventAutoCaptureWritten,
		plan,
		&result,
		nil,
		map[string]any{
			"category":   string(candidate.Category),
			"concept":    candidate.Concept,
			"type_label": candidate.TypeLabel,
		},
	)
}

func extractMuninnAutoCaptureCandidate(opts processOptions) (*muninnAutoCaptureCandidate, string) {
	message := normalizeWhitespace(opts.UserMessage)
	if message == "" {
		return nil, "empty user message"
	}
	if strings.Count(message, " ") < 3 {
		return nil, "message too short for durable capture"
	}
	trimmed := strings.TrimSpace(message)
	if !(strings.Contains(trimmed, ".") || strings.Contains(trimmed, ";") || strings.Contains(trimmed, ",") || len([]rune(trimmed)) >= 28) {
		return nil, "message lacks durable detail"
	}

	candidate := muninnAutoCaptureCandidate{
		SessionKey: strings.TrimSpace(opts.SessionKey),
		SenderID:   strings.TrimSpace(opts.SenderID),
		Channel:    strings.TrimSpace(opts.Channel),
	}

	switch {
	case muninnExplicitPreferenceRe.MatchString(trimmed):
		candidate.Category = muninnCaptureCategoryPreference
		candidate.Concept = "user_preference"
		candidate.TypeLabel = "preference"
		candidate.Confidence = 0.96
		candidate.Stability = 0.93
	case muninnConstraintLeadRe.MatchString(trimmed):
		candidate.Category = muninnCaptureCategoryConstraint
		candidate.Concept = "explicit_constraint"
		candidate.TypeLabel = "constraint"
		candidate.Confidence = 0.97
		candidate.Stability = 0.97
	case muninnDecisionLeadRe.MatchString(trimmed):
		candidate.Category = muninnCaptureCategoryDecision
		candidate.Concept = "project_decision"
		candidate.TypeLabel = "decision"
		candidate.Confidence = 0.95
		candidate.Stability = 0.95
	case muninnContactLeadRe.MatchString(trimmed):
		candidate.Category = muninnCaptureCategoryContact
		candidate.Concept = "contact_mapping"
		candidate.TypeLabel = "contact_mapping"
		candidate.Confidence = 0.95
		candidate.Stability = 0.9
	default:
		return nil, "message did not match durable capture categories"
	}

	candidate.Content = trimmed
	candidate.Tags = muninnAutoCaptureTags(candidate, opts)
	candidate.Summary = muninnAutoCaptureSummary(candidate)
	candidate.Fingerprint = normalizeWhitespace(
		strings.ToLower(candidate.Concept + "|" + candidate.Content),
	)
	return &candidate, ""
}

func muninnAutoCaptureTags(candidate muninnAutoCaptureCandidate, opts processOptions) []string {
	tags := []string{"auto-capture", string(candidate.Category), "transparent-memory"}
	if channel := strings.TrimSpace(opts.Channel); channel != "" {
		tags = append(tags, "channel:"+channel)
	}
	if sender := strings.TrimSpace(opts.SenderID); sender != "" {
		tags = append(tags, "sender:"+sender)
	}
	sort.Strings(tags)
	return compactStrings(tags)
}

func muninnAutoCaptureSummary(candidate muninnAutoCaptureCandidate) string {
	return truncateRunes(
		fmt.Sprintf(
			"Auto-captured %s: %s",
			strings.ReplaceAll(string(candidate.Category), "_", " "),
			candidate.Content,
		),
		180,
	)
}

func muninnAutoCaptureDuplicateExists(
	ctx context.Context,
	client *muninndb.Client,
	plan MuninnProxyPlan,
	candidate muninnAutoCaptureCandidate,
) (bool, error) {
	query := truncateRunes(candidate.Concept+"\n"+candidate.Content, muninnAutoRecallQueryLimit)
	resp, err := client.Activate(ctx, query, muninnAutoCaptureActivateLimit)
	if err != nil {
		return false, err
	}
	for _, item := range resp.Activations {
		if muninnCaptureLooksDuplicate(item, candidate) {
			return true, nil
		}
	}
	_ = plan
	return false, nil
}

func muninnCaptureLooksDuplicate(
	item muninndb.ActivationItem,
	candidate muninnAutoCaptureCandidate,
) bool {
	normalizedCandidateContent := normalizeWhitespace(strings.ToLower(candidate.Content))
	if strings.EqualFold(strings.TrimSpace(item.Concept), candidate.Concept) {
		if normalizeWhitespace(strings.ToLower(item.Content)) == normalizedCandidateContent {
			return true
		}
	}
	if item.Score >= muninnAutoCaptureDuplicateCutoff {
		existing := normalizeWhitespace(strings.ToLower(item.Content))
		return existing == normalizedCandidateContent
	}
	return false
}

func compactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func newMuninnAutoCaptureClient(cfg *config.MuninnDBConfig) *muninndb.Client {
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
