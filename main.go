package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	retry "github.com/appleboy/go-httpretry"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"

	tea "charm.land/bubbletea/v2"
	"github.com/go-authgate/device-cli/tui"
)

var (
	serverURL         string
	clientID          string
	tokenFile         string
	flagServerURL     *string
	flagClientID      *string
	flagTokenFile     *string
	configInitialized bool
	retryClient       *retry.Client
)

// Timeout configuration for different operations
const (
	deviceCodeRequestTimeout = 10 * time.Second
	tokenExchangeTimeout     = 5 * time.Second
	tokenVerificationTimeout = 10 * time.Second
	refreshTokenTimeout      = 10 * time.Second
)

func init() {
	// Load .env file if exists (ignore error if not found)
	_ = godotenv.Load()

	// Define flags (but don't parse yet to avoid conflicts with test flags)
	flagServerURL = flag.String(
		"server-url",
		"",
		"OAuth server URL (default: http://localhost:8080 or SERVER_URL env)",
	)
	flagClientID = flag.String("client-id", "", "OAuth client ID (required, or set CLIENT_ID env)")
	flagTokenFile = flag.String(
		"token-file",
		"",
		"Token storage file (default: .authgate-tokens.json or TOKEN_FILE env)",
	)
}

// initConfig parses flags and initializes configuration
// Separated from init() to avoid conflicts with test flag parsing
func initConfig() {
	if configInitialized {
		return
	}
	configInitialized = true

	flag.Parse()

	// Priority: flag > env > default
	serverURL = getConfig(*flagServerURL, "SERVER_URL", "http://localhost:8080")
	clientID = getConfig(*flagClientID, "CLIENT_ID", "")
	tokenFile = getConfig(*flagTokenFile, "TOKEN_FILE", ".authgate-tokens.json")

	// Validate SERVER_URL format
	if err := validateServerURL(serverURL); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Invalid SERVER_URL: %v\n", err)
		os.Exit(1)
	}

	// Warn if using HTTP instead of HTTPS
	if strings.HasPrefix(strings.ToLower(serverURL), "http://") {
		fmt.Fprintln(
			os.Stderr,
			"⚠️  WARNING: Using HTTP instead of HTTPS. Tokens will be transmitted in plaintext!",
		)
		fmt.Fprintln(
			os.Stderr,
			"⚠️  This is only safe for local development. Use HTTPS in production.",
		)
		fmt.Fprintln(os.Stderr)
	}

	if clientID == "" {
		fmt.Println("Error: CLIENT_ID not set. Please provide it via:")
		fmt.Println("  1. Command line flag: -client-id=<your-client-id>")
		fmt.Println("  2. Environment variable: CLIENT_ID=<your-client-id>")
		fmt.Println("  3. .env file: CLIENT_ID=<your-client-id>")
		fmt.Println("\nYou can find the client_id in the server startup logs.")
		os.Exit(1)
	}

	// Validate CLIENT_ID format (should be UUID)
	if _, err := uuid.Parse(clientID); err != nil {
		fmt.Fprintf(
			os.Stderr,
			"⚠️  Warning: CLIENT_ID doesn't appear to be a valid UUID: %s\n",
			clientID,
		)
		fmt.Fprintln(
			os.Stderr,
			"⚠️  This may cause authentication issues if the server expects UUID format.",
		)
		fmt.Fprintln(os.Stderr)
	}

	// Initialize HTTP client with retry support
	baseHTTPClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			DisableKeepAlives:   false,
		},
	}

	// Wrap with retry logic using go-httpretry
	var err error
	retryClient, err = retry.NewBackgroundClient(
		retry.WithHTTPClient(baseHTTPClient),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to create retry client: %v", err))
	}
}

// getConfig returns value with priority: flag > env > default
func getConfig(flagValue, envKey, defaultValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return getEnv(envKey, defaultValue)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// validateServerURL validates that the server URL is properly formatted
func validateServerURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("server URL cannot be empty")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got: %s", u.Scheme)
	}

	if u.Host == "" {
		return errors.New("URL must include a host")
	}

	return nil
}

type ErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ErrRefreshTokenExpired indicates that the refresh token has expired or is invalid
var ErrRefreshTokenExpired = errors.New("refresh token expired or invalid")

// validateTokenResponse validates the OAuth token response
func validateTokenResponse(accessToken, tokenType string, expiresIn int) error {
	if accessToken == "" {
		return errors.New("access_token is empty")
	}

	if len(accessToken) < 10 {
		return fmt.Errorf("access_token is too short (length: %d)", len(accessToken))
	}

	if expiresIn <= 0 {
		return fmt.Errorf("expires_in must be positive, got: %d", expiresIn)
	}

	// Token type is optional in OAuth 2.0, but if present, should be "Bearer"
	if tokenType != "" && tokenType != "Bearer" {
		return fmt.Errorf("unexpected token_type: %s (expected Bearer)", tokenType)
	}

	return nil
}

// TokenStorage represents saved tokens for a specific client
type TokenStorage struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	ClientID     string    `json:"client_id"`
}

// TokenStorageMap manages tokens for multiple clients
type TokenStorageMap struct {
	Tokens map[string]*TokenStorage `json:"tokens"` // key = client_id
}

// isTTY reports whether stderr is a character device (interactive terminal).
// We check stderr because the TUI renders to stderr, allowing stdout to be piped.
func isTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func main() {
	initConfig()

	if isTTY() {
		// Run TUI program on stderr so stdout pipes are not corrupted
		m := tui.NewModel()
		// WithInput(nil): disable stdin/keyboard input so BubbleTea skips terminal
		// capability queries (?2026/?2027). Ctrl+C is handled by signal.NotifyContext.
		p := tea.NewProgram(m, tea.WithOutput(os.Stderr), tea.WithInput(nil))

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
			}
		}()

		d := tui.NewProgramDisplayer(p)
		d.Banner()
		runErr := run(d)
		p.Quit() // let BubbleTea drain terminal query responses before exiting
		wg.Wait()
		if runErr != nil {
			os.Exit(1)
		}
	} else {
		d := tui.NewPlainDisplayer(os.Stderr)
		d.Banner()
		if err := run(d); err != nil {
			os.Exit(1)
		}
	}
}

func run(d tui.Displayer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var storage *TokenStorage

	// Try to load existing tokens
	storage, err := loadTokens()
	if err == nil && storage != nil {
		d.TokensFound()

		// Check if access token is still valid
		if time.Now().Before(storage.ExpiresAt) {
			d.TokenValid()
		} else {
			d.TokenExpired()
			d.Refreshing()

			// Try to refresh
			newStorage, err := refreshAccessToken(ctx, storage.RefreshToken, d)
			if err != nil {
				d.RefreshFailed(err)
				storage = nil // Force device flow
			} else {
				storage = newStorage
				d.RefreshOK()
			}
		}
	} else {
		d.TokensNotFound()
	}

	// If no valid tokens, do device flow
	if storage == nil {
		storage, err = performDeviceFlow(ctx, d)
		if err != nil {
			d.Fatal(err)
			return err
		}
	}

	// Display current token info
	tokenPreview := storage.AccessToken
	if len(tokenPreview) > 50 {
		tokenPreview = tokenPreview[:50]
	}
	d.Done(tokenPreview, storage.TokenType, time.Until(storage.ExpiresAt).Round(time.Second))

	// Verify token
	d.Verifying()
	if err := verifyToken(ctx, storage.AccessToken, d); err != nil {
		d.VerifyFailed(err)
	}

	// Demonstrate automatic refresh on 401
	if err := makeAPICallWithAutoRefresh(ctx, storage, d); err != nil {
		// Check if error is due to expired refresh token
		if err == ErrRefreshTokenExpired {
			d.ReAuthRequired()
			storage, err = performDeviceFlow(ctx, d)
			if err != nil {
				d.Fatal(err)
				return err
			}

			// Retry API call with new tokens
			d.TokenRefreshedRetrying()
			if err := makeAPICallWithAutoRefresh(ctx, storage, d); err != nil {
				d.Fatal(err)
				return err
			}
			d.APICallOK()
		} else {
			d.APICallFailed(err)
		}
	}

	return nil
}

// requestDeviceCode requests a device code from the OAuth server with retry logic
func requestDeviceCode(ctx context.Context) (*oauth2.DeviceAuthResponse, error) {
	// Create request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, deviceCodeRequestTimeout)
	defer cancel()

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("scope", "read write")

	req, err := http.NewRequestWithContext(
		reqCtx,
		http.MethodPost,
		serverURL+"/oauth/device/code",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Execute request with retry logic
	resp, err := retryClient.DoWithContext(reqCtx, req)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"device code request failed with status %d: %s",
			resp.StatusCode,
			string(body),
		)
	}

	// Parse response
	var deviceResp struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}

	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}

	return &oauth2.DeviceAuthResponse{
		DeviceCode:              deviceResp.DeviceCode,
		UserCode:                deviceResp.UserCode,
		VerificationURI:         deviceResp.VerificationURI,
		VerificationURIComplete: deviceResp.VerificationURIComplete,
		Expiry:                  time.Now().Add(time.Duration(deviceResp.ExpiresIn) * time.Second),
		Interval:                int64(deviceResp.Interval),
	}, nil
}

// performDeviceFlow performs the OAuth device authorization flow
func performDeviceFlow(ctx context.Context, d tui.Displayer) (*TokenStorage, error) {
	config := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: serverURL + "/oauth/device/code",
			TokenURL:      serverURL + "/oauth/token",
		},
		Scopes: []string{"read", "write"},
	}

	// Step 1: Request device code (with retry logic)
	deviceAuth, err := requestDeviceCode(ctx)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}

	d.DeviceCodeReady(
		deviceAuth.UserCode,
		deviceAuth.VerificationURI,
		deviceAuth.VerificationURIComplete,
		deviceAuth.Expiry,
	)

	// Step 2: Poll for token
	d.WaitingForAuth()
	token, err := pollForTokenWithProgress(ctx, config, deviceAuth, d)
	if err != nil {
		return nil, fmt.Errorf("token poll failed: %w", err)
	}

	d.AuthSuccess()

	// Convert to TokenStorage and save
	storage := &TokenStorage{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.Type(),
		ExpiresAt:    token.Expiry,
		ClientID:     clientID,
	}

	if err := saveTokens(storage); err != nil {
		d.TokenSaveFailed(err)
	} else {
		d.TokenSaved(tokenFile)
	}

	return storage, nil
}

// pollForTokenWithProgress polls for token while reporting progress via Displayer.
// Implements exponential backoff for slow_down errors per RFC 8628.
func pollForTokenWithProgress(
	ctx context.Context,
	config *oauth2.Config,
	deviceAuth *oauth2.DeviceAuthResponse,
	d tui.Displayer,
) (*oauth2.Token, error) {
	// Initial polling interval (from DeviceAuthResponse)
	interval := deviceAuth.Interval
	if interval == 0 {
		interval = 5 // Default to 5 seconds per RFC 8628
	}

	// Exponential backoff state
	pollInterval := time.Duration(interval) * time.Second
	backoffMultiplier := 1.0

	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case <-pollTicker.C:
			// Attempt to exchange device code for token
			token, err := exchangeDeviceCode(
				ctx,
				config.Endpoint.TokenURL,
				config.ClientID,
				deviceAuth.DeviceCode,
			)
			if err != nil {
				var oauthErr *oauth2.RetrieveError
				if errors.As(err, &oauthErr) {
					// Parse OAuth error response
					var errResp ErrorResponse
					if jsonErr := json.Unmarshal(oauthErr.Body, &errResp); jsonErr == nil {
						switch errResp.Error {
						case "authorization_pending":
							// User hasn't authorized yet, continue polling
							continue

						case "slow_down":
							// Server requests slower polling - increase interval
							backoffMultiplier *= 1.5
							pollInterval = min(
								time.Duration(float64(pollInterval)*backoffMultiplier),
								60*time.Second,
							)
							pollTicker.Reset(pollInterval)
							d.PollSlowDown(pollInterval)
							continue

						case "expired_token":
							return nil, errors.New("device code expired, please restart the flow")

						case "access_denied":
							return nil, errors.New("user denied authorization")

						default:
							return nil, fmt.Errorf(
								"authorization failed: %s - %s",
								errResp.Error,
								errResp.ErrorDescription,
							)
						}
					}
				}
				// Unknown error
				return nil, fmt.Errorf("token exchange failed: %w", err)
			}

			// Success!
			return token, nil
		}
	}
}

// exchangeDeviceCode exchanges device code for access token
func exchangeDeviceCode(
	ctx context.Context,
	tokenURL, clientID, deviceCode string,
) (*oauth2.Token, error) {
	// Create request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, tokenExchangeTimeout)
	defer cancel()

	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("device_code", deviceCode)
	data.Set("client_id", clientID)

	req, err := http.NewRequestWithContext(
		reqCtx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := retryClient.DoWithContext(reqCtx, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Handle non-200 responses
	if resp.StatusCode != http.StatusOK {
		return nil, &oauth2.RetrieveError{
			Response: resp,
			Body:     body,
		}
	}

	// Parse successful token response
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Validate token response
	if err := validateTokenResponse(
		tokenResp.AccessToken,
		tokenResp.TokenType,
		tokenResp.ExpiresIn,
	); err != nil {
		return nil, fmt.Errorf("invalid token response: %w", err)
	}

	token := &oauth2.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	return token, nil
}

func verifyToken(ctx context.Context, accessToken string, d tui.Displayer) error {
	// Create request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, tokenVerificationTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(
		reqCtx, http.MethodGet, serverURL+"/oauth/tokeninfo", nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// Execute request with retry logic
	resp, err := retryClient.DoWithContext(reqCtx, req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
		}
		return fmt.Errorf("%s: %s", errResp.Error, errResp.ErrorDescription)
	}

	d.VerifyOK(string(body))
	return nil
}

// loadTokens loads tokens from file for the current client
func loadTokens() (*TokenStorage, error) {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}

	var storageMap TokenStorageMap
	if err := json.Unmarshal(data, &storageMap); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	if storageMap.Tokens == nil {
		return nil, errors.New("no tokens found in token file")
	}

	// Look up token for current client_id
	if storage, ok := storageMap.Tokens[clientID]; ok {
		return storage, nil
	}

	return nil, fmt.Errorf("no tokens found for client_id: %s", clientID)
}

// saveTokens saves tokens to file (merges with existing tokens for other clients)
// Uses file locking to prevent race conditions when multiple processes access the same file
func saveTokens(storage *TokenStorage) error {
	// Ensure ClientID is set
	if storage.ClientID == "" {
		storage.ClientID = clientID
	}

	// Acquire file lock to prevent concurrent access
	lock, err := acquireFileLock(tokenFile)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil {
			fmt.Fprintf(os.Stderr, "failed to release lock: %v\n", releaseErr)
		}
	}()

	// Load existing token map (inside lock to ensure consistency)
	var storageMap TokenStorageMap
	existingData, err := os.ReadFile(tokenFile)
	if err == nil {
		// File exists, try to load it
		if unmarshalErr := json.Unmarshal(existingData, &storageMap); unmarshalErr != nil {
			// If unmarshal fails, start with empty map
			storageMap.Tokens = make(map[string]*TokenStorage)
		}
	}

	// Initialize map if nil
	if storageMap.Tokens == nil {
		storageMap.Tokens = make(map[string]*TokenStorage)
	}

	// Add or update token for current client
	storageMap.Tokens[storage.ClientID] = storage

	// Marshal data
	data, err := json.MarshalIndent(storageMap, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file first (atomic write pattern)
	tempFile := tokenFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename (replaces old file)
	if err := os.Rename(tempFile, tokenFile); err != nil {
		if removeErr := os.Remove(tempFile); removeErr != nil {
			return fmt.Errorf(
				"failed to rename temp file: %v; additionally failed to remove temp file: %w",
				err,
				removeErr,
			)
		}
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// refreshAccessToken refreshes the access token using refresh token
func refreshAccessToken(
	ctx context.Context,
	refreshToken string,
	d tui.Displayer,
) (*TokenStorage, error) {
	// Create request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, refreshTokenTimeout)
	defer cancel()

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientID)

	req, err := http.NewRequestWithContext(
		reqCtx,
		http.MethodPost,
		serverURL+"/oauth/token",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Execute request with retry logic
	resp, err := retryClient.DoWithContext(reqCtx, req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil {
			// Check if refresh token is expired or invalid
			if errResp.Error == "invalid_grant" || errResp.Error == "invalid_token" {
				return nil, ErrRefreshTokenExpired
			}
			return nil, fmt.Errorf("%s: %s", errResp.Error, errResp.ErrorDescription)
		}
		return nil, fmt.Errorf("refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse token response
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Validate token response
	if err := validateTokenResponse(
		tokenResp.AccessToken,
		tokenResp.TokenType,
		tokenResp.ExpiresIn,
	); err != nil {
		return nil, fmt.Errorf("invalid token response: %w", err)
	}

	// Handle refresh token rotation modes:
	// - Rotation mode: Server returns new refresh_token (use it)
	// - Fixed mode: Server doesn't return refresh_token (preserve old one)
	newRefreshToken := tokenResp.RefreshToken
	if newRefreshToken == "" {
		// Server didn't return a new refresh token (fixed mode)
		newRefreshToken = refreshToken
	}

	storage := &TokenStorage{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: newRefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		ClientID:     clientID,
	}

	// Save updated tokens
	if err := saveTokens(storage); err != nil {
		d.TokenSaveFailed(err)
	}

	return storage, nil
}

// makeAPICallWithAutoRefresh demonstrates automatic refresh on 401
func makeAPICallWithAutoRefresh(ctx context.Context, storage *TokenStorage, d tui.Displayer) error {
	// Try with current access token
	reqCtx, cancel := context.WithTimeout(ctx, tokenVerificationTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(
		reqCtx, http.MethodGet, serverURL+"/oauth/tokeninfo", nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+storage.AccessToken)

	resp, err := retryClient.DoWithContext(reqCtx, req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	// If 401, try to refresh and retry
	if resp.StatusCode == http.StatusUnauthorized {
		d.AccessTokenRejected()

		newStorage, err := refreshAccessToken(ctx, storage.RefreshToken, d)
		if err != nil {
			// If refresh token is expired, propagate the error to trigger device flow
			if err == ErrRefreshTokenExpired {
				return ErrRefreshTokenExpired
			}
			return fmt.Errorf("refresh failed: %w", err)
		}

		// Update storage in memory
		// Note: newStorage has already been saved to disk by refreshAccessToken()
		storage.AccessToken = newStorage.AccessToken
		storage.RefreshToken = newStorage.RefreshToken
		storage.ExpiresAt = newStorage.ExpiresAt

		d.TokenRefreshedRetrying()

		// Retry with new token
		retryCtx, retryCancel := context.WithTimeout(ctx, tokenVerificationTimeout)
		defer retryCancel()

		req, err = http.NewRequestWithContext(
			retryCtx, http.MethodGet, serverURL+"/oauth/tokeninfo", nil,
		)
		if err != nil {
			return fmt.Errorf("failed to create retry request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+storage.AccessToken)

		resp, err = retryClient.DoWithContext(retryCtx, req)
		if err != nil {
			return fmt.Errorf("retry failed: %w", err)
		}
		defer resp.Body.Close()
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API call failed with status %d: %s", resp.StatusCode, string(body))
	}

	d.APICallOK()
	return nil
}
