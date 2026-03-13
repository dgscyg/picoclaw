# Cron & Heartbeat System Architecture

## 1. Identity

- **What it is:** PicoClaw's scheduling subsystem providing persistent cron jobs and periodic heartbeat checks.
- **Purpose:** Enable automated task scheduling, reminders, and proactive agent behavior at configurable intervals.

## 2. Core Components

- `pkg/cron/service.go` (CronService, CronJob, CronSchedule, CronPayload, CronJobState): Core cron engine with JSON persistence, supporting one-time, interval, and cron-expression schedules.
- `pkg/heartbeat/service.go` (HeartbeatService, HeartbeatHandler): Periodic heartbeat service reading tasks from HEARTBEAT.md, executing via agent with activity-aware skip logic.
- `pkg/tools/cron.go` (CronTool, JobExecutor): Tool interface for agent to manage cron jobs via add/list/remove/enable/disable actions.
- `pkg/tools/spawn.go` (SpawnTool, AsyncExecutor): Async subagent spawner for background task execution.
- `pkg/tools/subagent.go` (SubagentManager, SubagentTask, SubagentTool): Subagent lifecycle management using shared RunToolLoop.
- `pkg/tools/toolloop.go` (RunToolLoop, ToolLoopConfig, ToolLoopResult): Shared LLM+tool execution loop for main agent and subagents.
- `pkg/state/state.go` (Manager): Persistent state for last channel tracking used by heartbeat targeting.
- `pkg/config/config.go` (HeartbeatConfig, CronToolsConfig): Configuration structures with defaults.

## 3. Execution Flow (LLM Retrieval Map)

### Cron Job Lifecycle

- **1. Initialization:** `cmd/picoclaw/internal/gateway/helpers.go:221-255` - `setupCronTool` creates CronService with store path, registers CronTool with AgentLoop, sets JobHandler callback.
- **2. Persistence:** `pkg/cron/service.go:338-362` - Jobs stored in `workspace/cron/jobs.json` with atomic writes via `fileutil.WriteFileAtomic`; Windows-safe replacement is handled in `pkg/fileutil`.
- **3. Scheduling Loop:** `pkg/cron/service.go:121-133` - `runLoop` uses 1-second ticker, calls `checkJobs()` each tick.
- **4. Job Detection:** `pkg/cron/service.go:135-175` - `checkJobs` collects due jobs (NextRunAtMS <= now), nullifies NextRunAtMS to prevent re-execution, saves store only when due jobs exist, then executes outside lock.
- **5. Job Execution:** `pkg/cron/service.go:177-264` - `executeJobByID` calls JobHandler, updates LastRunAtMS/LastStatus/LastError, computes next run for recurring jobs.
- **6. Next Run Computation:** `pkg/cron/service.go:266-300` - `computeNextRun` handles three schedule kinds: "at" (one-time), "every" (interval), "cron" (gronx expression parsing).

### Cron Tool Actions

- **1. Add Job:** `pkg/tools/cron.go` - `addJob` creates CronSchedule from at_seconds/every_seconds/cron_expr, rejects MCP-like shell misuse in `command`, deduplicates equivalent jobs by schedule + payload + target, and forcibly downgrades `deliver=true` to `deliver=false` when the scheduled message reads like an agent task (query/report/device/tool execution) rather than a fixed reminder string.
- **2. Execute Job:** `pkg/tools/cron.go` - `ExecuteJob` routes to: (a) ExecTool for shell commands, (b) MessageBus for literal direct delivery, or (c) AgentLoop.ProcessDirectWithChannel for agent processing with a dedicated `cron-<jobID>` session key. If the scheduled agent turn returns a direct final answer instead of calling `message`, `ExecuteJob` now publishes that answer itself to the target channel.

### Heartbeat Lifecycle

- **1. Initialization:** `cmd/picoclaw/internal/gateway/helpers.go:97-106` - Creates HeartbeatService with workspace, interval, enabled flag; sets handler and MessageBus.
- **2. Loop:** `pkg/heartbeat/service.go:133-154` - `runLoop` with startup delay cap (30s), periodic execution per interval (min 5 min, default 30 min).
- **3. Target Resolution:** `pkg/heartbeat/service.go:227-254` - `resolveTarget` gets last external channel from state, skips if recent activity (<15s grace period).
- **4. Prompt Building:** `pkg/heartbeat/service.go:257-286` - `buildPrompt` reads workspace/HEARTBEAT.md, wraps with heartbeat context and current time.
- **5. Execution:** `pkg/heartbeat/service.go:156-225` - `executeHeartbeat` calls HeartbeatHandler (AgentLoop.ProcessHeartbeat), handles async/silent/error results.
- **6. Response Delivery:** `pkg/heartbeat/service.go:324-357` - `sendResponse` publishes via MessageBus to last active channel.

### Spawn (Async Subagent) Flow

- **1. Tool Invocation:** `pkg/tools/spawn.go:66-106` - `execute` validates task, checks allowlist via `allowlistCheck`, calls SubagentManager.Spawn with callback.
- **2. Task Creation:** `pkg/tools/subagent.go:79-109` - `Spawn` creates SubagentTask with ID, starts `runTask` goroutine.
- **3. Execution:** `pkg/tools/subagent.go:111-213` - `runTask` builds subagent system prompt, invokes `RunToolLoop` with tool registry, handles context cancellation.
- **4. Completion:** Callback invoked with ToolResult (Async=true indicates background completion).

## 4. Design Rationale

### Schedule Types
- **"at" (one-time):** DeleteAfterRun=true, job removed or disabled after first execution.
- **"every" (interval):** NextRun computed as now + EveryMS.
- **"cron" (expression):** Uses gronx library for next tick calculation with timezone support.

### Activity Awareness
- Heartbeat skips execution if user activity detected within 15-second grace period (`pkg/heartbeat/service.go:244`).
- Prevents interrupting active conversations with proactive messages.

### Async Pattern
- SpawnTool returns `AsyncResult` immediately; completion notification via callback.
- SubagentTool provides synchronous alternative for blocking execution.
- Both use shared `RunToolLoop` for consistent LLM+tool behavior.

### Integration Points
- CronService.SetOnJob callback wired to CronTool.ExecuteJob (`helpers.go:248-251`).
- HeartbeatHandler callback wired to AgentLoop.ProcessHeartbeat.
- Both services share MessageBus for outbound message delivery.
- `agent_turn` cron jobs should notify users through explicit tools such as `message`; after a direct tool send, cron-scoped sessions suppress trailing LLM narration so scheduled reports do not append unrelated fallback chat text. When the agent instead returns a non-empty direct final answer, the cron layer must publish that answer because the direct-processing path itself runs with `SendResponse=false`.
