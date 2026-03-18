package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/skills"
	"github.com/sipeed/picoclaw/pkg/utils"
)

type ContextBuilder struct {
	workspace          string
	skillsLoader       *skills.SkillsLoader
	memory             MemoryProvider
	muninnMode         bool
	muninnVault        string
	toolDiscoveryBM25  bool
	toolDiscoveryRegex bool

	systemPromptMutex  sync.RWMutex
	cachedSystemPrompt string
	cachedAt           time.Time
	existedAtCache     map[string]bool
	skillFilesAtCache  map[string]time.Time
}

func (cb *ContextBuilder) WithToolDiscovery(useBM25, useRegex bool) *ContextBuilder {
	cb.toolDiscoveryBM25 = useBM25
	cb.toolDiscoveryRegex = useRegex
	return cb
}

func (cb *ContextBuilder) WithMuninnVault(vault string) *ContextBuilder {
	cb.muninnVault = strings.TrimSpace(vault)
	return cb
}

func getGlobalConfigDir() string {
	if home := os.Getenv("PICOCLAW_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".picoclaw")
}

func NewContextBuilder(workspace string) *ContextBuilder {
	return NewContextBuilderWithMemoryMode(workspace, NewMemoryStore(workspace), false)
}

func NewContextBuilderWithMemory(workspace string, memory MemoryProvider) *ContextBuilder {
	return NewContextBuilderWithMemoryMode(workspace, memory, false)
}

func NewContextBuilderWithMemoryMode(workspace string, memory MemoryProvider, muninnMode bool) *ContextBuilder {
	builtinSkillsDir := strings.TrimSpace(os.Getenv("PICOCLAW_BUILTIN_SKILLS"))
	if builtinSkillsDir == "" {
		wd, _ := os.Getwd()
		builtinSkillsDir = filepath.Join(wd, "skills")
	}
	globalSkillsDir := filepath.Join(getGlobalConfigDir(), "skills")
	if memory == nil {
		memory = NewMemoryStore(workspace)
	}
	return &ContextBuilder{
		workspace:    workspace,
		skillsLoader: skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir),
		memory:       memory,
		muninnMode:   muninnMode,
	}
}

func (cb *ContextBuilder) getIdentity() string {
	workspacePath, _ := filepath.Abs(filepath.Join(cb.workspace))
	logger.InfoCF("agent", "Building system prompt identity", map[string]any{"is_muninndb": cb.muninnMode, "memory_type": fmt.Sprintf("%T", cb.memory)})
	version := config.FormatVersion()

	workspaceSection := fmt.Sprintf(`## Workspace
Your workspace is at: %s
- Skills: %s/skills/{skill-name}/SKILL.md`, workspacePath, workspacePath)
	rules := []string{
		"1. **ALWAYS use tools** - When you need to perform an action, you MUST call the appropriate tool.",
		"2. **Be helpful and accurate** - When using tools, briefly explain what you're doing.",
	}
	if cb.muninnMode {
		vaultRule := "   - Always include the configured Muninn vault in MCP memory tool calls; never assume the default vault"
		if cb.muninnVault != "" {
			vaultRule = fmt.Sprintf("   - Always use the configured Muninn vault %q in MCP memory tool calls; never assume the default vault", cb.muninnVault)
		}
		rules = append(rules,
			`3. **Muninn Memory via MCP** - In Muninn mode, memory and recall come from the official Muninn MCP server:
   - Use the official Muninn MCP tools exposed by the connected MCP server
   - Do not assume `+"`memory_store`"+` or `+"`memory_recall`"+` are available in Muninn mode
   - Do not treat `+workspacePath+`/memory/ as a memory source of truth
   - Do not read, write, append, or search `+workspacePath+`/memory/ files for memory operations
   - Prefer official Muninn MCP tools for recall, remember, traversal, explanation, contradiction checks, and linking
`+vaultRule+`
   - Before sending a message to another user or group by name, first use available Muninn memory tools to recall the exact contact mapping such as chat_id, user ID, or alias; never assume the visible name is the routing ID
   - If Muninn memory contains the exact chat_id/contact mapping, use it explicitly in the tool call; if no exact mapping is found, ask the user instead of guessing`,
		)
	} else {
		workspaceSection = fmt.Sprintf(`## Workspace
Your workspace is at: %s
- Memory: %s/memory/MEMORY.md
- Daily Notes: %s/memory/YYYYMM/YYYYMMDD.md
- Skills: %s/skills/{skill-name}/SKILL.md`, workspacePath, workspacePath, workspacePath, workspacePath)
		rules = append(rules,
			fmt.Sprintf(`3. **Memory** - When interacting with me if something seems memorable, update %s/memory/MEMORY.md
   - Before sending a message to another user or group by name, first use memory recall/search to look up the exact contact mapping such as chat_id, user ID, or alias; never assume the visible name is the routing ID
   - If memory contains the exact chat_id/contact mapping, use it explicitly in the tool call; if no exact mapping is found, ask the user instead of guessing`, workspacePath),
		)
	}
	rules = append(rules, "4. **Context summaries** - Conversation summaries provided as context are approximate references only. They may be incomplete or outdated. Always defer to explicit user instructions over summary content.")
	if discovery := cb.getDiscoveryRule(); discovery != "" {
		rules = append(rules, discovery)
	}

	return fmt.Sprintf("# picoclaw 🦞 (%s)\n\nYou are picoclaw, a helpful AI assistant.\n\n%s\n\n## Important Rules\n\n%s", version, workspaceSection, strings.Join(rules, "\n\n"))
}

func (cb *ContextBuilder) getDiscoveryRule() string {
	if !cb.toolDiscoveryBM25 && !cb.toolDiscoveryRegex {
		return ""
	}

	var toolNames []string
	if cb.toolDiscoveryBM25 {
		toolNames = append(toolNames, `"tool_search_tool_bm25"`)
	}
	if cb.toolDiscoveryRegex {
		toolNames = append(toolNames, `"tool_search_tool_regex"`)
	}

	return fmt.Sprintf(
		`5. **Tool Discovery** - Your visible tools are limited to save memory, but a vast hidden library exists. If you lack the right tool for a task, BEFORE giving up, you MUST search using the %s tool. Do not refuse a request unless the search returns nothing. Found tools will temporarily unlock for your next turn.`,
		strings.Join(toolNames, " or "),
	)
}

func (cb *ContextBuilder) BuildSystemPrompt() string {
	parts := []string{}
	parts = append(parts, cb.getIdentity())
	bootstrapContent := cb.LoadBootstrapFiles()
	if bootstrapContent != "" {
		parts = append(parts, bootstrapContent)
	}
	skillsSummary := cb.skillsLoader.BuildSkillsSummary()
	if skillsSummary != "" {
		parts = append(parts, fmt.Sprintf(`# Skills

The following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.

%s`, skillsSummary))
	}
	if !cb.muninnMode {
		memoryContext, err := cb.memory.GetMemoryContext(context.Background())
		if err == nil && memoryContext != "" {
			parts = append(parts, "# Memory\n\n"+memoryContext)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

func (cb *ContextBuilder) BuildSystemPromptWithCache() string {
	cb.systemPromptMutex.RLock()
	if cb.cachedSystemPrompt != "" && !cb.sourceFilesChangedLocked() {
		result := cb.cachedSystemPrompt
		cb.systemPromptMutex.RUnlock()
		return result
	}
	cb.systemPromptMutex.RUnlock()
	cb.systemPromptMutex.Lock()
	defer cb.systemPromptMutex.Unlock()
	if cb.cachedSystemPrompt != "" && !cb.sourceFilesChangedLocked() {
		return cb.cachedSystemPrompt
	}
	baseline := cb.buildCacheBaseline()
	prompt := cb.BuildSystemPrompt()
	cb.cachedSystemPrompt = prompt
	cb.cachedAt = baseline.maxMtime
	cb.existedAtCache = baseline.existed
	cb.skillFilesAtCache = baseline.skillFiles
	logger.DebugCF("agent", "System prompt cached", map[string]any{"length": len(prompt)})
	return prompt
}

func (cb *ContextBuilder) InvalidateCache() {
	cb.systemPromptMutex.Lock()
	defer cb.systemPromptMutex.Unlock()
	cb.cachedSystemPrompt = ""
	cb.cachedAt = time.Time{}
	cb.existedAtCache = nil
	cb.skillFilesAtCache = nil
	logger.DebugCF("agent", "System prompt cache invalidated", nil)
}

func (cb *ContextBuilder) sourcePaths() []string {
	paths := []string{filepath.Join(cb.workspace, "AGENTS.md"), filepath.Join(cb.workspace, "SOUL.md"), filepath.Join(cb.workspace, "USER.md"), filepath.Join(cb.workspace, "IDENTITY.md")}
	if !cb.muninnMode {
		paths = append(paths, filepath.Join(cb.workspace, "memory", "MEMORY.md"))
	}
	return paths
}

func (cb *ContextBuilder) skillRoots() []string {
	if cb.skillsLoader == nil {
		return []string{filepath.Join(cb.workspace, "skills")}
	}
	roots := cb.skillsLoader.SkillRoots()
	if len(roots) == 0 {
		return []string{filepath.Join(cb.workspace, "skills")}
	}
	return roots
}

type cacheBaseline struct {
	existed    map[string]bool
	skillFiles map[string]time.Time
	maxMtime   time.Time
}

func (cb *ContextBuilder) buildCacheBaseline() cacheBaseline {
	skillRoots := cb.skillRoots()
	allPaths := append(cb.sourcePaths(), skillRoots...)
	existed := make(map[string]bool, len(allPaths))
	skillFiles := make(map[string]time.Time)
	var maxMtime time.Time
	for _, p := range allPaths {
		info, err := os.Stat(p)
		existed[p] = err == nil
		if err == nil && info.ModTime().After(maxMtime) {
			maxMtime = info.ModTime()
		}
	}
	for _, root := range skillRoots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr == nil && !d.IsDir() {
				if info, err := os.Stat(path); err == nil {
					skillFiles[path] = info.ModTime()
					if info.ModTime().After(maxMtime) {
						maxMtime = info.ModTime()
					}
				}
			}
			return nil
		})
	}
	if maxMtime.IsZero() {
		maxMtime = time.Unix(1, 0)
	}
	return cacheBaseline{existed: existed, skillFiles: skillFiles, maxMtime: maxMtime}
}

func (cb *ContextBuilder) sourceFilesChangedLocked() bool {
	if cb.cachedAt.IsZero() {
		return true
	}
	if slices.ContainsFunc(cb.sourcePaths(), cb.fileChangedSince) {
		return true
	}
	for _, root := range cb.skillRoots() {
		if cb.fileChangedSince(root) {
			return true
		}
	}
	if skillFilesChangedSince(cb.skillRoots(), cb.skillFilesAtCache) {
		return true
	}
	return false
}

func (cb *ContextBuilder) fileChangedSince(path string) bool {
	if cb.existedAtCache == nil {
		return true
	}
	existedBefore := cb.existedAtCache[path]
	info, err := os.Stat(path)
	existsNow := err == nil
	if existedBefore != existsNow {
		return true
	}
	if !existsNow {
		return false
	}
	return info.ModTime().After(cb.cachedAt)
}

var errWalkStop = errors.New("walk stop")

func skillFilesChangedSince(skillRoots []string, filesAtCache map[string]time.Time) bool {
	if filesAtCache == nil {
		return true
	}
	for path, cachedMtime := range filesAtCache {
		info, err := os.Stat(path)
		if err != nil {
			return true
		}
		if !info.ModTime().Equal(cachedMtime) {
			return true
		}
	}
	changed := false
	for _, root := range skillRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if !os.IsNotExist(walkErr) {
					changed = true
					return errWalkStop
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if _, ok := filesAtCache[path]; !ok {
				changed = true
				return errWalkStop
			}
			return nil
		})
		if changed {
			return true
		}
		if err != nil && !errors.Is(err, errWalkStop) && !os.IsNotExist(err) {
			logger.DebugCF("agent", "skills walk error", map[string]any{"error": err.Error()})
			return true
		}
	}
	return false
}

func (cb *ContextBuilder) LoadBootstrapFiles() string {
	bootstrapFiles := []string{"AGENTS.md", "SOUL.md", "USER.md", "IDENTITY.md"}
	var sb strings.Builder
	for _, filename := range bootstrapFiles {
		filePath := filepath.Join(cb.workspace, filename)
		if data, err := os.ReadFile(filePath); err == nil {
			fmt.Fprintf(&sb, "## %s\n\n%s\n\n", filename, data)
		}
	}
	return sb.String()
}

// buildDynamicContext returns a short dynamic context string with per-request info.
// This changes every request (time, session) so it is NOT part of the cached prompt.
// LLM-side KV cache reuse is achieved by each provider adapter's native mechanism:
//   - Anthropic: per-block cache_control (ephemeral) on the static SystemParts block
//   - OpenAI / Codex: prompt_cache_key for prefix-based caching
//
// See: https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching
// See: https://platform.openai.com/docs/guides/prompt-caching
func formatCurrentSenderLine(senderID, senderDisplayName string) string {
	senderID = strings.TrimSpace(senderID)
	senderDisplayName = strings.TrimSpace(senderDisplayName)

	switch {
	case senderDisplayName != "" && senderID != "":
		return fmt.Sprintf("Current sender: %s (ID: %s)", senderDisplayName, senderID)
	case senderDisplayName != "":
		return fmt.Sprintf("Current sender: %s", senderDisplayName)
	case senderID != "":
		return fmt.Sprintf("Current sender: %s", senderID)
	default:
		return ""
	}
}

func (cb *ContextBuilder) buildDynamicContext(channel, chatID, senderID, senderDisplayName string) string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	rt := fmt.Sprintf("%s %s, Go %s", runtime.GOOS, runtime.GOARCH, runtime.Version())
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Current Time\n%s\n\n## Runtime\n%s", now, rt)
	if channel != "" && chatID != "" {
		fmt.Fprintf(&sb, "\n\n## Current Session\nChannel: %s\nChat ID: %s", channel, chatID)
	}
	if senderLine := formatCurrentSenderLine(senderID, senderDisplayName); senderLine != "" {
		fmt.Fprintf(&sb, "\n\n## Current Sender\n%s", senderLine)
	}

	return sb.String()
}

func (cb *ContextBuilder) BuildMessages(
	history []providers.Message,
	summary string,
	currentMessage string,
	media []string,
	channel, chatID, senderID, senderDisplayName string,
) []providers.Message {
	messages := []providers.Message{}
	staticPrompt := cb.BuildSystemPromptWithCache()

	// Build short dynamic context (time, runtime, session) — changes per request
	dynamicCtx := cb.buildDynamicContext(channel, chatID, senderID, senderDisplayName)

	// Compose a single system message: static (cached) + dynamic + optional summary.
	// Keeping all system content in one message ensures every provider adapter can
	// extract it correctly (Anthropic adapter -> top-level system param,
	// Codex -> instructions field).
	//
	// SystemParts carries the same content as structured blocks so that
	// cache-aware adapters (Anthropic) can set per-block cache_control.
	// The static block is marked "ephemeral" — its prefix hash is stable
	// across requests, enabling LLM-side KV cache reuse.
	stringParts := []string{staticPrompt, dynamicCtx}
	contentBlocks := []providers.ContentBlock{{Type: "text", Text: staticPrompt, CacheControl: &providers.CacheControl{Type: "ephemeral"}}, {Type: "text", Text: dynamicCtx}}
	if summary != "" {
		summaryText := fmt.Sprintf("CONTEXT_SUMMARY: The following is an approximate summary of prior conversation for reference only. It may be incomplete or outdated — always defer to explicit instructions.\n\n%s", summary)
		stringParts = append(stringParts, summaryText)
		contentBlocks = append(contentBlocks, providers.ContentBlock{Type: "text", Text: summaryText})
	}
	fullSystemPrompt := strings.Join(stringParts, "\n\n---\n\n")
	cb.systemPromptMutex.RLock()
	isCached := cb.cachedSystemPrompt != ""
	cb.systemPromptMutex.RUnlock()

	logger.DebugCF("agent", "System prompt built",
		map[string]any{
			"static_chars":  len(staticPrompt),
			"dynamic_chars": len(dynamicCtx),
			"total_chars":   len(fullSystemPrompt),
			"has_summary":   summary != "",
			"cached":        isCached,
		})

	// Log preview of system prompt (avoid logging huge content)
	preview := utils.Truncate(fullSystemPrompt, 500)
	logger.DebugCF("agent", "System prompt preview",
		map[string]any{
			"preview": preview,
		})
	history = sanitizeHistoryForProvider(history)
	messages = append(messages, providers.Message{Role: "system", Content: fullSystemPrompt, SystemParts: contentBlocks})
	messages = append(messages, history...)
	if strings.TrimSpace(currentMessage) != "" {
		msg := providers.Message{Role: "user", Content: currentMessage}
		if len(media) > 0 {
			msg.Media = media
		}
		messages = append(messages, msg)
	}
	return messages
}

func sanitizeHistoryForProvider(history []providers.Message) []providers.Message {
	if len(history) == 0 {
		return history
	}
	sanitized := make([]providers.Message, 0, len(history))
	for _, msg := range history {
		if msg.Role == "user" {
			if normalized, ok := normalizeTemplateCardEventHistory(msg.Content); ok {
				msg.Content = normalized
			}
			if strings.TrimSpace(msg.Content) == "" {
				logger.DebugCF("agent", "Dropping empty user message from history after template-card-event normalization", map[string]any{})
				continue
			}
		}
		switch msg.Role {
		case "system":
			logger.DebugCF("agent", "Dropping system message from history", map[string]any{})
			continue
		case "tool":
			if len(sanitized) == 0 {
				logger.DebugCF("agent", "Dropping orphaned leading tool message", map[string]any{})
				continue
			}
			foundAssistant := false
			for i := len(sanitized) - 1; i >= 0; i-- {
				if sanitized[i].Role == "tool" {
					continue
				}
				if sanitized[i].Role == "assistant" && len(sanitized[i].ToolCalls) > 0 {
					foundAssistant = true
				}
				break
			}
			if !foundAssistant {
				logger.DebugCF("agent", "Dropping orphaned tool message", map[string]any{})
				continue
			}
			sanitized = append(sanitized, msg)
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				if len(sanitized) == 0 {
					logger.DebugCF("agent", "Dropping assistant tool-call turn at history start", map[string]any{})
					continue
				}
				prev := sanitized[len(sanitized)-1]
				if prev.Role != "user" && prev.Role != "tool" {
					logger.DebugCF("agent", "Dropping assistant tool-call turn with invalid predecessor", map[string]any{"prev_role": prev.Role})
					continue
				}
			}
			sanitized = append(sanitized, msg)
		default:
			sanitized = append(sanitized, msg)
		}
	}
	final := make([]providers.Message, 0, len(sanitized))
	for i := 0; i < len(sanitized); i++ {
		msg := sanitized[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			expected := make(map[string]bool, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				expected[tc.ID] = false
			}
			toolMsgCount := 0
			for j := i + 1; j < len(sanitized); j++ {
				if sanitized[j].Role != "tool" {
					break
				}
				toolMsgCount++
				if _, exists := expected[sanitized[j].ToolCallID]; exists {
					expected[sanitized[j].ToolCallID] = true
				}
			}
			allFound := true
			for toolCallID, found := range expected {
				if !found {
					allFound = false
					logger.DebugCF("agent", "Dropping assistant message with incomplete tool results", map[string]any{"missing_tool_call_id": toolCallID, "expected_count": len(expected), "found_count": toolMsgCount})
					break
				}
			}
			if !allFound {
				i += toolMsgCount
				continue
			}
		}
		final = append(final, msg)
	}
	return final
}

func normalizeTemplateCardEventHistory(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", false
	}

	if idx := strings.Index(content, `Equivalent user instruction: "`); idx >= 0 {
		rest := content[idx+len(`Equivalent user instruction: "`):]
		if end := strings.Index(rest, `".`); end >= 0 {
			action := strings.TrimSpace(rest[:end])
			if action != "" {
				return fmt.Sprintf("User clicked template card action: %s.", action), true
			}
		}
	}

	if strings.HasPrefix(content, "User clicked template card action: ") {
		line := content
		if end := strings.Index(line, "\n"); end >= 0 {
			line = line[:end]
		}
		if end := strings.Index(line, " Equivalent user instruction:"); end >= 0 {
			line = line[:end]
		}
		if end := strings.Index(line, " Card context:"); end >= 0 {
			line = line[:end]
		}
		return strings.TrimSpace(line), true
	}

	if strings.HasPrefix(content, "User clicked template card action key: ") {
		line := content
		if end := strings.Index(line, "\n"); end >= 0 {
			line = line[:end]
		}
		if end := strings.Index(line, " Card context:"); end >= 0 {
			line = line[:end]
		}
		return strings.TrimSpace(line), true
	}

	if strings.HasPrefix(content, "Template card event triggered.") {
		if eventKeyIdx := strings.Index(content, "event_key="); eventKeyIdx >= 0 {
			rest := content[eventKeyIdx+len("event_key="):]
			end := strings.Index(rest, ".")
			if end >= 0 {
				rest = rest[:end]
			}
			eventKey := strings.TrimSpace(rest)
			if eventKey != "" {
				return fmt.Sprintf("User clicked template card action key: %s.", eventKey), true
			}
		}
		return "", true
	}

	return content, false
}

func (cb *ContextBuilder) AddToolResult(messages []providers.Message, toolCallID, toolName, result string) []providers.Message {
	messages = append(messages, providers.Message{Role: "tool", Content: result, ToolCallID: toolCallID})
	return messages
}

func (cb *ContextBuilder) AddAssistantMessage(messages []providers.Message, content string, toolCalls []map[string]any) []providers.Message {
	msg := providers.Message{Role: "assistant", Content: content}
	messages = append(messages, msg)
	return messages
}

func (cb *ContextBuilder) GetSkillsInfo() map[string]any {
	allSkills := cb.skillsLoader.ListSkills()
	skillNames := make([]string, 0, len(allSkills))
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
	}
	return map[string]any{"total": len(allSkills), "available": len(allSkills), "names": skillNames}
}
