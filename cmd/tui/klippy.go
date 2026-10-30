package main

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
)

// ---------------------------------------------------------------------------
// Phase 14: generic notification routing + Klippy lifecycle
//
// The client exposes ONE channel of raw notifications. One listener Cmd
// reads it and re-issues itself; Update hands each message to
// handleNotification, which looks the method up in notificationHandlers.
// Supporting a new notification is one new table entry — no new
// channel, no new listener, no new re-issue site on reconnect.
// ---------------------------------------------------------------------------

// Identity sent with server.connection.identify.
const (
	clientName    = "Moontop"
	clientVersion = "0.14.0"
	clientURL     = "https://github.com/justinh-rahb/moonraker-tui"
)

// gcodeStoreBackfill is how many cached console lines to pull from
// Moonraker on startup.
const gcodeStoreBackfill = 100

// notificationMsg wraps one notification from Client.Notifications().
type notificationMsg moonraker.Notification

// notificationHandler applies one notification to the model and may
// return follow-up work. It must not block.
type notificationHandler func(m *model, params []json.RawMessage) tea.Cmd

// notificationHandlers is the dispatch table. Methods not listed here
// are ignored (Moonraker sends ~26 kinds; we route the ones we use).
var notificationHandlers = map[string]notificationHandler{
	moonraker.MethodStatusUpdate:       (*model).onStatusUpdate,
	moonraker.MethodGcodeResponse:      (*model).onGcodeResponse,
	moonraker.MethodKlippyReady:        (*model).onKlippyReady,
	moonraker.MethodKlippyShutdown:     (*model).onKlippyShutdown,
	moonraker.MethodKlippyDisconnected: (*model).onKlippyDisconnected,
}

// listenNotifications blocks on one notification, then is re-issued by
// handleNotification. The channel survives reconnect, so this listener
// is never re-issued from the reconnect path — it's already waiting.
func (m model) listenNotifications() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		n, ok := <-client.Notifications()
		if !ok {
			return errMsg{fmt.Errorf("notification channel closed")}
		}
		return notificationMsg(n)
	}
}

// handleNotification dispatches through the table and always re-arms
// the listener, whether or not the method was handled.
func (m *model) handleNotification(n notificationMsg) tea.Cmd {
	var cmd tea.Cmd
	if h, ok := notificationHandlers[n.Method]; ok {
		cmd = h(m, n.Params)
	}
	return tea.Batch(m.listenNotifications(), cmd)
}

func (m *model) onStatusUpdate(params []json.RawMessage) tea.Cmd {
	update, err := moonraker.ParseStatusUpdate(params)
	if err != nil {
		m.appendLog("!! " + err.Error())
		return nil
	}
	if !m.ready {
		// No subscription snapshot yet; initMsg will carry full state.
		return nil
	}
	mergeStatus(m.sensors, &m.position, &m.job, &m.limits, &m.klippy, update.Objects)
	m.klippyChanged()
	m.rebuildRows()
	m.refreshTuningInputs()
	// Drive the progress bar toward the live percent; snap to 0
	// between prints so the next one starts from empty.
	if m.job.State == "printing" {
		return m.progress.SetPercent(m.job.Progress)
	}
	return m.progress.SetPercent(0)
}

func (m *model) onGcodeResponse(params []json.RawMessage) tea.Cmd {
	line, err := moonraker.ParseGcodeResponse(params)
	if err != nil {
		m.appendLog("!! " + err.Error())
		return nil
	}
	m.appendLog(line)
	return nil
}

// onKlippyReady re-runs the full initialize path. Moonraker drops every
// subscription when Klippy disconnects, and the object list itself may
// have changed (config edit + RESTART), so re-listing is required, not
// just re-subscribing.
func (m *model) onKlippyReady(_ []json.RawMessage) tea.Cmd {
	m.appendLog("** Klipper ready — resubscribing")
	return m.initialize()
}

// onKlippyShutdown marks the state immediately (no params carry the
// reason) and fetches printer.info for the message.
func (m *model) onKlippyShutdown(_ []json.RawMessage) tea.Cmd {
	m.setKlippy("shutdown", m.klippy.Message)
	return m.fetchPrinterInfo()
}

func (m *model) onKlippyDisconnected(_ []json.RawMessage) tea.Cmd {
	m.setKlippy("disconnected", "Klipper host disconnected — waiting for it to come back")
	return nil
}

// ---------------------------------------------------------------------------
// Klippy state
// ---------------------------------------------------------------------------

// klippyStatus mirrors the webhooks object / printer.info. State "" means
// unknown (before the first server.info answer, or mid-reconnect).
type klippyStatus struct {
	State   string // ready | startup | shutdown | error | disconnected
	Message string
}

// bannerVisible is true whenever Klippy is known and not ready.
func (k klippyStatus) bannerVisible() bool {
	return k.State != "" && k.State != "ready"
}

// klippyStateMsg reports Klippy state learned outside the subscription
// (server.info during initialize, printer.info after a shutdown).
type klippyStateMsg struct {
	state   string
	message string
	err     error
}

// setKlippy updates Klippy state from any source and keeps derived UI
// (console note, layout reserve for the banner) consistent.
func (m *model) setKlippy(state, message string) {
	m.klippy.State = state
	m.klippy.Message = message
	m.klippyChanged()
}

// klippyChanged is called after anything that may have touched
// m.klippy. Logs transitions once (with the full multi-line message,
// which the one-line banner truncates) and re-runs layout when the
// banner appears or disappears.
func (m *model) klippyChanged() {
	cur := m.klippy
	prev := m.lastKlippy
	if cur == prev {
		return
	}
	m.lastKlippy = cur
	if cur.State != prev.State && cur.bannerVisible() {
		m.appendLog(fmt.Sprintf("!! Klipper %s", cur.State))
	}
	if cur.Message != prev.Message && cur.bannerVisible() && cur.Message != "" {
		m.appendLog("!! " + cur.Message)
	}
	if cur.bannerVisible() != prev.bannerVisible() && m.ready {
		m.resizeLayout()
	}
}

// fetchPrinterInfo asks printer.info for Klippy's state message.
func (m model) fetchPrinterInfo() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		info, err := client.GetPrinterInfo()
		if err != nil {
			return klippyStateMsg{err: err}
		}
		return klippyStateMsg{state: info.State, message: info.StateMessage}
	}
}

// ---------------------------------------------------------------------------
// Safety and restart actions — all confirm-gated
// ---------------------------------------------------------------------------

// klippyActionMsg surfaces send failures for E-stop / restarts. Success
// is silent: the visible state change arrives as notifications.
type klippyActionMsg struct {
	action string
	err    error
}

func (m model) emergencyStop() tea.Cmd {
	client := m.client
	return func() tea.Msg { return klippyActionMsg{"emergency stop", client.EmergencyStop()} }
}

func (m model) restartKlipper() tea.Cmd {
	client := m.client
	return func() tea.Msg { return klippyActionMsg{"restart", client.RestartKlipper()} }
}

func (m model) firmwareRestart() tea.Cmd {
	client := m.client
	return func() tea.Msg { return klippyActionMsg{"firmware restart", client.FirmwareRestart()} }
}

// armEmergencyStop replaces any pending confirmation: E-stop must never
// be blocked by a half-answered prompt for something else.
func (m *model) armEmergencyStop() {
	m.confirmation = &pendingConfirmation{
		prompt: "EMERGENCY STOP — halt the printer now?",
		onYes:  m.emergencyStop(),
		danger: true,
	}
}

// handleKlippyControl handles K / F (restart / firmware restart). Like
// job controls, these only fire outside the console so typing gcode
// isn't hijacked. Restarts kill an active print, hence the confirm.
func (m *model) handleKlippyControl(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "K":
		m.confirmation = &pendingConfirmation{
			prompt: "Restart Klipper (RESTART)?",
			onYes:  m.restartKlipper(),
			danger: jobActive(m.job.State),
		}
		return nil, true
	case "F":
		m.confirmation = &pendingConfirmation{
			prompt: "Firmware restart (FIRMWARE_RESTART)?",
			onYes:  m.firmwareRestart(),
			danger: jobActive(m.job.State),
		}
		return nil, true
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Console backfill
// ---------------------------------------------------------------------------

type gcodeStoreMsg struct {
	entries []moonraker.GcodeStoreEntry
	err     error
}

func (m model) loadGcodeStore() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		entries, err := client.GcodeStore(gcodeStoreBackfill)
		return gcodeStoreMsg{entries: entries, err: err}
	}
}

// backfillLines converts gcode_store entries to console lines, using the
// same ">>> " prefix as commands typed here.
func backfillLines(entries []moonraker.GcodeStoreEntry) []string {
	var lines []string
	for _, e := range entries {
		msg := e.Message
		if e.Type == "command" {
			msg = ">>> " + msg
		}
		lines = append(lines, strings.Split(msg, "\n")...)
	}
	return lines
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// consoleLineKind classifies a console line for styling. Klipper marks
// errors with "!!" and informational echoes with "//"; Moontop uses
// ">>> " for sent commands and "** " for its own notices.
type consoleLineKind int

const (
	lineNormal consoleLineKind = iota
	lineError
	lineInfo
	lineCommand
	lineNotice
)

func classifyLine(l string) consoleLineKind {
	switch {
	case strings.HasPrefix(l, "!!"):
		return lineError
	case strings.HasPrefix(l, "//"):
		return lineInfo
	case strings.HasPrefix(l, ">>> "):
		return lineCommand
	case strings.HasPrefix(l, "** "):
		return lineNotice
	}
	return lineNormal
}

func styleConsoleLine(l string) string {
	switch classifyLine(l) {
	case lineError:
		return consoleErrStyle.Render(l)
	case lineInfo:
		return consoleInfoStyle.Render(l)
	case lineCommand:
		return consoleCmdStyle.Render(l)
	case lineNotice:
		return consoleNoticeStyle.Render(l)
	}
	return l
}

// renderKlippyBanner is the one-line state banner shown between the
// title and the panes while Klippy isn't ready. The full (often
// multi-line) message is in the console; the banner shows its first
// line, truncated to the terminal width.
func renderKlippyBanner(k klippyStatus, width int) string {
	label := "Klipper " + k.State
	msg := k.Message
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	keys := "  K: restart  •  F: firmware restart  •  ctrl+x: e-stop"
	text := "⚠ " + label
	if msg != "" {
		text += ": " + msg
	}
	avail := width - 4 - lipgloss.Width(keys)
	if avail > 8 && lipgloss.Width(text) > avail {
		text = truncateRunes(text, avail-1) + "…"
	}
	return bannerStyle.Render(text) + footerStyle.UnsetMarginLeft().Render(keys)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if n < 0 {
		n = 0
	}
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// renderNotReady is shown instead of the dashboard until the first
// successful subscribe: Klippy is starting / in error / shut down, or
// we're still waiting on server.info.
func (m model) renderNotReady() string {
	var b strings.Builder
	b.WriteString(appTitleStyle.Render("🌡 Moontop") + "\n\n")
	switch {
	case m.klippy.bannerVisible():
		b.WriteString(renderKlippyBanner(m.klippy, m.width) + "\n")
		if m.klippy.Message != "" {
			for _, l := range strings.Split(m.klippy.Message, "\n") {
				b.WriteString(footerStyle.Render(l) + "\n")
			}
		}
	case m.initErr != nil:
		b.WriteString(errStyle.MarginLeft(2).Render(fmt.Sprintf("init: %v", m.initErr)) + "\n")
		b.WriteString(footerStyle.Render("Will retry when Klipper reports ready or the connection is re-established.") + "\n")
	default:
		b.WriteString(footerStyle.Render("Connecting…") + "\n")
	}
	b.WriteString("\n")
	if m.confirmation != nil {
		b.WriteString(renderConfirmation(m.confirmation.prompt, m.confirmation.danger) + "\n")
	}
	b.WriteString(lipgloss.NewStyle().MarginLeft(2).Render(m.renderConnView()) +
		footerStyle.Render("  •  ctrl+c: quit") + "\n")
	return b.String()
}
