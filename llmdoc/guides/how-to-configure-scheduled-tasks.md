# How to Configure Scheduled Tasks

This guide covers configuring cron jobs and heartbeat tasks in PicoClaw.

## 1. Enable/Disable Services

Configure in `config.json` under the `heartbeat` and `tools.cron` sections:

```json
{
  "heartbeat": {
    "enabled": true,
    "interval": 30
  },
  "tools": {
    "cron": {
      "enabled": true,
      "exec_timeout_minutes": 5
    }
  }
}
```

Environment variables:
- `PICOCLAW_HEARTBEAT_ENABLED`
- `PICOCLAW_HEARTBEAT_INTERVAL` (minutes, minimum 5)
- `PICOCLAW_TOOLS_CRON_ENABLED`
- `PICOCLAW_TOOLS_CRON_EXEC_TIMEOUT_MINUTES`

## 2. Configure Heartbeat Tasks

1. **Create/Edit HEARTBEAT.md** in workspace directory.
2. Add task instructions in markdown format.
3. Service reads file each interval, executes via agent.

Example `workspace/HEARTBEAT.md`:
```markdown
## Daily Tasks
- Check for system updates
- Review calendar events

## Hourly Tasks
- Monitor device status
```

Heartbeat behavior:
- Minimum interval: 5 minutes
- Default interval: 30 minutes
- Startup delay capped at 30 seconds
- Skips if user activity within 15 seconds

## 3. Create Cron Jobs via Agent

Use the cron tool through conversation:

**One-time reminder (in 10 minutes):**
```
Remind me to take a break in 10 minutes
```

**Recurring task (every 2 hours):**
```
Check server status every 2 hours
```

**Cron expression (daily at 9am):**
```
Send me a daily summary at 9am using cron expression "0 9 * * *"
```

**Shell command execution:**
```
Run "df -h" every hour and send me the output
```

## 4. Manage Existing Jobs

**List jobs:**
```
Show me my scheduled tasks
```

**Remove job:**
```
Cancel the reminder with job ID abc123
```

**Disable/Enable:**
```
Disable job abc123
Enable job abc123
```

## 5. Storage Location

- Cron jobs: `workspace/cron/jobs.json`
- Heartbeat log: `workspace/heartbeat.log`
- State (last channel): `workspace/state/last_channel.json`

## 6. Verify Configuration

Check logs for service startup:
```
[cron] service started
[heartbeat] Heartbeat service started interval_minutes=30
```

Test heartbeat by checking `heartbeat.log` after interval passes.
