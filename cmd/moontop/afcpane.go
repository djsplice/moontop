package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moontop/internal/afc"
)

// ---------------------------------------------------------------------------
// Phase 13: the AFC pane (pane 6, beside the console)
//
// Every AFC object is subscribed whole and merged into m.raw by the
// generic store; afc.Parse turns it into units and lanes on each render.
// Actions follow retrograde's rules (see internal/afc): nothing runs
// during a print, while AFC is busy or in error (except clearing the
// message), or when the printer doesn't advertise the command; load,
// unload and eject ask first.
// ---------------------------------------------------------------------------

func isAFCObject(name string) bool { return afc.IsObject(name) }

// afcModel parses the current AFC state, nil when there's no AFC.
func (m *model) afcModel() *afc.Model { return afc.Parse(m.raw) }

// selectedLane is the lane under the AFC cursor.
func (m *model) selectedLane(am *afc.Model) *afc.Lane {
	if am == nil || len(am.Lanes) == 0 {
		return nil
	}
	if m.afcCursor >= len(am.Lanes) {
		m.afcCursor = len(am.Lanes) - 1
	}
	if m.afcCursor < 0 {
		m.afcCursor = 0
	}
	return am.Lanes[m.afcCursor]
}

// afcBlocked returns why AFC actions can't run now ("" = they can).
func (m *model) afcBlocked(am *afc.Model, allowError bool) string {
	switch {
	case !m.connected():
		return "the printer is disconnected"
	case m.klippy.State != "ready":
		return "Klipper isn't ready"
	case jobActive(m.job.State):
		return "finish the current print first"
	case am == nil:
		return "AFC is no longer available"
	case am.Error && !allowError:
		return "AFC reports an error; clear it first (x)"
	case am.Busy():
		return "AFC is busy (" + am.State + ")"
	}
	return ""
}

// commandAvailable: AFC-Lite doesn't implement every AFC command, so a
// command the printer doesn't list in its gcode help isn't offered.
// Until the list arrives, everything is assumed available.
func (m *model) commandAvailable(cmd string) bool {
	return m.gcodeCommands == nil || m.gcodeCommands[cmd]
}

func (m *model) afcRefuse(why string) tea.Cmd {
	m.appendLog("!! AFC: " + why)
	return nil
}

func (m *model) handleAFCKey(msg tea.KeyMsg) tea.Cmd {
	am := m.afcModel()
	if key.Matches(msg, *keys.AfcMove) {
		if am != nil {
			switch msg.String() {
			case "up", "k":
				m.afcCursor--
			default:
				m.afcCursor++
			}
			m.selectedLane(am) // clamps
		}
		return nil
	}

	// Clearing the message is allowed while AFC is in error.
	if key.Matches(msg, *keys.AfcClear) {
		if why := m.afcBlocked(am, true); why != "" {
			return m.afcRefuse(why)
		}
		if !m.commandAvailable(afc.Clear) {
			return m.afcRefuse(afc.Clear + " isn't available on this printer")
		}
		m.appendLog(">>> " + afc.Clear)
		return m.requireConnected("clear AFC message", m.sendGcode(afc.Clear))
	}

	lane := m.selectedLane(am)
	var command string
	var setting *afcSetting
	switch {
	case key.Matches(msg, *keys.AfcLoad):
		command = afc.Load
	case key.Matches(msg, *keys.AfcUnload):
		command = afc.Unload
	case key.Matches(msg, *keys.AfcEject):
		command = afc.Eject
	default:
		for i := range afcSettings {
			if key.Matches(msg, *afcSettings[i].key) {
				setting = &afcSettings[i]
			}
		}
		if setting == nil {
			return nil
		}
		command = setting.command
	}
	if lane == nil {
		return m.afcRefuse("no lane selected")
	}
	if why := m.afcBlocked(am, false); why != "" {
		return m.afcRefuse(why)
	}
	if !m.commandAvailable(command) {
		return m.afcRefuse(command + " isn't available on this printer")
	}

	if setting != nil {
		m.openAFCEdit(lane, *setting)
		return nil
	}
	gcode, err := afc.LaneCommand(lane, command)
	if err != nil {
		return m.afcRefuse(err.Error())
	}
	verb := map[string]string{afc.Load: "Load lane %s into %s", afc.Unload: "Unload lane %s from %s", afc.Eject: "Eject lane %s%s"}[command]
	target := ""
	switch command {
	case afc.Load, afc.Unload:
		target = "its tool"
		if len(lane.Tools) > 0 {
			target = strings.Join(lane.Tools, "/")
		}
	}
	m.confirmation = &pendingConfirmation{
		prompt: fmt.Sprintf(verb+"?", lane.Name, target),
		onYes:  m.sendGcode(gcode),
	}
	return nil
}

// afcSetting is a lane setting edited on the prompt line.
type afcSetting struct {
	key     *key.Binding
	command string
	label   string
	current func(l *afc.Lane) string
}

var afcSettings = []afcSetting{
	{key: keys.AfcMap, command: afc.Map, label: "tool (e.g. T1)", current: func(l *afc.Lane) string { return strings.Join(l.Tools, ",") }},
	{key: keys.AfcMaterial, command: afc.SetMat, label: "material", current: func(l *afc.Lane) string { return l.Material }},
	{key: keys.AfcWeight, command: afc.SetWt, label: "remaining weight in g", current: func(l *afc.Lane) string {
		if l.HasWeight {
			return fmt.Sprintf("%g", l.Weight)
		}
		return ""
	}},
	{key: keys.AfcColor, command: afc.SetCol, label: "colour (hex, e.g. FF8800)", current: func(l *afc.Lane) string { return strings.TrimPrefix(l.Color, "#") }},
	{key: keys.AfcSpool, command: afc.SetSpl, label: "spool ID (0 clears)", current: func(l *afc.Lane) string {
		if l.SpoolID > 0 {
			return fmt.Sprint(l.SpoolID)
		}
		return ""
	}},
}

func (m *model) openAFCEdit(lane *afc.Lane, s afcSetting) {
	name := lane.Name
	m.openLineEdit(fmt.Sprintf("Lane %s %s:", name, s.label), s.current(lane), func(m *model, v string) (tea.Cmd, error) {
		// Re-check against fresh state: the lane may have changed while
		// the prompt was open.
		am := m.afcModel()
		if why := m.afcBlocked(am, false); why != "" {
			return nil, fmt.Errorf("%s", why)
		}
		l := am.Lane(name)
		if l == nil {
			return nil, fmt.Errorf("lane %s is no longer available", name)
		}
		g, err := afc.SetCommand(l, s.command, v)
		if err != nil {
			return nil, err
		}
		m.appendLog(">>> " + g)
		return m.requireConnected("AFC setting", m.sendGcode(g)), nil
	})
}

// ---------------------------------------------------------------------------
// One-line edit on the prompt line (shared; first used by AFC)
// ---------------------------------------------------------------------------

type lineEdit struct {
	prompt string
	input  textinput.Model
	apply  func(m *model, value string) (tea.Cmd, error)
	err    string
}

func (m *model) openLineEdit(prompt, initial string, apply func(m *model, v string) (tea.Cmd, error)) {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 120
	ti.Width = 30
	ti.SetValue(initial)
	ti.CursorEnd()
	ti.Focus()
	m.lineEdit = &lineEdit{prompt: prompt, input: ti, apply: apply}
}

// handleLineEditKey owns the keyboard while a line edit is open.
func (m *model) handleLineEditKey(msg tea.KeyMsg) tea.Cmd {
	le := m.lineEdit
	switch msg.String() {
	case "esc":
		m.lineEdit = nil
		return nil
	case "enter":
		cmd, err := le.apply(m, le.input.Value())
		if err != nil {
			le.err = err.Error() // stay open so it can be fixed
			return nil
		}
		m.lineEdit = nil
		return cmd
	}
	le.err = ""
	var cmd tea.Cmd
	le.input, cmd = le.input.Update(msg)
	return cmd
}

func renderLineEdit(le *lineEdit, width int) string {
	head := footerFocusStyle.Foreground(colorAccentWarm).Render("? " + le.prompt)
	line := head + " " + le.input.View() + footerText.Render("   enter set  •  esc cancel")
	if le.err != "" {
		line += "  " + consoleErrStyle.Render(le.err)
	}
	return lipgloss.NewStyle().MarginLeft(2).Render(clipWidth(line, width-3))
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// afcPaneLines is the number of content lines the pane wants.
func afcPaneLines(am *afc.Model) int {
	if am == nil {
		return 2
	}
	n := 1 + len(am.Lanes) // summary + lanes
	if am.Message != "" {
		n++
	}
	if len(am.Units) > 1 {
		n += len(am.Units)
	}
	return n
}

func (m model) renderAFC() string {
	am := m.afcModel()
	w := m.layout.w(focusAFC) - panelInnerXReserve()
	if am == nil {
		return consoleInfoStyle.Render("No AFC on this printer.")
	}
	var lines []string

	state := am.State
	stateStyle := consoleInfoStyle
	switch {
	case am.Error:
		state, stateStyle = "error", consoleErrStyle
	case am.Busy():
		stateStyle = consoleCmdStyle
	}
	summary := stateStyle.Render(state)
	if am.Current != "" {
		summary += consoleInfoStyle.Render(" · active ") + footerFocusStyle.Render(am.Current)
	}
	if am.Next != "" {
		summary += consoleInfoStyle.Render(" · next " + am.Next)
	}
	lines = append(lines, summary)
	if am.Message != "" {
		st := consoleErrStyle
		switch strings.ToLower(am.MessageType) {
		case "warning":
			st = consoleCmdStyle
		case "info", "success":
			st = consoleNoticeStyle
		}
		lines = append(lines, st.Render(am.Message)+consoleInfoStyle.Render("  (x clears)"))
	}

	selected := m.selectedLane(am)
	for _, u := range am.Units {
		if len(am.Units) > 1 {
			lines = append(lines, helpGroupStyle.Render(u.Name)+consoleInfoStyle.Render(" "+u.Type))
		}
		for _, l := range u.Lanes {
			lines = append(lines, renderLane(l, l == selected && m.focus == focusAFC))
		}
	}
	for i, l := range lines {
		lines[i] = clipWidth(l, w)
	}
	return strings.Join(lines, "\n")
}

func renderLane(l *afc.Lane, cursor bool) string {
	mark := "  "
	if cursor {
		mark = setupCursorStyle.Render("▸ ")
	}
	swatch := consoleInfoStyle.Render("··")
	if l.Color != "" && l.State != "Empty" {
		swatch = lipgloss.NewStyle().Foreground(lipgloss.Color(l.Color)).Render("██")
	}
	// Pad before styling: ANSI codes would throw off %-Ns widths.
	name := fmt.Sprintf("%-5s", l.Name+map[bool]string{true: "*"}[l.Active])
	if l.Active {
		name = footerFocusStyle.Render(name)
	}
	tool := strings.Join(l.Tools, "/")
	mat := l.Material
	if mat == "" {
		mat = "—"
	}
	weight := ""
	if l.HasWeight {
		weight = fmt.Sprintf(" %gg", l.Weight)
	}
	stateStyle := consoleInfoStyle
	switch l.State {
	case "In tool":
		stateStyle = consoleNoticeStyle
	case "Ready":
		stateStyle = footerText
	case "Empty", "Unknown", "Prep":
	default:
		stateStyle = consoleCmdStyle // a busy status: Tool Loading, Ejecting…
	}
	return fmt.Sprintf("%s%s %s %-5s %-6s%s  %s", mark, swatch, name, tool, mat, weight, stateStyle.Render(l.State))
}
