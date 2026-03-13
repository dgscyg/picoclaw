# Built-in Tools Reference

## 1. Core Summary

PicoClaw includes 15+ built-in tools covering filesystem operations, shell execution, web access, messaging, subagent delegation, skill management, scheduling, and hardware interfaces. All tools implement the `Tool` interface defined in `pkg/tools/base.go:6-11`.

## 2. Source of Truth

- **Primary Code:** `pkg/tools/*.go` - Individual tool implementations
- **Interface:** `pkg/tools/base.go` - Tool interface definition
- **Registration:** `pkg/agent/instance.go` - Tool registration during agent initialization

## 3. Filesystem Tools

### read_file
- **Source:** `pkg/tools/filesystem.go:87-131`
- **Parameters:** `path` (string, required)
- **Returns:** File contents as text
- **Security:** Supports sandbox mode via `os.Root` and whitelist patterns

### write_file
- **Source:** `pkg/tools/filesystem.go:133-186`
- **Parameters:** `path` (string), `content` (string)
- **Returns:** Silent result on success
- **Security:** Atomic write with sync for flash storage reliability

### list_dir
- **Source:** `pkg/tools/filesystem.go:188-232`
- **Parameters:** `path` (string, optional, default ".")
- **Returns:** Directory listing with DIR/FILE prefixes

### edit_file
- **Source:** `pkg/tools/edit.go`
- **Parameters:** `path`, `old_string`, `new_string`
- **Returns:** Edit confirmation with match count

### append_file
- **Source:** `pkg/tools/edit.go`
- **Parameters:** `path`, `content`
- **Returns:** Append confirmation

## 4. Shell Execution

### exec
- **Source:** `pkg/tools/shell.go:19-368`
- **Parameters:** `command` (string, required), `working_dir` (string, optional)
- **Returns:** stdout/stderr output (max 10000 chars)
- **Security:** 30+ deny patterns blocking dangerous commands
- **Timeout:** Configurable via `config.Tools.Exec.TimeoutSeconds`
- **Platform:** Uses PowerShell on Windows, sh on Unix

## 5. Web Tools

### web_search
- **Source:** `pkg/tools/web.go:542-700`
- **Parameters:** `query` (string), `count` (integer, 1-10)
- **Providers:** Perplexity > Brave > SearXNG > Tavily > DuckDuckGo > GLM Search (priority order)
- **Returns:** Search results with titles, URLs, snippets

### web_fetch
- **Source:** `pkg/tools/web.go:702-887`
- **Parameters:** `url` (string), `maxChars` (integer, optional)
- **Returns:** Extracted text with JSON/HTML/raw extraction
- **Security:** Max redirect limit (5), fetch size limit

## 6. Communication Tools

### message
- **Source:** `pkg/tools/message.go:11-103`
- **Parameters:** `content` (string), `channel` (optional), `chat_id` (optional)
- **Returns:** Silent result (message sent directly to user)
- **Context:** Uses `ToolChannel/ToolChatID` from context for routing; on `wecom_official`, `separate_message=true` sends proactive markdown, not template cards

### wecom_card
- **Source:** `pkg/tools/wecom_card.go`
- **Parameters:** Structured enterprise WeCom `template_card` payload fields, including `card_type`, `main_title`, `card_action`, interaction lists, and optional `send`
- **Returns:** Silent result when the card is sent directly to `wecom_official`
- **Context:** Uses `ToolChannel/ToolChatID/ToolReplyTo` from context so callback-scoped sends become official `template_card` replies or `aibot_respond_update_msg` card updates
- **Defaults:** Applies `channels.wecom_official.card.title` as the default branding title when the generated card still contains placeholder `PicoClaw` branding or empty `source.desc`

### send_file
- **Source:** `pkg/tools/send_file.go`
- **Parameters:** `path` (string)
- **Returns:** Media reference for channel delivery
- **MIME Detection:** Uses magic bytes for type detection

## 7. Subagent Tools

### subagent
- **Source:** `pkg/tools/subagent.go`
- **Parameters:** `prompt` (string), `tools` (array, optional)
- **Returns:** Subagent execution result
- **Mode:** Synchronous execution with independent tool registry

### spawn
- **Source:** `pkg/tools/spawn.go`
- **Parameters:** `prompt` (string), `tools` (array, optional)
- **Returns:** Async indication with callback notification
- **Interface:** Implements `AsyncExecutor` for background execution

## 8. Skill Management Tools

### find_skills
- **Source:** `pkg/tools/skills_search.go`
- **Parameters:** `query` (string), `registry` (string, optional)
- **Returns:** List of matching skills from registries

### install_skill
- **Source:** `pkg/tools/skills_install.go`
- **Parameters:** `name` (string), `registry` (string, optional)
- **Security:** Malware blocking, version resolution, origin tracking

## 9. Scheduling Tool

### cron
- **Source:** `pkg/tools/cron.go`
- **Parameters:** `schedule` (string), `action` (string), `message`/`command` (string)
- **Formats:** `at_seconds`, `every_seconds`, cron expressions
- **Returns:** Job ID and schedule confirmation

## 10. Hardware Interface Tools

### i2c
- **Source:** `pkg/tools/i2c.go`, `pkg/tools/i2c_linux.go`, `pkg/tools/i2c_other.go`
- **Parameters:** Various (detect, scan, read, write operations)
- **Platform:** Linux only; stubs on other platforms

### spi
- **Source:** `pkg/tools/spi.go`, `pkg/tools/spi_linux.go`, `pkg/tools/spi_other.go`
- **Parameters:** Various (list, transfer, read operations)
- **Platform:** Linux only; stubs on other platforms

## 11. MCP Tools

### mcp_{server}_{tool}
- **Source:** `pkg/tools/mcp_tool.go:24-247`
- **Naming:** Sanitized with hash suffix for collision avoidance
- **Discovery:** Automatic via `pkg/mcp/manager.go` server connection
- **Transports:** stdio, SSE/HTTP
