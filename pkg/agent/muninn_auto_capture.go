package agent

import (
	"context"
	"fmt"
	"math"
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
	muninnZHLike                = "\u559c\u6b22"
	muninnZHDislike             = "\u4e0d\u559c\u6b22"
	muninnZHPrefer              = "\u504f\u597d"
	muninnZHPreferMore          = "\u66f4\u559c\u6b22"
	muninnZHPlease              = "\u8bf7"
	muninnZHDoNot               = "\u4e0d\u8981"
	muninnZHDont                = "\u522b"
	muninnZHOnly                = "\u53ea\u7528"
	muninnZHOnlyAlt             = "\u4ec5\u7528"
	muninnZHMust                = "\u5fc5\u987b"
	muninnZHCertainly           = "\u52a1\u5fc5"
	muninnZHPunctuatedSentence  = "\u3002\uff01\uff1f\uff1b\uff0c"
	muninnExplicitPreferenceRe = regexp.MustCompile(
		`(?i)(?:\bi\s+(?:really\s+)?(?:like|prefer|love)\b|\bmy\s+(?:favorite|preferred)\b|\x{6211}(?:\x{771f}?\x{7684})?(?:\x{559c}\x{6b22}|\x{504f}\x{597d}|\x{66f4}\x{559c}\x{6b22}|\x{4e0d}\x{559c}\x{6b22}))`,
	)
	muninnExplicitPreferenceStatementRe = regexp.MustCompile(
		`(?i)\b(?:my\s+(?:preferred|favorite)\s+[^.?!,;]+\s+is|i\s+want\s+[^.?!,;]+\s+(?:by default|going forward|from now on))\b|\x{6211}(?:\x{5e0c}\x{671b}|\x{60f3}\x{8981})[^\x{3002}\x{ff01}\x{ff1f}!?]{0,40}(?:\x{9ed8}\x{8ba4}|\x{4ee5}\x{540e}|\x{4ece}\x{73b0}\x{5728}\x{8d77})`,
	)
	muninnConstraintLeadRe = regexp.MustCompile(
		`(?i)^\s*(?:please\s+)?(?:do not|don't|never|always|only|must|avoid|use)\b|^\s*(?:\x{8bf7}|\x{4e0d}\x{8981}|\x{522b}|\x{53ea}\x{80fd}|\x{5fc5}\x{987b}|\x{52a1}\x{5fc5}|\x{53ea}\x{7528}|\x{4ec5}\x{7528})`,
	)
	muninnConstraintSentenceRe = regexp.MustCompile(
		`(?i)\b(?:please\s+)?(?:do not|don't|never|always|only|must|avoid|use|keep|limit)\b|(?:\x{8bf7}|\x{4e0d}\x{8981}|\x{522b}|\x{53ea}\x{80fd}|\x{5fc5}\x{987b}|\x{52a1}\x{5fc5}|\x{53ea}\x{7528}|\x{4ec5}\x{7528})`,
	)
	muninnDecisionLeadRe = regexp.MustCompile(
		`(?i)^\s*(?:we|let'?s|the project|for this project)\b.*\b(?:will|won't|should|must|decided|decision|standardize|use)\b`,
	)
	muninnDecisionSentenceRe = regexp.MustCompile(
		`(?i)\b(?:we|team|project|repo|repository)\b[^.?!]*(?:decided|decision|standardize|standardized|will use|won't use|should use|must use|uses?)\b`,
	)
	muninnContactLeadRe = regexp.MustCompile(
		`(?i)\b(?:call me|my name is|you can reach|contact(?: mapping)?|alias is)\b`,
	)
	muninnCrossLingualAmbiguousRe = regexp.MustCompile(`(?i)(?:\x{53ef}\x{80fd}|\x{4e5f}\x{8bb8}|\x{5927}\x{6982}|\x{5dee}\x{4e0d}\x{591a}|\x{5148}\x{8fd9}\x{6837}|\x{6682}\x{65f6}|\x{56de}\x{5934}|\x{4ee5}\x{540e}\x{518d}\x{8bf4})`)
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
	trimmed := strings.TrimSpace(message)

	candidate := muninnAutoCaptureCandidate{
		SessionKey: strings.TrimSpace(opts.SessionKey),
		SenderID:   strings.TrimSpace(opts.SenderID),
		Channel:    strings.TrimSpace(opts.Channel),
	}

	switch {
	case muninnDecisionLeadRe.MatchString(trimmed) || muninnDecisionSentenceRe.MatchString(trimmed):
		candidate.Category = muninnCaptureCategoryDecision
		candidate.Concept = "project_decision"
		candidate.TypeLabel = "decision"
		candidate.Confidence = 0.95
		candidate.Stability = 0.95
	case muninnExplicitPreferenceRe.MatchString(trimmed) || muninnExplicitPreferenceStatementRe.MatchString(trimmed):
		candidate.Category = muninnCaptureCategoryPreference
		candidate.Concept = "user_preference"
		candidate.TypeLabel = "preference"
		candidate.Confidence = 0.96
		candidate.Stability = 0.93
	case muninnConstraintLeadRe.MatchString(trimmed) || muninnConstraintSentenceRe.MatchString(trimmed):
		candidate.Category = muninnCaptureCategoryConstraint
		candidate.Concept = "explicit_constraint"
		candidate.TypeLabel = "constraint"
		candidate.Confidence = 0.97
		candidate.Stability = 0.97
	case muninnContactLeadRe.MatchString(trimmed):
		candidate.Category = muninnCaptureCategoryContact
		candidate.Concept = "contact_mapping"
		candidate.TypeLabel = "contact_mapping"
		candidate.Confidence = 0.95
		candidate.Stability = 0.9
	default:
		return nil, "message did not match durable capture categories"
	}

	if len(strings.Fields(trimmed)) < 2 && len([]rune(trimmed)) < 6 {
		return nil, "message too short for durable capture"
	}
	if !(strings.Contains(trimmed, ".") || strings.Contains(trimmed, ";") || strings.Contains(trimmed, ",") ||
		strings.ContainsAny(trimmed, muninnZHPunctuatedSentence) || len([]rune(trimmed)) >= 12) {
		return nil, "message lacks durable detail"
	}

	if confidence, ok := muninnAutoCaptureConfidence(trimmed, candidate.Category); !ok {
		return nil, "message lacked high-confidence durable phrasing"
	} else {
		candidate.Confidence = confidence
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
	query := truncateRunes(buildMuninnCaptureDedupeQuery(candidate), muninnAutoRecallQueryLimit)
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

func buildMuninnCaptureDedupeQuery(candidate muninnAutoCaptureCandidate) string {
	content := normalizeWhitespace(strings.ToLower(candidate.Content))
	fingerprint := normalizeWhitespace(strings.ToLower(candidate.Fingerprint))
	parts := []string{candidate.Concept}
	if fingerprint != "" {
		parts = append(parts, fingerprint)
	}
	if content != "" && content != fingerprint {
		parts = append(parts, content)
	}
	return strings.Join(compactStrings(parts), "\n")
}

func muninnCaptureLooksDuplicate(
	item muninndb.ActivationItem,
	candidate muninnAutoCaptureCandidate,
) bool {
	normalizedCandidateContent := normalizeWhitespace(strings.ToLower(candidate.Content))
	if similarDuplicateConfidence(
		normalizedCandidateContent,
		normalizeWhitespace(strings.ToLower(item.Content)),
	) >= 0.85 {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(item.Concept), candidate.Concept) {
		if normalizeWhitespace(strings.ToLower(item.Content)) == normalizedCandidateContent {
			return true
		}
	}
	if item.Score >= muninnAutoCaptureDuplicateCutoff {
		existing := normalizeWhitespace(strings.ToLower(item.Content))
		return similarDuplicateConfidence(normalizedCandidateContent, existing) >= 0.85
	}
	return false
}

func muninnAutoCaptureConfidence(message string, category muninnCaptureCategory) (float64, bool) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return 0, false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "?") {
		return 0, false
	}
	if muninnCrossLingualAmbiguousRe.MatchString(trimmed) {
		return 0, false
	}
	for _, marker := range []string{"maybe", "might", "probably", "perhaps", "i guess", "not sure", "kind of", "sort of", "temporarily"} {
		if strings.Contains(lower, marker) {
			return 0, false
		}
	}
	if strings.Contains(lower, " for now") || strings.HasSuffix(lower, "for now.") ||
		strings.HasSuffix(lower, "for now") {
		return 0, false
	}

	score := 0.78
	if strings.Contains(lower, "from now on") || strings.Contains(lower, "going forward") ||
		strings.Contains(lower, "every project") {
		score += 0.16
	}
	if strings.Contains(lower, "for this repo") || strings.Contains(lower, "for this repository") ||
		strings.Contains(lower, "for local development") {
		score += 0.08
	}
	if strings.Contains(lower, "always") || strings.Contains(lower, "never") ||
		strings.Contains(lower, "only") ||
		strings.Contains(lower, "must") {
		score += 0.12
	}
	if strings.Contains(lower, "please") {
		score += 0.02
	}
	if strings.Contains(lower, "decided") || strings.Contains(lower, "prefer") ||
		strings.Contains(lower, "like") ||
		strings.Contains(lower, "favorite") ||
		strings.Contains(lower, "call me") {
		score += 0.08
	}
	if strings.Contains(trimmed, muninnZHLike) || strings.Contains(trimmed, muninnZHDislike) ||
		strings.Contains(trimmed, muninnZHPrefer) || strings.Contains(trimmed, muninnZHPreferMore) {
		score += 0.12
	}
	if strings.Contains(lower, "use ") || strings.HasPrefix(lower, "use ") ||
		strings.Contains(lower, "do not") || strings.Contains(lower, "don't") ||
		strings.Contains(lower, "avoid") || strings.Contains(lower, "keep ") {
		score += 0.08
	}
	if strings.Contains(trimmed, muninnZHPlease) || strings.Contains(trimmed, muninnZHDoNot) ||
		strings.Contains(trimmed, muninnZHDont) || strings.Contains(trimmed, muninnZHOnly) ||
		strings.Contains(trimmed, muninnZHOnlyAlt) || strings.Contains(trimmed, muninnZHMust) ||
		strings.Contains(trimmed, muninnZHCertainly) {
		score += 0.12
	}
	if strings.Contains(lower, "preferred") || strings.Contains(lower, "will use") ||
		strings.Contains(lower, "uses ") || strings.Contains(lower, "use sqlite") {
		score += 0.08
	}
	if category == muninnCaptureCategoryDecision || category == muninnCaptureCategoryConstraint {
		score += 0.04
	}
	if category == muninnCaptureCategoryPreference &&
		(strings.Contains(lower, "my preferred") || strings.Contains(lower, "my favorite")) {
		score += 0.06
	}
	if category == muninnCaptureCategoryPreference {
		score += 0.04
	}
	if category == muninnCaptureCategoryContact {
		score += 0.08
	}
	score = math.Min(score, 0.99)
	if score < 0.9 {
		return 0, false
	}
	return score, true
}

func similarDuplicateConfidence(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	aTokens := strings.Fields(a)
	bTokens := strings.Fields(b)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}
	aSet := make(map[string]struct{}, len(aTokens))
	for _, token := range aTokens {
		aSet[token] = struct{}{}
	}
	intersection := 0
	bSet := make(map[string]struct{}, len(bTokens))
	for _, token := range bTokens {
		bSet[token] = struct{}{}
		if _, ok := aSet[token]; ok {
			intersection++
		}
	}
	union := len(aSet)
	for token := range bSet {
		if _, ok := aSet[token]; !ok {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
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
		apiKey = strings.TrimSpace(cfg.RESTAPIKey)
	}
	return muninndb.NewClientWithHTTPClient(httpClient, endpoint, vault, apiKey)
}
