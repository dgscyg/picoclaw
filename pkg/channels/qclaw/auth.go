package qclaw

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/sipeed/picoclaw/pkg/logger"
)

// QClawAPI provides methods to interact with the QClaw JPRX gateway.
type QClawAPI struct {
	gatewayURL  string
	loginKey    string
	httpClient  *http.Client
}

// NewQClawAPI creates a new QClaw API client.
func NewQClawAPI(environment string) *QClawAPI {
	gatewayURL := JPRXGatewayProd
	if environment == EnvironmentTest {
		gatewayURL = JPRXGatewayTest
	}

	return &QClawAPI{
		gatewayURL: gatewayURL,
		loginKey:   "m83qdao0AmE5", // Default login key
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetWxLoginState retrieves OAuth state for WeChat login.
func (a *QClawAPI) GetWxLoginState(ctx context.Context) (*WxLoginStateResponse, error) {
	var result WxLoginStateResponse
	err := a.doRequest(ctx, CmdGetWxLoginState, map[string]any{
		"login_key": a.loginKey,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// WxLogin exchanges OAuth code for JWT token.
func (a *QClawAPI) WxLogin(ctx context.Context, code, state, loginKey, guid string) (*WxLoginResponse, error) {
	var result WxLoginResponse
	err := a.doRequest(ctx, CmdWxLogin, map[string]any{
		"code":      code,
		"state":     state,
		"login_key": loginKey,
		"guid":      guid,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// RefreshChannelToken refreshes the channel token.
func (a *QClawAPI) RefreshChannelToken(ctx context.Context, token, guid string) (*WxLoginResponse, error) {
	var result WxLoginResponse
	err := a.doRequest(ctx, CmdRefreshChannelToken, map[string]any{
		"token": token,
		"guid":  guid,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GenerateContactLink generates a WeCom customer service contact link for device binding.
func (a *QClawAPI) GenerateContactLink(ctx context.Context, token, guid, kfID string) (string, error) {
	if kfID == "" {
		kfID = "wkzLlJLAAAfbxEV3ZcS-lHZxkaKmpejQ" // Default KF ID
	}

	var result struct {
		ContactLink string `json:"contact_link"`
	}
	err := a.doRequestWithToken(ctx, CmdGenerateContactLink, map[string]any{
		"kf_id": kfID,
	}, token, guid, &result)
	if err != nil {
		return "", err
	}
	return result.ContactLink, nil
}

// QueryDeviceByGuid checks device binding status.
func (a *QClawAPI) QueryDeviceByGuid(ctx context.Context, token, guid string) (bool, error) {
	var result struct {
		IsBound bool `json:"is_bound"`
	}
	err := a.doRequestWithToken(ctx, CmdQueryDeviceByGuid, nil, token, guid, &result)
	if err != nil {
		return false, err
	}
	return result.IsBound, nil
}

// doRequest performs a generic API request to the JPRX gateway.
func (a *QClawAPI) doRequest(ctx context.Context, cmdID int, body any, result any) error {
	return a.doRequestWithToken(ctx, cmdID, body, "", "", result)
}

// doRequestWithToken performs an API request with authentication headers.
func (a *QClawAPI) doRequestWithToken(ctx context.Context, cmdID int, body any, token, guid string, result any) error {
	endpoint := fmt.Sprintf("%sdata/%d/forward", a.gatewayURL, cmdID)

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	} else {
		bodyReader = bytes.NewReader([]byte("{}"))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Version", "1.0")
	if token != "" {
		req.Header.Set("X-Token", token)
		req.Header.Set("X-OpenClaw-Token", token)
	}
	if guid != "" {
		req.Header.Set("X-Guid", guid)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var apiResp QClawAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if apiResp.CommonCode != 0 {
		return fmt.Errorf("api error code=%d message=%s", apiResp.CommonCode, apiResp.Message)
	}

	// Check for token expired code
	if apiResp.CommonCode == 21004 {
		return &TokenExpiredError{Message: apiResp.Message}
	}

	if result != nil && len(apiResp.Data) > 0 {
		if err := json.Unmarshal(apiResp.Data, result); err != nil {
			return fmt.Errorf("parse data: %w", err)
		}
	}

	return nil
}

// TokenExpiredError indicates the token has expired.
type TokenExpiredError struct {
	Message string
}

func (e *TokenExpiredError) Error() string {
	return fmt.Sprintf("token expired: %s", e.Message)
}

// AuthStateManager manages authentication state persistence.
type AuthStateManager struct {
	statePath string
}

// NewAuthStateManager creates a new auth state manager.
func NewAuthStateManager(statePath string) *AuthStateManager {
	if statePath == "" {
		homeDir, _ := os.UserHomeDir()
		statePath = filepath.Join(homeDir, ".picoclaw", "qclaw-auth.json")
	}
	return &AuthStateManager{statePath: statePath}
}

// LoadState loads the authentication state from disk.
func (m *AuthStateManager) LoadState() (*AuthState, error) {
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No state file exists
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var state AuthState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}

	return &state, nil
}

// SaveState saves the authentication state to disk.
func (m *AuthStateManager) SaveState(state *AuthState) error {
	// Ensure directory exists
	dir := filepath.Dir(m.statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Write with restrictive permissions (0600)
	if err := os.WriteFile(m.statePath, data, 0o600); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}

	logger.InfoCF("qclaw", "Saved auth state", map[string]any{
		"path": m.statePath,
	})
	return nil
}

// ClearState removes the authentication state file.
func (m *AuthStateManager) ClearState() error {
	if err := os.Remove(m.statePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove state file: %w", err)
	}
	return nil
}

// Path returns the path to the authentication state file.
func (m *AuthStateManager) Path() string {
	return m.statePath
}

// GenerateDeviceGUID generates a stable device GUID.
func GenerateDeviceGUID() string {
	uuidStr := uuid.NewString()
	hash := md5.Sum([]byte(uuidStr))
	return hex.EncodeToString(hash[:])
}

// BuildOAuthURL builds the WeChat OAuth authorization URL.
func BuildOAuthURL(state, appID, redirectURI string, isTest bool) string {
	if appID == "" {
		if isTest {
			appID = "wx3dd49afb7e2cf957"
		} else {
			appID = "wx9d11056dd75b7240"
		}
	}

	if redirectURI == "" {
		if isTest {
			redirectURI = "security-test.guanjia.qq.com/login"
		} else {
			redirectURI = "security.guanjia.qq.com/login"
		}
	}

	params := url.Values{}
	params.Set("appid", appID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", "snsapi_login")
	params.Set("state", state)

	return fmt.Sprintf("https://open.weixin.qq.com/connect/qrconnect?%s#wechat_redirect", params.Encode())
}