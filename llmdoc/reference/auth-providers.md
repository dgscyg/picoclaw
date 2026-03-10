# Authentication Providers Reference

## 1. Core Summary

PicoClaw supports three authentication methods: OAuth 2.0 with PKCE, Device Code Flow, and Direct Token Paste. Provider-specific implementations handle OpenAI, Anthropic, and Google Antigravity authentication with automatic token refresh and secure credential storage.

## 2. Supported Providers

### OpenAI

- **Method:** OAuth 2.0 with PKCE (browser or device code flow)
- **Issuer:** `https://auth.openai.com`
- **Callback Port:** 1455
- **Token Types:** Access token, refresh token
- **Account ID Extraction:** JWT claims (`chatgpt_account_id`, `organizations`)

### Anthropic

- **Method 1:** Direct API Key - Paste from console.anthropic.com
- **Method 2:** Setup Token - OAuth-derived tokens with prefix `sk-ant-oat01-`
- **OAuth Usage Stats:** `pkg/auth/anthropic_usage.go` fetches 5-hour and 7-day utilization
- **Streaming:** OAuth tokens use streaming API with `anthropic-beta` header

### Google Antigravity (Code Assist)

- **Method:** OAuth 2.0 with PKCE
- **Callback Port:** 51121
- **Confidential Client:** Uses embedded client ID and secret (base64 encoded)
- **Post-Auth:** Fetches user email and project ID for Code Assist

## 3. Authentication Methods

| Method | Use Case | Flow |
|--------|----------|------|
| Browser OAuth | Desktop with GUI | Local callback server, auto-redirect |
| Device Code | Headless/CI | Manual code entry at verification URL |
| Token Paste | API keys, setup tokens | Direct stdin input |

## 4. Token Lifecycle

- **Access Token:** Short-lived, used for API calls
- **Refresh Token:** Long-lived, used to obtain new access tokens
- **Proactive Refresh:** 5 minutes before expiration
- **Expiration Check:** `pkg/auth/store.go:IsExpired`, `NeedsRefresh`

## 5. Source of Truth

- **OAuth Core:** `pkg/auth/oauth.go` - OAuth 2.0 and PKCE implementation
- **Credential Storage:** `pkg/auth/store.go` - AuthCredential, AuthStore
- **Token Input:** `pkg/auth/token.go` - Token paste methods
- **Identity System:** `pkg/identity/identity.go` - Cross-platform user identification
- **CLI Commands:** `cmd/picoclaw/internal/auth/` - Login, logout, status handlers
- **Web UI:** `cmd/picoclaw-launcher/internal/server/auth_handlers.go` - Web-based authentication
