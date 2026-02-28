package tui

import (
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Displayer abstracts all output from the OAuth flow.
type Displayer interface {
	Banner()
	TokensFound()
	TokenValid()
	TokenExpired()
	TokensNotFound()
	Refreshing()
	RefreshOK()
	RefreshFailed(err error)
	DeviceCodeReady(userCode, verifyURI, verifyURIComplete string, expiry time.Time)
	WaitingForAuth()
	PollSlowDown(newInterval time.Duration)
	AuthSuccess()
	TokenSaved(path string)
	TokenSaveFailed(err error)
	Verifying()
	VerifyOK(body string)
	VerifyFailed(err error)
	APICallOK()
	APICallFailed(err error)
	AccessTokenRejected()
	TokenRefreshedRetrying()
	ReAuthRequired()
	Done(preview, tokenType string, expiresIn time.Duration)
	Fatal(err error)
}

// PlainDisplayer writes plain text output to w, reproducing the original CLI output.
// Used when stdout is not a TTY (pipes, CI, SSH without pty).
type PlainDisplayer struct {
	w io.Writer
}

// NewPlainDisplayer creates a PlainDisplayer that writes to w.
func NewPlainDisplayer(w io.Writer) *PlainDisplayer {
	return &PlainDisplayer{w: w}
}

func (p *PlainDisplayer) Banner() {
	fmt.Fprintln(p.w, "=== OAuth Device Code Flow CLI Demo (with Refresh Token) ===")
	fmt.Fprintln(p.w)
}

func (p *PlainDisplayer) TokensFound() {
	fmt.Fprintln(p.w, "Found existing tokens!")
}

func (p *PlainDisplayer) TokenValid() {
	fmt.Fprintln(p.w, "Access token is still valid, using it...")
}

func (p *PlainDisplayer) TokenExpired() {
	fmt.Fprintln(p.w, "Access token expired, refreshing...")
}

func (p *PlainDisplayer) TokensNotFound() {
	fmt.Fprintln(p.w, "No existing tokens found, starting device flow...")
}

func (p *PlainDisplayer) Refreshing() {
	fmt.Fprintln(p.w, "Refreshing access token...")
}

func (p *PlainDisplayer) RefreshOK() {
	fmt.Fprintln(p.w, "Token refreshed successfully!")
}

func (p *PlainDisplayer) RefreshFailed(err error) {
	fmt.Fprintf(p.w, "Refresh failed: %v\n", err)
	fmt.Fprintln(p.w, "Starting new device flow...")
}

func (p *PlainDisplayer) DeviceCodeReady(
	userCode, verifyURI, verifyURIComplete string,
	expiry time.Time,
) {
	fmt.Fprintln(p.w, "Step 1: Requesting device code...")
	fmt.Fprintln(p.w, "----------------------------------------")
	fmt.Fprintf(p.w, "Please open this link to authorize:\n%s\n", verifyURIComplete)
	fmt.Fprintf(p.w, "\nOr manually visit: %s\n", verifyURI)
	fmt.Fprintf(p.w, "And enter code: %s\n", userCode)
	fmt.Fprintln(p.w, "----------------------------------------")
	fmt.Fprintln(p.w)
}

func (p *PlainDisplayer) WaitingForAuth() {
	fmt.Fprintln(p.w, "Step 2: Waiting for authorization...")
}

func (p *PlainDisplayer) PollSlowDown(newInterval time.Duration) {
	fmt.Fprintf(p.w, "Server requested slower polling, new interval: %s\n", newInterval)
}

func (p *PlainDisplayer) AuthSuccess() {
	fmt.Fprintln(p.w, "\nAuthorization successful!")
}

func (p *PlainDisplayer) TokenSaved(path string) {
	fmt.Fprintf(p.w, "Tokens saved to %s\n", path)
}

func (p *PlainDisplayer) TokenSaveFailed(err error) {
	fmt.Fprintf(p.w, "Warning: Failed to save tokens: %v\n", err)
}

func (p *PlainDisplayer) Verifying() {
	fmt.Fprintln(p.w, "\nVerifying token...")
}

func (p *PlainDisplayer) VerifyOK(body string) {
	if body != "" {
		fmt.Fprintf(p.w, "Token Info: %s\n", body)
	}
	fmt.Fprintln(p.w, "Token verified successfully!")
}

func (p *PlainDisplayer) VerifyFailed(err error) {
	fmt.Fprintf(p.w, "Token verification failed: %v\n", err)
}

func (p *PlainDisplayer) APICallOK() {
	fmt.Fprintln(p.w, "API call successful!")
}

func (p *PlainDisplayer) APICallFailed(err error) {
	fmt.Fprintf(p.w, "API call failed: %v\n", err)
}

func (p *PlainDisplayer) AccessTokenRejected() {
	fmt.Fprintln(p.w, "Access token rejected (401), refreshing...")
}

func (p *PlainDisplayer) TokenRefreshedRetrying() {
	fmt.Fprintln(p.w, "Token refreshed, retrying API call...")
}

func (p *PlainDisplayer) ReAuthRequired() {
	fmt.Fprintln(p.w, "Refresh token expired, re-authenticating...")
}

func (p *PlainDisplayer) Done(preview, tokenType string, expiresIn time.Duration) {
	fmt.Fprintln(p.w, "\n========================================")
	fmt.Fprintln(p.w, "Current Token Info:")
	fmt.Fprintf(p.w, "Access Token: %s...\n", preview)
	fmt.Fprintf(p.w, "Token Type: %s\n", tokenType)
	fmt.Fprintf(p.w, "Expires In: %s\n", expiresIn.Round(time.Second))
	fmt.Fprintln(p.w, "========================================")
}

func (p *PlainDisplayer) Fatal(err error) {
	fmt.Fprintf(p.w, "Error: %v\n", err)
}

// NoopDisplayer is a no-op implementation used in tests.
type NoopDisplayer struct{}

func (NoopDisplayer) Banner()                                     {}
func (NoopDisplayer) TokensFound()                                {}
func (NoopDisplayer) TokenValid()                                 {}
func (NoopDisplayer) TokenExpired()                               {}
func (NoopDisplayer) TokensNotFound()                             {}
func (NoopDisplayer) Refreshing()                                 {}
func (NoopDisplayer) RefreshOK()                                  {}
func (NoopDisplayer) RefreshFailed(_ error)                       {}
func (NoopDisplayer) DeviceCodeReady(_, _, _ string, _ time.Time) {}
func (NoopDisplayer) WaitingForAuth()                             {}
func (NoopDisplayer) PollSlowDown(_ time.Duration)                {}
func (NoopDisplayer) AuthSuccess()                                {}
func (NoopDisplayer) TokenSaved(_ string)                         {}
func (NoopDisplayer) TokenSaveFailed(_ error)                     {}
func (NoopDisplayer) Verifying()                                  {}
func (NoopDisplayer) VerifyOK(_ string)                           {}
func (NoopDisplayer) VerifyFailed(_ error)                        {}
func (NoopDisplayer) APICallOK()                                  {}
func (NoopDisplayer) APICallFailed(_ error)                       {}
func (NoopDisplayer) AccessTokenRejected()                        {}
func (NoopDisplayer) TokenRefreshedRetrying()                     {}
func (NoopDisplayer) ReAuthRequired()                             {}
func (NoopDisplayer) Done(_, _ string, _ time.Duration)           {}
func (NoopDisplayer) Fatal(_ error)                               {}

// ProgramDisplayer sends BubbleTea messages to a running tea.Program.
type ProgramDisplayer struct {
	p *tea.Program
}

// NewProgramDisplayer creates a ProgramDisplayer that sends messages to p.
func NewProgramDisplayer(p *tea.Program) *ProgramDisplayer {
	return &ProgramDisplayer{p: p}
}

func (t *ProgramDisplayer) Banner() {
	t.p.Send(MsgBanner{})
}

func (t *ProgramDisplayer) TokensFound() {
	t.p.Send(MsgTokensFound{})
}

func (t *ProgramDisplayer) TokenValid() {
	t.p.Send(MsgTokenValid{})
}

func (t *ProgramDisplayer) TokenExpired() {
	t.p.Send(MsgTokenExpired{})
}

func (t *ProgramDisplayer) TokensNotFound() {
	t.p.Send(MsgTokensNotFound{})
}

func (t *ProgramDisplayer) Refreshing() {
	t.p.Send(MsgRefreshing{})
}

func (t *ProgramDisplayer) RefreshOK() {
	t.p.Send(MsgRefreshOK{})
}

func (t *ProgramDisplayer) RefreshFailed(err error) {
	t.p.Send(MsgRefreshFailed{Err: err})
}

func (t *ProgramDisplayer) DeviceCodeReady(
	userCode, verifyURI, verifyURIComplete string,
	expiry time.Time,
) {
	t.p.Send(MsgDeviceCodeReady{
		UserCode:          userCode,
		VerifyURI:         verifyURI,
		VerifyURIComplete: verifyURIComplete,
		Expiry:            expiry,
	})
}

func (t *ProgramDisplayer) WaitingForAuth() {
	t.p.Send(MsgWaitingForAuth{})
}

func (t *ProgramDisplayer) PollSlowDown(newInterval time.Duration) {
	t.p.Send(MsgPollSlowDown{NewInterval: newInterval})
}

func (t *ProgramDisplayer) AuthSuccess() {
	t.p.Send(MsgAuthSuccess{})
}

func (t *ProgramDisplayer) TokenSaved(path string) {
	t.p.Send(MsgTokenSaved{Path: path})
}

func (t *ProgramDisplayer) TokenSaveFailed(err error) {
	t.p.Send(MsgTokenSaveFailed{Err: err})
}

func (t *ProgramDisplayer) Verifying() {
	t.p.Send(MsgVerifying{})
}

func (t *ProgramDisplayer) VerifyOK(body string) {
	t.p.Send(MsgVerifyOK{Body: body})
}

func (t *ProgramDisplayer) VerifyFailed(err error) {
	t.p.Send(MsgVerifyFailed{Err: err})
}

func (t *ProgramDisplayer) APICallOK() {
	t.p.Send(MsgAPICallOK{})
}

func (t *ProgramDisplayer) APICallFailed(err error) {
	t.p.Send(MsgAPICallFailed{Err: err})
}

func (t *ProgramDisplayer) AccessTokenRejected() {
	t.p.Send(MsgAccessTokenRejected{})
}

func (t *ProgramDisplayer) TokenRefreshedRetrying() {
	t.p.Send(MsgTokenRefreshedRetrying{})
}

func (t *ProgramDisplayer) ReAuthRequired() {
	t.p.Send(MsgReAuthRequired{})
}

func (t *ProgramDisplayer) Done(preview, tokenType string, expiresIn time.Duration) {
	t.p.Send(MsgDone{Preview: preview, TokenType: tokenType, ExpiresIn: expiresIn})
}

func (t *ProgramDisplayer) Fatal(err error) {
	t.p.Send(MsgFatal{Err: err})
}
