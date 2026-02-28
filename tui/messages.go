package tui

import (
	"time"
)

// MsgBanner signals that the banner/title should be displayed.
type MsgBanner struct{}

// MsgTokensFound signals that existing tokens were found on disk.
type MsgTokensFound struct{}

// MsgTokenValid signals that the existing access token is still valid.
type MsgTokenValid struct{}

// MsgTokenExpired signals that the access token has expired.
type MsgTokenExpired struct{}

// MsgTokensNotFound signals that no tokens were found (starting fresh).
type MsgTokensNotFound struct{}

// MsgRefreshing signals that a token refresh is in progress.
type MsgRefreshing struct{}

// MsgRefreshOK signals that the token was refreshed successfully.
type MsgRefreshOK struct{}

// MsgRefreshFailed signals that token refresh failed.
type MsgRefreshFailed struct{ Err error }

// MsgDeviceCodeReady signals that the device code is ready for user action.
type MsgDeviceCodeReady struct {
	UserCode          string
	VerifyURI         string
	VerifyURIComplete string
	Expiry            time.Time
}

// MsgWaitingForAuth signals that polling for authorization has started.
type MsgWaitingForAuth struct{}

// MsgPollSlowDown signals that the server requested slower polling.
type MsgPollSlowDown struct{ NewInterval time.Duration }

// MsgAuthSuccess signals that the user authorized successfully.
type MsgAuthSuccess struct{}

// MsgTokenSaved signals that tokens were saved to disk.
type MsgTokenSaved struct{ Path string }

// MsgTokenSaveFailed signals that saving tokens failed.
type MsgTokenSaveFailed struct{ Err error }

// MsgVerifying signals that token verification is in progress.
type MsgVerifying struct{}

// MsgVerifyOK signals that token verification succeeded.
type MsgVerifyOK struct{ Body string }

// MsgVerifyFailed signals that token verification failed.
type MsgVerifyFailed struct{ Err error }

// MsgAPICallOK signals that an API call succeeded.
type MsgAPICallOK struct{}

// MsgAPICallFailed signals that an API call failed.
type MsgAPICallFailed struct{ Err error }

// MsgAccessTokenRejected signals that the access token was rejected (401).
type MsgAccessTokenRejected struct{}

// MsgTokenRefreshedRetrying signals that the token was refreshed and a retry is starting.
type MsgTokenRefreshedRetrying struct{}

// MsgReAuthRequired signals that the refresh token is expired and re-auth is required.
type MsgReAuthRequired struct{}

// MsgDone signals successful completion of the OAuth flow.
type MsgDone struct {
	Preview   string
	TokenType string
	ExpiresIn time.Duration
}

// MsgFatal signals a fatal error that should terminate the flow.
type MsgFatal struct{ Err error }
