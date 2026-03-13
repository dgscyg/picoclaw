package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/cron"
)

type fakeCronExecutor struct{}

func (f *fakeCronExecutor) ProcessDirectWithChannel(
	ctx context.Context,
	content, sessionKey, channel, chatID string,
) (string, error) {
	return "ok", nil
}

func newTestCronTool(t *testing.T) (*CronTool, *bus.MessageBus) {
	t.Helper()

	msgBus := bus.NewMessageBus()
	cronService := cron.NewCronService(t.TempDir()+`\\jobs.json`, nil)
	tool, err := NewCronTool(
		cronService,
		&fakeCronExecutor{},
		msgBus,
		t.TempDir(),
		false,
		time.Second,
		&config.Config{},
	)
	if err != nil {
		t.Fatalf("NewCronTool() error = %v", err)
	}
	return tool, msgBus
}

func TestCronTool_AddRejectsToolLikeCommand(t *testing.T) {
	tool, _ := newTestCronTool(t)

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
	tool, msgBus := newTestCronTool(t)

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
	msg, ok := msgBus.SubscribeOutbound(ctx)
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
	tool, _ := newTestCronTool(t)

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
