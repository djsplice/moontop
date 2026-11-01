package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moontop/internal/console"
	"github.com/justinh-rahb/moontop/internal/moo"
	"github.com/justinh-rahb/moontop/internal/moonraker"
)

// ---------------------------------------------------------------------------
// `moontop console` — an interactive gcode console that lives in the
// terminal's normal screen, not the alternate screen.
//
// Output is printed above the program with tea.Println, so it becomes
// ordinary terminal scrollback (scroll, search, copy all work natively);
// the program itself only renders the prompt and a one-line status bar.
//
// Ordering: notification output is printed with
// tea.Sequence(print, listenNotifications) — the next notification
// isn't read until the previous one's lines have been handed to the
// renderer, so lines can never be reordered by concurrent Cmds.
// ---------------------------------------------------------------------------

func runConsole(args []string) int {
	fs := flag.NewFlagSet("console", flag.ContinueOnError)
	host := hostFlag(fs)
	noHistory := fs.Bool("no-history", false, "don't load or save command history")
	cfgFlag := configFlag(fs)
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// Client log lines would be drawn into the middle of the prompt.
	log.SetOutput(io.Discard)
	cfg, _ := loadConfig(*cfgFlag)
	applyTheme(cfg.Display.Theme)
	client, h, err := connectClient(hostOrEnv(*host), true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moontop console: connect to %s: %v\n", h, err)
		return 2
	}
	if client == nil {
		return 0
	}

	hist := console.NewHistory(0)
	histPath := ""
	if !*noHistory {
		if p, err := console.DefaultHistoryPath(); err == nil {
			histPath = p
			if err := hist.Load(p); err != nil {
				fmt.Fprintf(os.Stderr, "moontop console: history: %v\n", err)
			}
		}
	}

	m := newReplModel(client, h, hist, histPath)
	m.filters = buildFilters(cfg.Console)
	_, err = tea.NewProgram(m).Run()
	client.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "moontop console: %v\n", err)
		return 1
	}
	return 0
}

// replModel is the line console's state. It reuses the dashboard's
// message types and Cmds (cmds.go) for everything client-facing.
type replModel struct {
	client   *moonraker.Client
	host     string
	input    textinput.Model
	hist     *console.History
	histPath string

	klippy klippyStatus

	connState        connState
	reconnectAttempt int
	nextRetryIn      time.Duration

	confirmation *pendingConfirmation

	// inflight counts gcode sends awaiting their reply; running is the
	// most recent one, shown in the status bar ("running G28").
	inflight int
	running  string

	// last line printed, so an RPC error Klipper already echoed as a
	// "!!" response isn't printed twice.
	last string

	// Console filters: on by default; ctrl+o toggles them for output
	// from then on (lines already in scrollback can't be re-shown).
	filters    console.FilterSet
	showHidden bool
	hidden     int

	width    int
	quitting bool
}

// gcodeHelpMsg carries printer.gcode.help for tab completion.
type gcodeHelpMsg struct {
	commands []string
	err      error
}

func newReplModel(client *moonraker.Client, host string, hist *console.History, histPath string) replModel {
	ti := textinput.New()
	ti.Prompt = "> "
	// Quitting and E-stop are on the status line; the placeholder
	// only covers editing.
	ti.Placeholder = "gcode — tab completes, ↑/↓ history"
	ti.CharLimit = 1024
	ti.ShowSuggestions = true
	ti.PromptStyle = replPromptStyle
	ti.CompletionStyle = consoleInfoStyle
	// Up/down are history here; keep ctrl+n/ctrl+p for cycling
	// completion candidates.
	ti.KeyMap.NextSuggestion.SetKeys("ctrl+n")
	ti.KeyMap.PrevSuggestion.SetKeys("ctrl+p")
	ti.Focus()

	return replModel{
		client:   client,
		host:     host,
		input:    ti,
		hist:     hist,
		histPath: histPath,
		filters:  console.DefaultFilters,
	}
}

func (m replModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		checkKlippyCmd(m.client),
		loadGcodeStoreCmd(m.client),
		listenNotificationsCmd(m.client),
		listenDisconnectCmd(m.client),
	)
}

func loadGcodeHelpCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		help, err := client.GcodeHelp()
		if err != nil {
			return gcodeHelpMsg{err: err}
		}
		cmds := make([]string, 0, len(help))
		for k := range help {
			cmds = append(cmds, strings.ToUpper(k))
		}
		sort.Strings(cmds)
		return gcodeHelpMsg{commands: cmds}
	}
}

// printKlipper prints Klipper output, dropping lines the filters hide.
func (m *replModel) printKlipper(lines ...string) tea.Cmd {
	kept := lines[:0:0]
	for _, l := range lines {
		if !m.showHidden && m.filters.Hides(l) {
			m.hidden++
			continue
		}
		kept = append(kept, l)
	}
	return m.println(kept...)
}

// println returns a Cmd printing lines above the prompt, styled by kind,
// and remembers the last one.
func (m *replModel) println(lines ...string) tea.Cmd {
	if len(lines) == 0 {
		return nil
	}
	m.last = lines[len(lines)-1]
	styled := make([]string, len(lines))
	for i, l := range lines {
		styled[i] = styleConsoleLine(l)
	}
	return tea.Println(strings.Join(styled, "\n"))
}

func (m replModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.Width = msg.Width - lipgloss.Width(m.input.Prompt) - 1
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case notificationMsg:
		print, follow := m.handleNotification(msg)
		return m, tea.Batch(tea.Sequence(print, listenNotificationsCmd(m.client)), follow)

	case klippyStateMsg:
		if msg.err != nil {
			return m, m.println(console.ErrorLine(msg.err))
		}
		prev := m.klippy
		m.klippy = klippyStatus{State: msg.state, Message: msg.message}
		var cmds []tea.Cmd
		if msg.state == "ready" {
			cmds = append(cmds, loadGcodeHelpCmd(m.client))
			if prev.bannerVisible() {
				cmds = append(cmds, m.println(console.PrefixNotice+"Klipper ready"))
			}
		} else {
			// The state line may already have been printed by the
			// shutdown/disconnected notification; print only what's new.
			var lines []string
			if msg.state != prev.State {
				lines = append(lines, fmt.Sprintf("!! Klipper %s", msg.state))
			}
			if msg.message != "" && msg.message != prev.Message {
				lines = append(lines, prefixLines("!! ", msg.message)...)
			}
			cmds = append(cmds, m.println(lines...))
		}
		return m, tea.Batch(cmds...)

	case gcodeStoreMsg:
		if msg.err != nil {
			return m, m.println(console.ErrorLine(fmt.Errorf("console history: %w", msg.err)))
		}
		// The host is already on the status line.
		print := m.printKlipper(console.BackfillLines(msg.entries)...)
		return m, tea.Sequence(print, m.println(console.PrefixNotice+"── live ──"))

	case gcodeHelpMsg:
		if msg.err == nil {
			m.input.SetSuggestions(msg.commands)
		}
		return m, nil

	case gcodeSentMsg:
		m.inflight--
		if m.inflight <= 0 {
			m.inflight = 0
			m.running = ""
		}
		if msg.err != nil {
			if line := console.ErrorLine(msg.err); line != m.last {
				return m, m.println(line)
			}
		}
		return m, nil

	case klippyActionMsg:
		if msg.err != nil {
			return m, m.println(console.ErrorLine(fmt.Errorf("%s: %w", msg.action, msg.err)))
		}
		return m, nil

	case disconnectedMsg:
		m.connState = stateReconnecting
		m.reconnectAttempt = 1
		m.nextRetryIn = backoffDelay(1)
		m.inflight, m.running = 0, ""
		return m, tea.Batch(
			m.println(fmt.Sprintf("!! disconnected: %v", msg.err)),
			attemptReconnectCmd(m.client, 1, m.nextRetryIn),
		)

	case reconnectResultMsg:
		if msg.err == nil {
			m.connState = stateConnected
			m.reconnectAttempt, m.nextRetryIn = 0, 0
			m.klippy = klippyStatus{}
			return m, tea.Batch(
				m.println(fmt.Sprintf("** reconnected (attempt %d)", msg.attempt)),
				listenDisconnectCmd(m.client),
				checkKlippyCmd(m.client),
			)
		}
		next := msg.attempt + 1
		if next > reconnectFailedThreshold {
			m.connState = stateFailed
		}
		m.reconnectAttempt = next
		m.nextRetryIn = backoffDelay(next)
		return m, attemptReconnectCmd(m.client, next, m.nextRetryIn)

	case errMsg:
		m.quitting = true
		return m, tea.Sequence(m.println(console.ErrorLine(msg.err)), tea.Quit)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleNotification returns lines to print (ordered with the listener)
// and any follow-up work.
func (m *replModel) handleNotification(n notificationMsg) (print, follow tea.Cmd) {
	switch n.Method {
	case moonraker.MethodGcodeResponse:
		line, err := moonraker.ParseGcodeResponse(n.Params)
		if err != nil {
			return nil, nil
		}
		return m.printKlipper(strings.Split(line, "\n")...), nil
	case moonraker.MethodKlippyReady:
		// Same path as startup: re-check (which also reloads the
		// completion list — macros may have changed with the config).
		return nil, checkKlippyCmd(m.client)
	case moonraker.MethodKlippyShutdown:
		m.klippy.State = "shutdown"
		return m.println("!! Klipper shutdown"), fetchPrinterInfoCmd(m.client)
	case moonraker.MethodKlippyDisconnected:
		m.klippy = klippyStatus{State: "disconnected"}
		return m.println("!! Klipper host disconnected — waiting for it to come back"), nil
	}
	return nil, nil
}

func (m replModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// E-stop from anywhere, replacing any other prompt.
	if msg.String() == "ctrl+x" {
		m.confirmation = &pendingConfirmation{
			prompt: "EMERGENCY STOP — halt the printer now?",
			onYes:  emergencyStopCmd(m.client),
			danger: true,
		}
		return m, nil
	}
	if m.confirmation != nil {
		switch msg.String() {
		case "y", "Y":
			c := m.confirmation
			m.confirmation = nil
			if m.connState != stateConnected {
				return m, m.println("!! disconnected — emergency stop not sent")
			}
			return m, tea.Batch(m.println(console.PrefixNotice+"emergency stop sent"), c.onYes)
		case "n", "N", "esc", "ctrl+c":
			m.confirmation = nil
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+o":
		m.showHidden = !m.showHidden
		if m.showHidden {
			return m, m.println(console.PrefixNotice + "showing filtered lines (ctrl+o hides them again)")
		}
		return m, m.println(console.PrefixNotice + "hiding filtered lines")
	case "ctrl+c":
		// Like a shell: clear the line first, quit on an empty one.
		if m.input.Value() != "" {
			m.input.SetValue("")
			m.hist.Reset()
			return m, nil
		}
		return m.quit()
	case "ctrl+d":
		if m.input.Value() == "" {
			return m.quit()
		}
		return m, nil
	case "up":
		if v, ok := m.hist.Up(m.input.Value()); ok {
			m.input.SetValue(v)
			m.input.CursorEnd()
		}
		return m, nil
	case "down":
		if v, ok := m.hist.Down(); ok {
			m.input.SetValue(v)
			m.input.CursorEnd()
		}
		return m, nil
	case "enter":
		return m.submit()
	}

	// Editing a recalled line makes it a fresh draft.
	if m.hist.Recalling() && msg.Type != tea.KeyTab {
		m.hist.Reset()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m replModel) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	if text == "" {
		m.hist.Reset()
		return m, nil
	}
	m.hist.Add(text)
	m.saveHistory()

	switch strings.ToLower(text) {
	case "exit", "quit":
		return m.quit()
	}

	if cow, _ := moo.Trigger(text); cow {
		var cmds []tea.Cmd
		for _, l := range moo.Say(moo.Random(), 44) {
			cmds = append(cmds, m.println(l))
		}
		return m, tea.Sequence(cmds...)
	}

	echo := m.println(console.PrefixCommand + text)
	if m.connState != stateConnected {
		return m, tea.Sequence(echo, m.println("!! disconnected — not sent"))
	}
	m.inflight++
	m.running = text
	return m, tea.Sequence(echo, sendGcodeCmd(m.client, text))
}

func (m replModel) quit() (tea.Model, tea.Cmd) {
	m.saveHistory()
	m.quitting = true
	return m, tea.Quit
}

func (m *replModel) saveHistory() {
	if m.histPath != "" {
		_ = m.hist.Save(m.histPath)
	}
}

func (m replModel) View() string {
	if m.quitting {
		return ""
	}
	prompt := m.input.View()
	if m.confirmation != nil {
		// Same prompt as the dashboard, minus its left margin so it
		// lines up with the "> " prompt.
		prompt = strings.TrimLeft(renderConfirmation(m.confirmation.prompt, m.confirmation.danger), " ")
	}
	return prompt + "\n" + m.statusLine()
}

// statusLine: connection • Klipper state • what's running • keys.
func (m replModel) statusLine() string {
	sep := consoleInfoStyle.Render("  •  ")
	var conn string
	switch m.connState {
	case stateConnected:
		conn = renderConnIndicator(m.host, true, false)
	case stateReconnecting:
		conn = renderConnIndicator(fmt.Sprintf("%s reconnecting (#%d, next in %s)",
			m.host, m.reconnectAttempt, m.nextRetryIn.Truncate(time.Second)), false, true)
	default:
		conn = renderConnIndicator(fmt.Sprintf("%s disconnected — retrying in %s",
			m.host, m.nextRetryIn.Truncate(time.Second)), false, false)
	}

	parts := []string{conn}
	switch {
	case m.klippy.State == "":
		parts = append(parts, consoleInfoStyle.Render("Klipper …"))
	case m.klippy.bannerVisible():
		parts = append(parts, errStyle.Render("Klipper "+m.klippy.State))
	default:
		parts = append(parts, consoleNoticeStyle.Render("Klipper ready"))
	}
	if m.inflight > 0 {
		run := "running " + m.running
		if m.inflight > 1 {
			run += fmt.Sprintf(" (+%d queued)", m.inflight-1)
		}
		parts = append(parts, consoleCmdStyle.Render(run))
	}
	switch {
	case m.showHidden:
		parts = append(parts, consoleInfoStyle.Render("filters off (ctrl+o)"))
	case m.hidden > 0:
		parts = append(parts, consoleInfoStyle.Render(fmt.Sprintf("%d hidden (ctrl+o)", m.hidden)))
	}
	parts = append(parts, consoleInfoStyle.Render("ctrl+x e-stop  ctrl+d quit"))
	line := strings.Join(parts, sep)
	if m.width > 0 && lipgloss.Width(line) > m.width {
		// Drop the key hints first on narrow terminals.
		line = strings.Join(parts[:len(parts)-1], sep)
	}
	return line
}

// prefixLines prefixes every line of a multi-line message.
func prefixLines(prefix, msg string) []string {
	ls := strings.Split(msg, "\n")
	for i, l := range ls {
		ls[i] = prefix + l
	}
	return ls
}
