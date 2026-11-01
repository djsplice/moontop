package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moonraker-tui/internal/console"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
	"github.com/justinh-rahb/moonraker-tui/internal/prompt"
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
	moonraker.MethodHistoryChanged:     (*model).onJobsChanged,
	moonraker.MethodJobQueueChanged:    (*model).onJobsChanged,
}

// listenNotifications blocks on one notification, then is re-issued by
// handleNotification. The channel survives reconnect, so this listener
// is never re-issued from the reconnect path — it's already waiting.
func (m model) listenNotifications() tea.Cmd {
	return listenNotificationsCmd(m.client)
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
	mergeStatus(m.sensors, &m.position, &m.job, &m.limits, &m.klippy, m.raw, update.Objects)
	m.klippyChanged()
	m.rebuildRows()
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
	if prompt.IsPromptLine(line) || strings.Contains(line, "action:prompt_") {
		m.promptLive = true
	}
	for _, l := range m.feedPromptLines(strings.Split(line, "\n")) {
		m.appendLog(l)
	}
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
	return fetchPrinterInfoCmd(m.client)
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
	return emergencyStopCmd(m.client)
}

func (m model) restartKlipper() tea.Cmd {
	return restartKlipperCmd(m.client)
}

func (m model) firmwareRestart() tea.Cmd {
	return firmwareRestartCmd(m.client)
}

// armEmergencyStop replaces any pending confirmation: E-stop must never
// be blocked by a half-answered prompt for something else.
// With confirmation turned off in setup (Safety), it fires at once.
func (m *model) armEmergencyStop() tea.Cmd {
	if !m.cfg.Safety.EStopConfirms() {
		m.confirmation = nil
		return m.requireConnected("emergency stop", m.emergencyStop())
	}
	m.confirmation = &pendingConfirmation{
		prompt: "EMERGENCY STOP — halt the printer now?",
		onYes:  m.emergencyStop(),
		danger: true,
	}
	return nil
}

// confirmOrRun asks first during a print, or when idle restarts are set
// to confirm (the default); otherwise it sends straight away.
func (m *model) confirmOrRun(prompt, what string, cmd tea.Cmd) tea.Cmd {
	active := jobActive(m.job.State)
	if !active && !m.cfg.Safety.RestartConfirmsIdle() {
		return m.requireConnected(what, cmd)
	}
	m.confirmation = &pendingConfirmation{prompt: prompt, onYes: cmd, danger: active}
	return nil
}

// handleKlippyControl handles K / F (restart / firmware restart).
// Restarts kill an active print, hence the confirm.
func (m *model) handleKlippyControl(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, *keys.Restart):
		return m.confirmOrRun("Restart Klipper (RESTART)?", "restart", m.restartKlipper()), true
	case key.Matches(msg, *keys.FwRestart):
		return m.confirmOrRun("Firmware restart (FIRMWARE_RESTART)?", "firmware restart", m.firmwareRestart()), true
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
	return loadGcodeStoreCmd(m.client)
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// styleConsoleLine maps the shared line classification to the
// dashboard's console styles.
func styleConsoleLine(l string) string {
	switch console.ClassifyLine(l) {
	case console.KindError:
		return consoleErrStyle.Render(l)
	case console.KindInfo:
		return consoleInfoStyle.Render(l)
	case console.KindCommand:
		return consoleCmdStyle.Render(l)
	case console.KindNotice:
		return consoleNoticeStyle.Render(l)
	}
	return l
}

// renderKlippyBanner is the one-line state banner shown between the
// title and the panes while Klippy isn't ready. The full (often
// multi-line) message is in the console; the banner shows its first
// line, truncated to the terminal width.
//
// lettersWork says whether K and F reach the restart actions; while the
// console (or a file filter) is taking text they're just letters, so
// the banner offers only F5 / F6, which work everywhere.
func renderKlippyBanner(k klippyStatus, width int, lettersWork bool) string {
	label := "Klipper " + k.State
	msg := k.Message
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	keys := "  F5: restart  •  F6: firmware restart  •  ctrl+x: e-stop"
	if lettersWork {
		keys = "  K/F5: restart  •  F/F6: firmware restart  •  ctrl+x: e-stop"
	}
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

// restartLettersWork reports whether K / F act as restart keys right
// now (false while the focused pane takes them as text).
func (m *model) restartLettersWork() bool {
	return !m.capturesText(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("K")}) && !m.paneClaims(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("K")})
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
	b.WriteString(appTitleStyle.Render(appTitle) + "\n\n")
	switch {
	case m.klippy.bannerVisible():
		b.WriteString(renderKlippyBanner(m.klippy, m.width, m.restartLettersWork()) + "\n")
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
		b.WriteString(renderPrompt(m.confirmation, m.width) + "\n")
	}
	b.WriteString(lipgloss.NewStyle().MarginLeft(2).Render(m.renderConnView()) +
		footerStyle.Render("  •  ctrl+c: quit") + "\n")
	return b.String()
}
