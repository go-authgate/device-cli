package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// tickMsg is fired every second to update the countdown timer.
type tickMsg time.Time

// state represents the current phase of the OAuth flow.
type state int

const (
	stateInit       state = iota
	stateRefreshing       // refreshing existing token
	stateDeviceFlow       // device code received, showing to user
	statePolling          // waiting for user authorization
	stateVerifying        // verifying token with server
	stateSuccess          // all done
	stateError            // fatal error
)

// statusKind distinguishes line types in the status log.
type statusKind int

const (
	statusOK   statusKind = iota
	statusWarn            // warning / non-fatal
	statusInfo            // neutral info
)

// statusLine is one row in the scrolling status log.
type statusLine struct {
	kind statusKind
	text string
}

// Model is the BubbleTea model for the device-flow TUI.
type Model struct {
	state   state
	spinner spinner.Model
	width   int
	height  int

	// Device code info
	userCode          string
	verifyURI         string
	verifyURIComplete string
	codeExpiry        time.Time
	remaining         time.Duration

	// Success / error display
	tokenPreview string
	tokenType    string
	expiresIn    time.Duration
	errMsg       string

	// Scrolling status log shown below the main panel
	statusLines []statusLine
}

// Lipgloss styles — defined once at package level.
var (
	styleTitleBox = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(0, 2)

	styleCodeBox = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("228")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("228")).
			Padding(0, 2)

	styleOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleDim  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleBold = lipgloss.NewStyle().Bold(true)
)

// NewModel creates the initial TUI model.
func NewModel() Model {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("99"))),
	)
	return Model{
		state:   stateInit,
		spinner: s,
	}
}

// Init starts the spinner animation.
func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tickMsg:
		m.remaining = max(time.Until(m.codeExpiry), 0)
		if m.remaining > 0 {
			return m, tickAfterSecond()
		}
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil

	// ── OAuth flow messages ──────────────────────────────────────────────────

	case MsgBanner:
		return m, nil

	case MsgTokensFound:
		m.addStatus(statusOK, "Found existing tokens")
		return m, nil

	case MsgTokenValid:
		m.addStatus(statusOK, "Access token is still valid")
		return m, nil

	case MsgTokenExpired:
		m.addStatus(statusWarn, "Access token expired")
		m.state = stateRefreshing
		return m, nil

	case MsgTokensNotFound:
		m.addStatus(statusInfo, "No existing tokens, starting device flow")
		return m, nil

	case MsgRefreshing:
		m.state = stateRefreshing
		m.addStatus(statusInfo, "Refreshing access token...")
		return m, nil

	case MsgRefreshOK:
		m.addStatus(statusOK, "Token refreshed successfully")
		return m, nil

	case MsgRefreshFailed:
		m.addStatus(statusWarn, fmt.Sprintf("Refresh failed: %v", msg.Err))
		return m, nil

	case MsgDeviceCodeReady:
		m.userCode = msg.UserCode
		m.verifyURI = msg.VerifyURI
		m.verifyURIComplete = msg.VerifyURIComplete
		m.codeExpiry = msg.Expiry
		m.remaining = time.Until(msg.Expiry)
		m.state = stateDeviceFlow
		m.addStatus(statusInfo, "Device code ready")
		return m, tickAfterSecond()

	case MsgWaitingForAuth:
		m.state = statePolling
		return m, nil

	case MsgPollSlowDown:
		m.addStatus(
			statusWarn,
			fmt.Sprintf("Server requested slower polling (%s)", msg.NewInterval),
		)
		return m, nil

	case MsgAuthSuccess:
		m.addStatus(statusOK, "Authorization successful!")
		return m, nil

	case MsgTokenSaved:
		m.addStatus(statusOK, "Tokens saved to "+msg.Path)
		return m, nil

	case MsgTokenSaveFailed:
		m.addStatus(statusWarn, fmt.Sprintf("Warning: failed to save tokens: %v", msg.Err))
		return m, nil

	case MsgVerifying:
		m.state = stateVerifying
		m.addStatus(statusInfo, "Verifying token...")
		return m, nil

	case MsgVerifyOK:
		m.addStatus(statusOK, "Token verified successfully")
		return m, nil

	case MsgVerifyFailed:
		m.addStatus(statusWarn, fmt.Sprintf("Token verification failed: %v", msg.Err))
		return m, nil

	case MsgAPICallOK:
		m.addStatus(statusOK, "API call successful")
		return m, nil

	case MsgAPICallFailed:
		m.addStatus(statusWarn, fmt.Sprintf("API call failed: %v", msg.Err))
		return m, nil

	case MsgAccessTokenRejected:
		m.addStatus(statusWarn, "Access token rejected (401), refreshing...")
		return m, nil

	case MsgTokenRefreshedRetrying:
		m.addStatus(statusOK, "Token refreshed, retrying API call...")
		return m, nil

	case MsgReAuthRequired:
		m.addStatus(statusWarn, "Refresh token expired, re-authenticating...")
		return m, nil

	case MsgDone:
		m.tokenPreview = msg.Preview
		m.tokenType = msg.TokenType
		m.expiresIn = msg.ExpiresIn
		m.state = stateSuccess
		return m, nil

	case MsgFatal:
		m.errMsg = msg.Err.Error()
		m.state = stateError
		return m, nil
	}

	return m, nil
}

// View renders the TUI.
func (m Model) View() tea.View {
	switch m.state {
	case stateSuccess:
		return tea.NewView(m.viewSuccess())
	case stateError:
		return tea.NewView(m.viewError())
	default:
		return tea.NewView(m.viewMain())
	}
}

// viewMain is shown during init, refresh, device flow, polling, and verifying.
func (m Model) viewMain() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(styleTitleBox.Render("  AuthGate Device Authorization  "))
	b.WriteString("\n\n")

	switch m.state {
	case stateDeviceFlow, statePolling:
		b.WriteString(styleBold.Render("Open this link to authorize:"))
		b.WriteString("\n")
		b.WriteString(m.verifyURIComplete)
		b.WriteString("\n\n")

		b.WriteString(styleDim.Render("Or visit: " + m.verifyURI))
		b.WriteString("\n")
		b.WriteString(styleDim.Render("Enter code:"))
		b.WriteString("\n\n")

		b.WriteString(styleCodeBox.Render("  " + m.userCode + "  "))
		b.WriteString("\n\n")

		if m.remaining > 0 {
			b.WriteString(m.spinner.View())
			b.WriteString(" Waiting for authorization...  ")
			b.WriteString(styleDim.Render(formatDuration(m.remaining) + " remaining"))
		} else if m.state == statePolling {
			b.WriteString(m.spinner.View())
			b.WriteString(" Waiting for authorization...")
		}
		b.WriteString("\n")

	case stateRefreshing:
		b.WriteString(m.spinner.View())
		b.WriteString(" Refreshing access token...\n")

	case stateVerifying:
		b.WriteString(m.spinner.View())
		b.WriteString(" Verifying token...\n")

	default:
		b.WriteString(m.spinner.View())
		b.WriteString(" Initializing...\n")
	}

	b.WriteString(m.viewStatusLog())
	return b.String()
}

// viewSuccess is shown after a successful authorization.
func (m Model) viewSuccess() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(styleOK.Render("  ✓ Authorization successful!"))
	b.WriteString("\n\n")

	b.WriteString(styleBold.Render("Access Token: "))
	b.WriteString(m.tokenPreview + "...\n")

	b.WriteString(styleBold.Render("Token Type:   "))
	b.WriteString(m.tokenType + "\n")

	b.WriteString(styleBold.Render("Expires In:   "))
	b.WriteString(formatDuration(m.expiresIn) + "\n")

	b.WriteString(m.viewStatusLog())
	return b.String()
}

// viewError is shown when a fatal error occurs.
func (m Model) viewError() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(styleErr.Render("  ✗ Authentication failed"))
	b.WriteString("\n\n")
	b.WriteString(styleDim.Render("  " + m.errMsg))
	b.WriteString("\n")

	b.WriteString(m.viewStatusLog())
	return b.String()
}

// viewStatusLog renders the scrolling status log.
func (m Model) viewStatusLog() string {
	if len(m.statusLines) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n")

	for _, line := range m.statusLines {
		switch line.kind {
		case statusOK:
			b.WriteString(styleOK.Render("  ✓ " + line.text))
		case statusWarn:
			b.WriteString(styleWarn.Render("  ⚠ " + line.text))
		default:
			b.WriteString(styleDim.Render("  · " + line.text))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// addStatus appends a line to the status log.
func (m *Model) addStatus(kind statusKind, text string) {
	m.statusLines = append(m.statusLines, statusLine{kind: kind, text: text})
}

// tickAfterSecond returns a command that fires tickMsg after one second.
func tickAfterSecond() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// formatDuration formats a duration as "Xm Ys" or "Xs".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d <= 0 {
		return "0s"
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
