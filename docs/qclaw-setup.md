# QClaw Channel - WeChat Service Account Integration

## Overview

QClaw channel enables PicoClaw to integrate with WeChat service accounts through the QClaw AGP (Agent Gateway Protocol). This allows users to interact with the AI assistant via WeChat messages.

## Quick Start

### 1. Login

```bash
picoclaw qclaw login
```

This command will:
1. Request a login state from QClaw JPRX gateway
2. Display a QR code for WeChat scanning
3. Wait for you to scan with WeChat
4. Paste the auth code from the redirect URL
5. Save credentials to `~/.picoclaw/qclaw-auth.json`

### 2. Check Status

```bash
picoclaw qclaw status
```

### 3. Configure PicoClaw

Add to your `config.json`:

```json
{
  "channels": {
    "qclaw": {
      "enabled": true,
      "token": "your-channel-token-from-qclaw"
    }
  }
}
```

### 4. Start PicoClaw

```bash
picoclaw gateway
```

## Configuration

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Enable the QClaw channel |
| `token` | string | WebSocket authentication token from QClaw |

### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `websocket_url` | string | `wss://mmgrcalltoken.3g.qq.com/agentwss` | WebSocket gateway URL |
| `guid` | string | auto-generated | Device unique identifier |
| `user_id` | string | from login | User account ID |
| `environment` | string | `production` | `production` or `test` |
| `auth_state_path` | string | `~/.picoclaw/qclaw-auth.json` | Path to auth state file |
| `allow_from` | []string | `[]` | Allowed user IDs (empty = all allowed) |
| `heartbeat_interval` | int | `20` | WebSocket heartbeat interval in seconds |
| `reconnect_interval` | int | `3` | Base reconnection delay in seconds |
| `max_reconnects` | int | `0` | Max reconnect attempts (0 = unlimited) |

### Full Configuration Example

```json
{
  "channels": {
    "qclaw": {
      "enabled": true,
      "token": "your-websocket-token",
      "websocket_url": "wss://mmgrcalltoken.3g.qq.com/agentwss",
      "guid": "your-device-guid",
      "user_id": "your-user-id",
      "environment": "production",
      "allow_from": ["user1", "user2"],
      "group_trigger": {
        "mention_only": false,
        "prefixes": ["/ai", "/bot"]
      },
      "heartbeat_interval": 20,
      "reconnect_interval": 3,
      "max_reconnects": 0
    }
  }
}
```

## CLI Commands

### `picoclaw qclaw login`

Authenticate with QClaw via WeChat QR code.

**Flags:**
- `--environment, -e`: Environment to use (`production` or `test`)
- `--bypass-invite`: Skip invite code verification
- `--auth-path`: Custom path for auth state storage

**Example:**
```bash
picoclaw qclaw login --environment test
```

### `picoclaw qclaw logout`

Clear stored authentication credentials.

**Flags:**
- `--auth-path`: Custom path for auth state storage

### `picoclaw qclaw status`

Display current authentication status.

**Flags:**
- `--auth-path`: Custom path for auth state storage

## Architecture

### Message Flow

```
WeChat User
    ↓ (message)
QClaw Gateway
    ↓ (session.prompt via WebSocket)
PicoClaw QClaw Channel
    ↓ (InboundMessage)
MessageBus
    ↓ (agent processing)
PicoClaw Agent
    ↓ (OutboundMessage)
QClaw Channel
    ↓ (session.update chunks)
QClaw Gateway
    ↓ (session.promptResponse on idle)
WeChat User
```

### AGP Protocol

| Method | Direction | Purpose |
|--------|-----------|---------|
| `session.prompt` | Server → Client | User message to process |
| `session.cancel` | Server → Client | Cancel ongoing request |
| `session.update` | Client → Server | Streaming text chunks |
| `session.promptResponse` | Client → Server | Final response |

### Key Components

1. **WebSocket Client** (`websocket.go`)
   - Heartbeat: 20s ping interval
   - Reconnection: Exponential backoff (3s base, 25s max)
   - Wakeup Detection: 15s threshold for system sleep detection
   - Message Deduplication: LRU cache for msg_id

2. **QClaw Channel** (`qclaw.go`)
   - Reactive messaging (responds to prompts only)
   - Streaming responses with idle timeout
   - Prompt task tracking for response correlation

3. **Authentication** (`auth.go`)
   - JPRX Gateway API client
   - OAuth 2.0 with WeChat
   - Token persistence

## Troubleshooting

### Connection Issues

1. **WebSocket fails to connect**
   - Check `token` configuration
   - Verify `websocket_url` is correct
   - Check network connectivity

2. **Authentication failed**
   - Run `picoclaw qclaw login` to refresh token
   - Check `auth_state_path` permissions

3. **Messages not received**
   - Check `allow_from` configuration
   - Verify channel is enabled

### Debug Logging

Enable debug logging for gateway:

```bash
picoclaw gateway --debug
```

## Environment Variables

All configuration fields can be set via environment variables:

```bash
PICOCLAW_CHANNELS_QCLAW_ENABLED=true
PICOCLAW_CHANNELS_QCLAW_TOKEN=your-token
PICOCLAW_CHANNELS_QCLAW_WEBSOCKET_URL=wss://...
PICOCLAW_CHANNELS_QCLAW_GUID=your-guid
PICOCLAW_CHANNELS_QCLAW_USER_ID=your-user-id
```
