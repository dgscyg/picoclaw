# How to Configure Authentication

This guide covers configuring authentication for different LLM providers.

## OpenAI Authentication

1. **Browser Flow (Recommended):** Run `picoclaw auth login --provider openai`. A browser window opens for OAuth login. After authorization, tokens are saved automatically.
2. **Device Code Flow (Headless):** Run `picoclaw auth login --provider openai --device-code`. A code is displayed; visit the URL and enter the code. Polling continues for up to 15 minutes.
3. **Verify:** Run `picoclaw auth status` to confirm authentication and check token expiration.

## Anthropic Authentication

1. **API Key (Simplest):** Run `picoclaw auth login --provider anthropic`. Select "API Key" option and paste your key from console.anthropic.com.
2. **Setup Token (OAuth):** Select "Setup Token" option. Paste a token with prefix `sk-ant-oat01-` (minimum 80 characters). These tokens come from Claude CLI OAuth flow.
3. **Verify:** Run `picoclaw auth status` to view authentication method and usage statistics (for OAuth tokens).

## Google Antigravity Authentication

1. **Browser Flow:** Run `picoclaw auth login --provider google-antigravity`. Browser opens for Google Cloud authorization.
2. **Project Selection:** After OAuth, available projects are fetched. Select the target project for Code Assist.
3. **Verify:** Run `picoclaw auth models` to list available models for the selected project.

## Viewing and Managing Credentials

1. **Check Status:** Run `picoclaw auth status` to see all authenticated providers, token expiration, and usage stats.
2. **Logout:** Run `picoclaw auth logout --provider <name>` to remove credentials for a specific provider.
3. **Clear All:** Run `picoclaw auth logout --all` to remove all stored credentials.

## Credential Storage Location

- Default: `~/.picoclaw/auth.json`
- Custom: Set `PICOCLAW_HOME` environment variable to change base directory.
- Permissions: File is stored with 0600 (owner read/write only) permissions.

## Web UI Authentication (Launcher)

For headless servers, use the launcher web UI at `http://localhost:8080`:

1. Navigate to Settings > Authentication.
2. Click login for desired provider.
3. Follow provider-specific flow (paste token or OAuth redirect).
