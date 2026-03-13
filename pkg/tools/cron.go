package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/cron"
	"github.com/sipeed/picoclaw/pkg/utils"
)

// JobExecutor is the interface for executing cron jobs through the agent
type JobExecutor interface {
	ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string) (string, error)
}

// CronTool provides scheduling capabilities for the agent
type CronTool struct {
	cronService *cron.CronService
	executor    JobExecutor
	msgBus      *bus.MessageBus
	execTool    *ExecTool
}

func scheduledCommandLooksLikeToolInvocation(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	first := strings.TrimSpace(fields[0])
	if first == "" {
		return false
	}
	if strings.ContainsAny(first, `/\:.`) {
		return false
	}
	lower := strings.ToLower(first)
	if strings.HasPrefix(lower, "mcp_") {
		return true
	}
	if strings.HasPrefix(lower, "mcp") && len(fields) > 1 {
		for _, field := range fields[1:] {
			if strings.HasPrefix(strings.TrimSpace(field), "-") {
				return true
			}
		}
	}
	return false
}

func scheduledToolInvocationError(command string) string {
	command = strings.TrimSpace(command)
	return fmt.Sprintf(
		"`cron.command` only supports real shell commands, but %q looks like a PicoClaw/MCP tool invocation. Recreate the job with `deliver=false` and put the natural-language task in `message` so the agent can call tools at runtime, or replace `command` with an actual shell command.",
		command,
	)
}

func cronSchedulesEquivalent(a, b cron.CronSchedule) bool {
	if a.Kind != b.Kind || a.Expr != b.Expr || a.TZ != b.TZ {
		return false
	}
	switch a.Kind {
	case "every":
		if a.EveryMS == nil || b.EveryMS == nil {
			return a.EveryMS == nil && b.EveryMS == nil
		}
		return *a.EveryMS == *b.EveryMS
	case "cron":
		return true
	case "at":
		if a.AtMS == nil || b.AtMS == nil {
			return a.AtMS == nil && b.AtMS == nil
		}
		diff := *a.AtMS - *b.AtMS
		if diff < 0 {
			diff = -diff
		}
		return diff <= 5000
	default:
		return false
	}
}

func findEquivalentCronJob(
	jobs []cron.CronJob,
	schedule cron.CronSchedule,
	message, command string,
	deliver bool,
	channel, chatID string,
) *cron.CronJob {
	for i := range jobs {
		job := &jobs[i]
		if job.Payload.Kind != "agent_turn" {
			continue
		}
		if job.Payload.Message != message || job.Payload.Command != command || job.Payload.Deliver != deliver {
			continue
		}
		if job.Payload.Channel != channel || job.Payload.To != chatID {
			continue
		}
		if !cronSchedulesEquivalent(job.Schedule, schedule) {
			continue
		}
		return job
	}
	return nil
}

func scheduledMessageRequiresAgentTurn(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return false
	}

	taskHints := []string{
		"query ", "fetch ", "get ", "look up ", "check ", "report ", "summarize ",
		"call tool", "use tool", "mcp", "device", "status", "metrics",
		"查询", "获取", "检查", "汇报", "报告", "总结", "调用", "使用工具", "设备状态",
		"实时数据", "关键指标", "如遇", "如果失败", "并以", "并汇报", "并报告",
	}
	for _, hint := range taskHints {
		if strings.Contains(normalized, hint) {
			return true
		}
	}

	if strings.ContainsAny(normalized, "1234567890") &&
		(strings.Contains(normalized, "did") || strings.Contains(normalized, "di(") || strings.Contains(normalized, "di:")) {
		return true
	}

	return false
}

// NewCronTool creates a new CronTool
// execTimeout: 0 means no timeout, >0 sets the timeout duration
func NewCronTool(
	cronService *cron.CronService, executor JobExecutor, msgBus *bus.MessageBus, workspace string, restrict bool,
	execTimeout time.Duration, config *config.Config,
) (*CronTool, error) {
	execTool, err := NewExecToolWithConfig(workspace, restrict, config)
	if err != nil {
		return nil, fmt.Errorf("unable to configure exec tool: %w", err)
	}

	execTool.SetTimeout(execTimeout)
	return &CronTool{
		cronService: cronService,
		executor:    executor,
		msgBus:      msgBus,
		execTool:    execTool,
	}, nil
}

// Name returns the tool name
func (t *CronTool) Name() string {
	return "cron"
}

// Description returns the tool description
func (t *CronTool) Description() string {
	return "Schedule reminders, tasks, or system commands. IMPORTANT: When user asks to be reminded or scheduled, you MUST call this tool. Use 'at_seconds' for one-time reminders (e.g., 'remind me in 10 minutes' → at_seconds=600). Use 'every_seconds' ONLY for recurring tasks (e.g., 'every 2 hours' → every_seconds=7200). Use 'cron_expr' for complex recurring schedules. Use 'command' only for real shell commands such as `df -h` or `dir`; do not put PicoClaw tool names or MCP tool names into `command`. If the scheduled task should call agent tools or MCP tools later, leave `command` empty, keep `deliver=false`, and describe the task in natural language with `message`. For scheduled reports, device polling, monitoring, querying, or summarization, never use `deliver=true`: that would send the task prompt itself to the user instead of executing it."
}

// Parameters returns the tool parameters schema
func (t *CronTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "list", "remove", "enable", "disable"},
				"description": "Action to perform. Use 'add' when user wants to schedule a reminder or task.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "The reminder/task message to display when triggered. If 'command' is used, this describes what the command does.",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Optional: shell command to execute directly (for example `df -h`, `dir`, or `uptime`). This field is only for real OS shell commands. Do not put PicoClaw tool names or MCP tool names here. If the scheduled task should call agent tools later, leave `command` empty and use `message` with `deliver=false` instead. When set, `deliver` will be forced to false.",
			},
			"at_seconds": map[string]any{
				"type":        "integer",
				"description": "One-time reminder: seconds from now when to trigger (e.g., 600 for 10 minutes later). Use this for one-time reminders like 'remind me in 10 minutes'.",
			},
			"every_seconds": map[string]any{
				"type":        "integer",
				"description": "Recurring interval in seconds (e.g., 3600 for every hour). Use this ONLY for recurring tasks like 'every 2 hours' or 'daily reminder'.",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "Cron expression for complex recurring schedules (e.g., '0 9 * * *' for daily at 9am). Use this for complex recurring schedules.",
			},
			"job_id": map[string]any{
				"type":        "string",
				"description": "Job ID (for remove/enable/disable)",
			},
			"deliver": map[string]any{
				"type":        "boolean",
				"description": "If true, send `message` as the exact literal text directly to the channel. Use this only for fixed reminder text such as `喝水提醒` or `Stand up and stretch`. If the scheduled job must query data, call tools, inspect devices, summarize results, or decide what to send at runtime, this must be false so the agent can process the task first. Default: true",
			},
		},
		"required": []string{"action"},
	}
}

// Execute runs the tool with the given arguments
func (t *CronTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	action, ok := args["action"].(string)
	if !ok {
		return ErrorResult("action is required")
	}

	switch action {
	case "add":
		return t.addJob(ctx, args)
	case "list":
		return t.listJobs()
	case "remove":
		return t.removeJob(args)
	case "enable":
		return t.enableJob(args, true)
	case "disable":
		return t.enableJob(args, false)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *CronTool) addJob(ctx context.Context, args map[string]any) *ToolResult {
	channel := ToolChannel(ctx)
	chatID := ToolChatID(ctx)

	if channel == "" || chatID == "" {
		return ErrorResult("no session context (channel/chat_id not set). Use this tool in an active conversation.")
	}

	message, ok := args["message"].(string)
	if !ok || message == "" {
		return ErrorResult("message is required for add")
	}

	var schedule cron.CronSchedule

	// Check for at_seconds (one-time), every_seconds (recurring), or cron_expr
	atSeconds, hasAt := args["at_seconds"].(float64)
	everySeconds, hasEvery := args["every_seconds"].(float64)
	cronExpr, hasCron := args["cron_expr"].(string)

	// Fix: type assertions return true for zero values, need additional validity checks
	// This prevents LLMs that fill unused optional parameters with defaults (0) from triggering wrong type
	hasAt = hasAt && atSeconds > 0
	hasEvery = hasEvery && everySeconds > 0
	hasCron = hasCron && cronExpr != ""

	// Priority: at_seconds > every_seconds > cron_expr
	if hasAt {
		atMS := time.Now().UnixMilli() + int64(atSeconds)*1000
		schedule = cron.CronSchedule{
			Kind: "at",
			AtMS: &atMS,
		}
	} else if hasEvery {
		everyMS := int64(everySeconds) * 1000
		schedule = cron.CronSchedule{
			Kind:    "every",
			EveryMS: &everyMS,
		}
	} else if hasCron {
		schedule = cron.CronSchedule{
			Kind: "cron",
			Expr: cronExpr,
		}
	} else {
		return ErrorResult("one of at_seconds, every_seconds, or cron_expr is required")
	}

	// Read deliver parameter, default to true
	deliver := true
	if d, ok := args["deliver"].(bool); ok {
		deliver = d
	}

	command, _ := args["command"].(string)
	if command != "" {
		if scheduledCommandLooksLikeToolInvocation(command) {
			return ErrorResult(scheduledToolInvocationError(command))
		}
		// Commands must be processed by agent/exec tool, so deliver must be false (or handled specifically)
		// Actually, let's keep deliver=false to let the system know it's not a simple chat message
		// But for our new logic in ExecuteJob, we can handle it regardless of deliver flag if Payload.Command is set.
		// However, logically, it's not "delivered" to chat directly as is.
		deliver = false
	}
	if deliver && command == "" && scheduledMessageRequiresAgentTurn(message) {
		deliver = false
	}

	// Truncate message for job name (max 30 chars)
	messagePreview := utils.Truncate(message, 30)

	if existing := findEquivalentCronJob(
		t.cronService.ListJobs(true),
		schedule,
		message,
		command,
		deliver,
		channel,
		chatID,
	); existing != nil {
		if existing.Enabled {
			return SilentResult(fmt.Sprintf("Cron job already exists: %s (id: %s)", existing.Name, existing.ID))
		}
		reenabled := t.cronService.EnableJob(existing.ID, true)
		if reenabled == nil {
			return ErrorResult(fmt.Sprintf("Error re-enabling duplicate cron job: %s", existing.ID))
		}
		return SilentResult(fmt.Sprintf("Cron job re-enabled: %s (id: %s)", reenabled.Name, reenabled.ID))
	}

	job, err := t.cronService.AddJob(
		messagePreview,
		schedule,
		message,
		deliver,
		channel,
		chatID,
	)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Error adding job: %v", err))
	}

	if command != "" {
		job.Payload.Command = command
		// Need to save the updated payload
		t.cronService.UpdateJob(job)
	}

	if !deliver && command == "" && scheduledMessageRequiresAgentTurn(message) {
		return SilentResult(fmt.Sprintf("Cron job added: %s (id: %s, agent processing enforced)", job.Name, job.ID))
	}
	return SilentResult(fmt.Sprintf("Cron job added: %s (id: %s)", job.Name, job.ID))
}

func (t *CronTool) listJobs() *ToolResult {
	jobs := t.cronService.ListJobs(false)

	if len(jobs) == 0 {
		return SilentResult("No scheduled jobs")
	}

	var result strings.Builder
	result.WriteString("Scheduled jobs:\n")
	for _, j := range jobs {
		var scheduleInfo string
		if j.Schedule.Kind == "every" && j.Schedule.EveryMS != nil {
			scheduleInfo = fmt.Sprintf("every %ds", *j.Schedule.EveryMS/1000)
		} else if j.Schedule.Kind == "cron" {
			scheduleInfo = j.Schedule.Expr
		} else if j.Schedule.Kind == "at" {
			scheduleInfo = "one-time"
		} else {
			scheduleInfo = "unknown"
		}
		result.WriteString(fmt.Sprintf("- %s (id: %s, %s)\n", j.Name, j.ID, scheduleInfo))
	}

	return SilentResult(result.String())
}

func (t *CronTool) removeJob(args map[string]any) *ToolResult {
	jobID, ok := args["job_id"].(string)
	if !ok || jobID == "" {
		return ErrorResult("job_id is required for remove")
	}

	if t.cronService.RemoveJob(jobID) {
		return SilentResult(fmt.Sprintf("Cron job removed: %s", jobID))
	}
	return ErrorResult(fmt.Sprintf("Job %s not found", jobID))
}

func (t *CronTool) enableJob(args map[string]any, enable bool) *ToolResult {
	jobID, ok := args["job_id"].(string)
	if !ok || jobID == "" {
		return ErrorResult("job_id is required for enable/disable")
	}

	job := t.cronService.EnableJob(jobID, enable)
	if job == nil {
		return ErrorResult(fmt.Sprintf("Job %s not found", jobID))
	}

	status := "enabled"
	if !enable {
		status = "disabled"
	}
	return SilentResult(fmt.Sprintf("Cron job '%s' %s", job.Name, status))
}

// ExecuteJob executes a cron job through the agent
func (t *CronTool) ExecuteJob(ctx context.Context, job *cron.CronJob) string {
	// Get channel/chatID from job payload
	channel := job.Payload.Channel
	chatID := job.Payload.To

	// Default values if not set
	if channel == "" {
		channel = "cli"
	}
	if chatID == "" {
		chatID = "direct"
	}

	// Execute command if present
	if job.Payload.Command != "" {
		if scheduledCommandLooksLikeToolInvocation(job.Payload.Command) {
			output := "Error executing scheduled command: " + scheduledToolInvocationError(job.Payload.Command)
			pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer pubCancel()
			t.msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
				Channel: channel,
				ChatID:  chatID,
				Content: output,
			})
			return "ok"
		}

		args := map[string]any{
			"command": job.Payload.Command,
		}

		result := t.execTool.Execute(ctx, args)
		var output string
		if result.IsError {
			output = fmt.Sprintf("Error executing scheduled command: %s", result.ForLLM)
		} else {
			output = fmt.Sprintf("Scheduled command '%s' executed:\n%s", job.Payload.Command, result.ForLLM)
		}

		pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pubCancel()
		t.msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: output,
		})
		return "ok"
	}

	// If deliver=true, send message directly without agent processing
	if job.Payload.Deliver {
		pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pubCancel()
		t.msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: job.Payload.Message,
		})
		return "ok"
	}

	// For deliver=false, process through agent (for complex tasks)
	sessionKey := fmt.Sprintf("cron-%s", job.ID)

	// Call agent with job's message
	response, err := t.executor.ProcessDirectWithChannel(
		ctx,
		job.Payload.Message,
		sessionKey,
		channel,
		chatID,
	)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if strings.TrimSpace(response) != "" {
		pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pubCancel()
		t.msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: response,
		})
	}

	// Cron-scoped agent turns usually notify users through explicit tools such as
	// `message`. When they instead return a direct final answer, ExecuteJob now
	// publishes that answer itself because AgentLoop.ProcessDirectWithChannel
	// runs with SendResponse disabled.
	return "ok"
}
