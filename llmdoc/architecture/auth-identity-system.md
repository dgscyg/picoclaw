# Authentication & Identity System

## 1. Identity

- **What it is:** OAuth 2.0 authentication with PKCE extension and cross-platform identity management system.
- **Purpose:** Provides secure token-based authentication for LLM providers and canonical user identification across messaging channels.

## 2. Core Components

- `pkg/auth/oauth.go` (`OAuthProviderConfig`, `LoginBrowser`, `LoginDeviceCode`, `RefreshAccessToken`): Core OAuth 2.0 implementation with browser and device code flows.
- `pkg/auth/pkce.go` (`PKCECodes`, `GeneratePKCE`): PKCE code generation using S256 challenge method.
- `pkg/auth/store.go` (`AuthCredential`, `AuthStore`, `LoadStore`, `SaveStore`): Credential persistence with atomic file writes.
- `pkg/auth/token.go` (`LoginPasteToken`, `LoginSetupToken`): Alternative token input methods for non-OAuth authentication.
- `pkg/auth/anthropic_usage.go` (`FetchAnthropicUsage`): OAuth usage statistics retrieval for Anthropic.
- `pkg/identity/identity.go` (`BuildCanonicalID`, `ParseCanonicalID`, `MatchAllowed`): Cross-platform user identification and access control.
- `cmd/picoclaw/internal/auth/helpers.go` (`authLoginCmd`, `authLogoutCmd`, `authStatusCmd`): CLI authentication command handlers.
- `cmd/picoclaw-launcher/internal/server/auth_handlers.go` (`handleOpenAILogin`, `handleOAuthCallback`): Web UI authentication endpoints.

## 3. Execution Flow (LLM Retrieval Map)

### OAuth Browser Flow

- **1. Initiate:** `cmd/picoclaw/internal/auth/helpers.go:authLoginOpenAI` calls `pkg/auth/oauth.go:LoginBrowser`.
- **2. PKCE Generation:** `pkg/auth/oauth.go:LoginBrowser` generates PKCE codes via `pkg/auth/pkce.go:GeneratePKCE`.
- **3. State Generation:** `pkg/auth/oauth.go:GenerateState` creates 32-byte CSRF token.
- **4. Callback Server:** Local server starts on provider-specific port (1455 for OpenAI, 51121 for Google).
- **5. Browser Launch:** Authorization URL built via `buildAuthorizeURL` with PKCE and state parameters.
- **6. Code Exchange:** `pkg/auth/oauth.go:ExchangeCodeForTokens` exchanges auth code for tokens.
- **7. Credential Storage:** `pkg/auth/store.go:SetCredential` saves to `~/.picoclaw/auth.json` via `WriteFileAtomic`.

### OAuth Device Code Flow

- **1. Initiate:** `pkg/auth/oauth.go:LoginDeviceCode` starts device code flow for headless environments.
- **2. Poll Loop:** 15-minute timeout with interval-based polling for token grant.
- **3. Token Storage:** Same as browser flow via `SetCredential`.

### Token Refresh Flow

- **1. Check Expiry:** `pkg/auth/store.go:NeedsRefresh` returns true if token expires within 5 minutes.
- **2. Refresh:** `pkg/auth/oauth.go:RefreshAccessToken` POSTs to token endpoint with refresh token.
- **3. Fallback:** Preserves existing credentials if provider response omits fields.

### Identity Matching Flow

- **1. Build ID:** `pkg/identity/identity.go:BuildCanonicalID` creates "platform:id" format.
- **2. Match:** `MatchAllowed` validates sender against allow-list supporting numeric ID, @username, "id|username", or canonical format.

## 4. Design Rationale

- **PKCE (S256):** Prevents authorization code interception attacks per RFC 7636.
- **State Parameter:** 32-byte random token prevents CSRF attacks.
- **Proactive Refresh:** 5-minute buffer prevents mid-request token expiration.
- **Atomic Writes:** File integrity during credential storage with 0600 permissions.
- **No Encryption at Rest:** Relies on filesystem permissions; tokens are not encrypted in storage.
- **JWT Parsing Without Verification:** Account ID extraction only; signature verification not required for client-side use.
