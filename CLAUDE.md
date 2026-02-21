# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is an OAuth 2.0 Device Authorization Flow CLI tool (RFC 8628) written in Go. It authenticates with an AuthGate server and manages tokens for headless/SSH environments.

## Development Commands

### Building and Testing

```bash
# Build the binary
make build                # Output: bin/device-cli

# Run all tests with coverage
make test

# View test coverage in browser
make coverage

# Run a single test
go test -v -run TestFunctionName ./...

# Run tests for specific file
go test -v ./filelock_test.go filelock.go
```

### Linting and Formatting

```bash
# Run golangci-lint
make lint

# Auto-format code
make fmt

# Check installed tools
make check-tools
```

### Running the Application

```bash
# Build and run locally
make build
./bin/device-cli -client-id=<id> -server-url=http://localhost:8080

# Hot reload during development
make dev                  # Uses air for live reload
```

### Other Commands

```bash
# Generate files (templ templates if any)
make generate

# Clean build artifacts
make clean

# Cross-platform builds
make build_linux_amd64
make build_linux_arm64
```

## Architecture

### File Structure

- **main.go** (905 lines): Core application logic, contains all OAuth flow, HTTP client, token management, and CLI entry point
- **filelock.go**: File locking mechanism for safe concurrent token file access
- **polling_test.go**: Tests for device code polling with exponential backoff
- **main_test.go**: Core functionality tests including concurrent token writes

### Configuration Loading

Configuration uses a three-tier priority system implemented in `initConfig()`:

1. Command-line flags (highest priority)
2. Environment variables
3. `.env` file / defaults (lowest priority)

The `initConfig()` function is separated from `init()` to avoid conflicts with Go's test flag parsing. Tests manually initialize variables in their own `init()` function.

### Token Storage Architecture

**Multi-client support**: A single JSON file (`TokenStorageMap`) stores tokens for multiple OAuth clients, keyed by `client_id`:

```json
{
  "tokens": {
    "client-id-1": { "access_token": "...", "refresh_token": "...", ... },
    "client-id-2": { "access_token": "...", "refresh_token": "...", ... }
  }
}
```

**Atomic writes**: Uses temp file + rename pattern to prevent corruption:

1. Write new data to `<tokenfile>.tmp`
2. Rename tmp file to actual file (atomic operation)
3. On error, clean up temp file

**File locking**: `saveTokens()` acquires an exclusive lock via `acquireFileLock()` to prevent race conditions during concurrent access. Lock files (`<tokenfile>.lock`) have stale lock detection (30s timeout) and automatic cleanup.

### OAuth Flow Implementation

The main flow is orchestrated by `run()`:

1. **Token Loading**: Try to load existing tokens for current `client_id`
2. **Token Validation**: Check if access token is expired (compare `ExpiresAt` with current time)
3. **Refresh Flow**: If expired, call `refreshAccessToken()` with refresh token
4. **Device Flow Fallback**: If no tokens or refresh fails, call `performDeviceFlow()`
5. **Token Verification**: Verify token with server's `/oauth/tokeninfo` endpoint
6. **Auto-refresh Demo**: `makeAPICallWithAutoRefresh()` demonstrates automatic refresh on 401

### Device Code Polling

`pollForTokenWithProgress()` implements RFC 8628 polling with exponential backoff:

- Initial interval from server (default: 5s)
- On `slow_down` error: multiply interval by 1.5, cap at 60s
- Progress dots printed every 2s (UI update interval)
- Two separate tickers: one for polling, one for UI updates
- Handles errors: `authorization_pending`, `slow_down`, `expired_token`, `access_denied`

### HTTP Client Configuration

`retryClient` (initialized in `initConfig()`) wraps a base HTTP client with:

- TLS 1.2+ enforcement
- Connection pooling (MaxIdleConns: 10, IdleConnTimeout: 90s)
- Retry logic via `go-httpretry` package
- Per-operation timeouts (constants at top of main.go):
  - Device code request: 10s
  - Token exchange: 5s
  - Token verification: 10s
  - Refresh token: 10s

### Refresh Token Rotation

`refreshAccessToken()` handles two server modes:

1. **Rotation mode**: Server returns new `refresh_token` → use it
2. **Fixed mode**: Server returns empty `refresh_token` → preserve old one

The function checks `ErrRefreshTokenExpired` for `invalid_grant` or `invalid_token` errors, signaling the need for a new device flow.

### Error Handling

- **Context cancellation**: All HTTP operations accept `context.Context` and respect cancellation (Ctrl+C)
- **OAuth errors**: Parsed from JSON error responses (`ErrorResponse` struct)
- **Validation**: `validateTokenResponse()` checks access token length (≥10 chars), expiry, and token type
- **URL validation**: `validateServerURL()` ensures proper scheme (http/https) and host presence

## Testing Patterns

### Test Initialization

Tests use a separate `init()` function that sets defaults without calling `initConfig()`, avoiding flag parsing conflicts.

### Concurrent Testing

`TestSaveTokens_ConcurrentWrites` spawns 10 goroutines to verify file locking works correctly under concurrent access.

### Test Servers

Tests use `httptest` to create mock OAuth servers, returning device codes and tokens.

## Important Notes

### Security Warnings

The CLI automatically warns users when:

- Using HTTP instead of HTTPS (tokens transmitted in plaintext)
- CLIENT_ID is not a valid UUID format

### Token File Security

- Created with `0600` permissions (owner read/write only)
- Should be added to `.gitignore` (already configured)
- Stored at `.authgate-tokens.json` by default

### Version Information

The Makefile supports version injection via git tags:

```bash
VERSION=$(git describe --tags --always)
COMMIT=$(git rev-parse --short HEAD)
```

goreleaser uses these to build versioned binaries with naming pattern:
`authgate-device-cli-{version}-{os}-{arch}`

### CI/CD

- **Testing**: Runs on Ubuntu and macOS with Go 1.25 and 1.26
- **Linting**: Uses golangci-lint v2.9 with config from `.golangci.yml`
- **Release**: goreleaser builds for multiple platforms (see `.goreleaser.yaml`)
- **Generate step**: Always run `make generate` before building (for templ templates)

## Go Modules

Requires Go 1.25+. Key dependencies:

- `golang.org/x/oauth2`: OAuth 2.0 client library
- `github.com/appleboy/go-httpretry`: HTTP retry logic
- `github.com/joho/godotenv`: .env file loading
- `github.com/google/uuid`: UUID validation
