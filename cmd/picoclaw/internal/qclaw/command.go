// Package qclaw provides CLI commands for QClaw channel management.
package qclaw

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/pkg/channels/qclaw"
	"github.com/sipeed/picoclaw/pkg/config"
)

func NewQClawCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qclaw",
		Short: "Manage QClaw channel (WeChat service account integration)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newLoginCommand(),
		newLogoutCommand(),
		newStatusCommand(),
	)

	return cmd
}

func newLoginCommand() *cobra.Command {
	var (
		environment string
		bypassInvite bool
		authPath    string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to QClaw via WeChat QR code",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadConfig("")
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return loginCmd(cfg.Channels.QClaw, environment, bypassInvite, authPath)
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "production", "Environment (production or test)")
	cmd.Flags().BoolVar(&bypassInvite, "bypass-invite", false, "Bypass invite code verification")
	cmd.Flags().StringVar(&authPath, "auth-path", "", "Custom path for auth state storage")

	return cmd
}

func newLogoutCommand() *cobra.Command {
	var authPath string

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Logout from QClaw and clear stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return logoutCmd(authPath)
		},
	}

	cmd.Flags().StringVar(&authPath, "auth-path", "", "Custom path for auth state storage")

	return cmd
}

func newStatusCommand() *cobra.Command {
	var authPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show QClaw authentication status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return statusCmd(authPath)
		},
	}

	cmd.Flags().StringVar(&authPath, "auth-path", "", "Custom path for auth state storage")

	return cmd
}

func loginCmd(cfg config.QClawConfig, environment string, bypassInvite bool, authPath string) error {
	ctx := context.Background()

	// Use config values if not specified
	if cfg.Environment != "" && environment == "production" {
		environment = cfg.Environment
	}
	if cfg.AuthStatePath != "" && authPath == "" {
		authPath = cfg.AuthStatePath
	}

	// Create API client
	api := qclaw.NewQClawAPI(environment)

	fmt.Println("\n🔐 QClaw WeChat Login")
	fmt.Println("====================")
	fmt.Println()

	// Generate device GUID
	guid := cfg.GUID
	if guid == "" {
		guid = qclaw.GenerateDeviceGUID()
		fmt.Printf("📱 Generated device GUID: %s\n", guid)
	}

	// Step 1: Get OAuth state
	fmt.Println("📋 Requesting login state...")
	stateResp, err := api.GetWxLoginState(ctx)
	if err != nil {
		return fmt.Errorf("get login state: %w", err)
	}

	// Step 2: Build OAuth URL and display QR code
	authURL := qclaw.BuildOAuthURL(stateResp.State, "", "", environment == "test")
	fmt.Printf("📱 Scan the QR code with WeChat to login:\n\n")

	// Generate and print QR code
	qr, err := qrcode.New(authURL, qrcode.Medium)
	if err != nil {
		fmt.Printf("📱 Open this URL in your browser:\n%s\n\n", authURL)
	} else {
		fmt.Println(qr.ToSmallString(false))
		fmt.Printf("\n📱 Or open this URL directly:\n%s\n\n", authURL)
	}

	// Step 3: Wait for user to scan and provide auth code
	fmt.Println("⏳ After scanning, you will be redirected to a URL like:")
	fmt.Println("   security.guanjia.qq.com/login?code=XXX&state=XXX")
	fmt.Println()
	fmt.Print("📋 Paste the 'code' parameter from the redirect URL: ")

	reader := bufio.NewReader(os.Stdin)
	code, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read auth code: %w", err)
	}
	code = strings.TrimSpace(code)

	if code == "" {
		return fmt.Errorf("auth code is required")
	}

	// Step 4: Exchange code for token
	fmt.Println("\n🔄 Exchanging code for token...")
	loginResp, err := api.WxLogin(ctx, code, stateResp.State, stateResp.LoginKey, guid)
	if err != nil {
		return fmt.Errorf("wx login: %w", err)
	}

	fmt.Printf("✅ Login successful!\n")
	fmt.Printf("   User ID: %s\n", loginResp.UserID)
	fmt.Printf("   GUID: %s\n", guid)

	// Step 5: Save auth state
	authMgr := qclaw.NewAuthStateManager(authPath)
	authState := &qclaw.AuthState{
		Token:        loginResp.Token,
		RefreshToken: loginResp.RefreshToken,
		GUID:         guid,
		UserID:       loginResp.UserID,
		ExpiresAt:    loginResp.ExpiresAt,
		ChannelToken: loginResp.ChannelToken,
	}

	if err := authMgr.SaveState(authState); err != nil {
		return fmt.Errorf("save auth state: %w", err)
	}

	fmt.Printf("\n💾 Credentials saved to: %s\n", authMgr.Path())
	fmt.Println("\n✅ Setup complete! You can now use the QClaw channel.")

	return nil
}

func logoutCmd(authPath string) error {
	authMgr := qclaw.NewAuthStateManager(authPath)

	// Check if state exists
	state, err := authMgr.LoadState()
	if err != nil {
		fmt.Println("ℹ️  No stored credentials found.")
		return nil
	}

	if state == nil {
		fmt.Println("ℹ️  No stored credentials found.")
		return nil
	}

	// Clear state
	if err := authMgr.ClearState(); err != nil {
		return fmt.Errorf("clear auth state: %w", err)
	}

	fmt.Println("✅ Logged out successfully. Credentials cleared.")
	return nil
}

func statusCmd(authPath string) error {
	authMgr := qclaw.NewAuthStateManager(authPath)

	state, err := authMgr.LoadState()
	if err != nil {
		fmt.Println("❌ Error loading credentials:", err)
		return nil
	}

	if state == nil {
		fmt.Println("ℹ️  No stored credentials found.")
		fmt.Println("   Run 'picoclaw qclaw login' to authenticate.")
		return nil
	}

	fmt.Println("\n📊 QClaw Authentication Status")
	fmt.Println("==============================")
	fmt.Printf("   User ID:    %s\n", state.UserID)
	fmt.Printf("   GUID:       %s\n", state.GUID)
	fmt.Printf("   Has Token:  %v\n", state.Token != "")

	if state.ExpiresAt > 0 {
		expiresAt := time.Unix(state.ExpiresAt, 0)
		if time.Now().Before(expiresAt) {
			fmt.Printf("   Expires:    %s (valid)\n", expiresAt.Format(time.RFC3339))
		} else {
			fmt.Printf("   Expires:    %s (expired)\n", expiresAt.Format(time.RFC3339))
		}
	}

	fmt.Printf("   State Path: %s\n", authMgr.Path())

	return nil
}
