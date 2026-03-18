package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/cron"
)

type fakeCronExecutor struct {
	response string
	err      error
}

func (f *fakeCronExecutor) ProcessDirectWithChannel(
	ctx context.Context,
	content, sessionKey, channel, chatID string,
) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.response != "" {
		return f.response, nil
	}
	return "ok", nil
}

func newTestCronToolWithConfig(t *testing.T, cfg *config.Config) *CronTool {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "cron.json")
	cronService := cron.NewCronService(storePath, nil)
	msgBus := bus.NewMessageBus()
	tool, err := NewCronTool(
		cronService,
		&fakeCronExecutor{response: "ok"},
		msgBus,
		t.TempDir(),
		true,
		time.Second,
		cfg,
	)
	if err != nil {
		t.Fatalf("NewCronTool() error: %v", err)
	}
	return tool
}

func newTestCronToolWithExecutor(t *testing.T, executor JobExecutor) *CronTool {
	t.Helper()
	cfg := config.DefaultConfig()
	storePath := filepath.Join(t.TempDir(), "cron.json")
	cronService := cron.NewCronService(storePath, nil)
	msgBus := bus.NewMessageBus()
	tool, err := NewCronTool(cronService, executor, msgBus, t.TempDir(), true, time.Second, cfg)
	if err != nil {
		t.Fatalf("NewCronTool() error = %v", err)
	}
	return tool
}

func newTestCronTool(t *testing.T) *CronTool {
	t.Helper()
	return newTestCronToolWithConfig(t, config.DefaultConfig())
}

func TestCronTool_CommandBlockedFromRemoteChannel(t *testing.T) {
	tool := newTestCronTool(t)
	ctx := WithToolContext(context.Background(), "telegram", "chat-1")
	result := tool.Execute(ctx, map[string]any{
		"action":          "add",
		"message":         "check disk",
		"command":         "df -h",
		"command_confirm": true,
		"at_seconds":      float64(60),
	})

	if !result.IsError {
		t.Fatal("expected command scheduling to be blocked from remote channel")
	}
	if !strings.Contains(result.ForLLM, "restricted to internal channels") {
		t.Errorf("expected 'restricted to internal channels', got: %s", result.ForLLM)
	}
}

func TestCronTool_CommandDoesNotRequireConfirmByDefault(t *testing.T) {
	tool := newTestCronTool(t)
	ctx := WithToolContext(context.Background(), "cli", "direct")
	result := tool.Execute(ctx, map[string]any{
		"action":     "add",
		"message":    "check disk",
		"command":    "df -h",
		"at_seconds": float64(60),
	})

	if result.IsError {
		t.Fatalf("expected command scheduling without confirm to succeed by default, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Cron job added") {
		t.Errorf("expected 'Cron job added', got: %s", result.ForLLM)
	}
}

func TestCronTool_CommandRequiresConfirmWhenAllowCommandDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Cron.AllowCommand = false

	tool := newTestCronToolWithConfig(t, cfg)
	ctx := WithToolContext(context.Background(), "cli", "direct")
	result := tool.Execute(ctx, map[string]any{
		"action":     "add",
		"message":    "check disk",
		"command":    "df -h",
		"at_seconds": float64(60),
	})

	if !result.IsError {
		t.Fatal("expected command scheduling to require confirm when allow_command is disabled")
	}
	if !strings.Contains(result.ForLLM, "command_confirm=true") {
		t.Errorf("expected command_confirm requirement message, got: %s", result.ForLLM)
	}
}

func TestCronTool_CommandAllowedWithConfirmWhenAllowCommandDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Cron.AllowCommand = false

	tool := newTestCronToolWithConfig(t, cfg)
	ctx := WithToolContext(context.Background(), "cli", "direct")
	result := tool.Execute(ctx, map[string]any{
		"action":          "add",
		"message":         "check disk",
		"command":         "df -h",
		"command_confirm": true,
		"at_seconds":      float64(60),
	})

	if result.IsError {
		t.Fatalf("expected command scheduling with confirm to succeed when allow_command is disabled, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Cron job added") {
		t.Errorf("expected 'Cron job added', got: %s", result.ForLLM)
	}
}

func TestCronTool_CommandBlockedWhenExecDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Exec.Enabled = false

	tool := newTestCronToolWithConfig(t, cfg)
	ctx := WithToolContext(context.Background(), "cli", "direct")
	result := tool.Execute(ctx, map[string]any{
		"action":          "add",
		"message":         "check disk",
		"command":         "df -h",
		"command_confirm": true,
		"at_seconds":      float64(60),
	})

	if !result.IsError {
		t.Fatal("expected command scheduling to be blocked when exec is disabled")
	}
	if !strings.Contains(result.ForLLM, "command execution is disabled") {
		t.Errorf("expected exec disabled message, got: %s", result.ForLLM)
	}
}

func TestCronTool_CommandAllowedFromInternalChannel(t *testing.T) {
	tool := newTestCronTool(t)
	ctx := WithToolContext(context.Background(), "cli", "direct")
	result := tool.Execute(ctx, map[string]any{
		"action":          "add",
		"message":         "check disk",
		"command":         "df -h",
		"command_confirm": true,
		"at_seconds":      float64(60),
	})

	if result.IsError {
		t.Fatalf("expected command scheduling to succeed from internal channel, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Cron job added") {
		t.Errorf("expected 'Cron job added', got: %s", result.ForLLM)
	}
}

func TestCronTool_AddJobRequiresSessionContext(t *testing.T) {
	tool := newTestCronTool(t)
	result := tool.Execute(context.Background(), map[string]any{
		"action":     "add",
		"message":    "reminder",
		"at_seconds": float64(60),
	})

	if !result.IsError {
		t.Fatal("expected error when session context is missing")
	}
	if !strings.Contains(result.ForLLM, "no session context") {
		t.Errorf("expected 'no session context' message, got: %s", result.ForLLM)
	}
}

func TestCronTool_NonCommandJobAllowedFromRemoteChannel(t *testing.T) {
	tool := newTestCronTool(t)
	ctx := WithToolContext(context.Background(), "telegram", "chat-1")
	result := tool.Execute(ctx, map[string]any{
		"action":     "add",
		"message":    "time to stretch",
		"at_seconds": float64(600),
	})

	if result.IsError {
		t.Fatalf("expected non-command reminder to succeed from remote channel, got: %s", result.ForLLM)
	}
}

func TestCronTool_NonCommandJobDefaultsDeliverToFalse(t *testing.T) {
	tool := newTestCronTool(t)
	ctx := WithToolContext(context.Background(), "telegram", "chat-1")
	result := tool.Execute(ctx, map[string]any{
		"action":     "add",
		"message":    "send me a poem",
		"at_seconds": float64(600),
	})

	if result.IsError {
		t.Fatalf("expected non-command reminder to succeed, got: %s", result.ForLLM)
	}

	jobs := tool.cronService.ListJobs(false)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Payload.Deliver {
		t.Fatal("expected deliver=false by default for non-command jobs")
	}
}

func TestCronTool_ExecuteJobPublishesErrorWhenExecDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Exec.Enabled = false

	tool := newTestCronToolWithConfig(t, cfg)
	job := &cron.CronJob{}
	job.Payload.Channel = "cli"
	job.Payload.To = "direct"
	job.Payload.Command = "df -h"

	if got := tool.ExecuteJob(context.Background(), job); got != "ok" {
		t.Fatalf("ExecuteJob() = %q, want ok", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msg, ok := tool.msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("timeout waiting for outbound message")
	}
	if !strings.Contains(msg.Content, "command execution is disabled") {
		t.Fatalf("expected exec disabled message, got: %s", msg.Content)
	}
}

func TestCronTool_AddRejectsToolLikeCommand(t *testing.T) {
	tool := newTestCronTool(t)

	ctx := WithToolContext(context.Background(), "wecom_official", "YangXu")
	result := tool.Execute(ctx, map[string]any{
		"action":     "add",
		"message":    "query device status",
		"command":    "mcparmanddl645tgetdevicecurrentdata -request '[{\"did\":\"6872176FF500\"}]'",
		"at_seconds": float64(60),
	})

	if !result.IsError {
		t.Fatal("expected add to reject tool-like command")
	}
	if !strings.Contains(result.ForLLM, "`cron.command` only supports real shell commands") {
		t.Fatalf("unexpected error: %q", result.ForLLM)
	}
}

func TestCronTool_ExecuteJobToolLikeCommandSendsHelpfulError(t *testing.T) {
	tool := newTestCronTool(t)

	job := &cron.CronJob{
		ID:   "job-1",
		Name: "bad-tool-command",
		Payload: cron.CronPayload{
			Command: "mcparmanddl645tgetdevicecurrentdata -request '[{\"did\":\"6872176FF500\"}]'",
			Channel: "wecom_official",
			To:      "YangXu",
		},
	}

	if got := tool.ExecuteJob(context.Background(), job); got != "ok" {
		t.Fatalf("ExecuteJob() = %q, want ok", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := tool.msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected helpful outbound error message")
	}
	if msg.Channel != "wecom_official" || msg.ChatID != "YangXu" {
		t.Fatalf("unexpected outbound target: %#v", msg)
	}
	if !strings.Contains(msg.Content, "`cron.command` only supports real shell commands") {
		t.Fatalf("unexpected outbound content: %q", msg.Content)
	}
	if strings.Contains(msg.Content, "CommandNotFoundException") {
		t.Fatalf("expected sanitized error, got raw shell failure: %q", msg.Content)
	}
}

func TestCronTool_AddDeduplicatesEquivalentRecurringJob(t *testing.T) {
	tool := newTestCronTool(t)

	ctx := WithToolContext(context.Background(), "wecom_official", "YangXu")
	args := map[string]any{
		"action":        "add",
		"message":       "查询空调设备状态并报告",
		"deliver":       false,
		"every_seconds": float64(60),
	}

	first := tool.Execute(ctx, args)
	if first.IsError {
		t.Fatalf("first add failed: %s", first.ForLLM)
	}
	second := tool.Execute(ctx, args)
	if second.IsError {
		t.Fatalf("second add failed: %s", second.ForLLM)
	}
	if !strings.Contains(second.ForLLM, "already exists") {
		t.Fatalf("expected duplicate add to report existing job, got %q", second.ForLLM)
	}

	jobs := tool.cronService.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 stored job after duplicate add, got %d", len(jobs))
	}
}

func TestCronTool_AddForcesAgentTurnForScheduledReport(t *testing.T) {
	tool := newTestCronTool(t)

	ctx := WithToolContext(context.Background(), "wecom_official", "YangXu")
	result := tool.Execute(ctx, map[string]any{
		"action":        "add",
		"message":       "查询会议室空调设备状态 (DID:6872176FF500)：获取当前温度(02800007)、设定温度(040E001B)、运行状态(E1010103)、风速、模式、电源状态等关键数据，并以清晰的格式汇报给用户",
		"deliver":       true,
		"every_seconds": float64(60),
	})

	if result.IsError {
		t.Fatalf("add failed: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "agent processing enforced") {
		t.Fatalf("expected add result to mention enforced agent processing, got %q", result.ForLLM)
	}

	jobs := tool.cronService.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 stored job, got %d", len(jobs))
	}
	if jobs[0].Payload.Deliver {
		t.Fatalf("expected scheduled report job to force deliver=false, got deliver=true")
	}
}

func TestCronTool_ExecuteJobPublishesDirectAgentResponse(t *testing.T) {
	tool := newTestCronToolWithExecutor(t, &fakeCronExecutor{response: "空调状态正常"})

	job := &cron.CronJob{
		ID:   "job-agent-response",
		Name: "scheduled-agent-turn",
		Payload: cron.CronPayload{
			Message: "查询会议室空调设备状态并汇报",
			Deliver: false,
			Channel: "wecom_official",
			To:      "YangXu",
		},
	}

	if got := tool.ExecuteJob(context.Background(), job); got != "ok" {
		t.Fatalf("ExecuteJob() = %q, want ok", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := tool.msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound scheduled report message")
	}
	if msg.Channel != "wecom_official" || msg.ChatID != "YangXu" {
		t.Fatalf("unexpected outbound target: %#v", msg)
	}
	if got, want := msg.Content, "空调状态正常"; got != want {
		t.Fatalf("msg.Content = %q, want %q", got, want)
	}
}
