package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handleKey is the dashboard's single key entry point. Order matters:
//
//  1. help overlay (when open) owns every key but quit and E-stop,
//  2. E-stop arms from anywhere, replacing any pending prompt,
//  3. a pending confirmation swallows everything but y / n / esc,
//  4. global and job/Klipper keys — unless the focused widget is
//     taking text (capturesText); job/Klipper keys also yield to a
//     pane that binds the same key (paneClaims),
//  5. the focused pane.
func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, *keys.EStop) {
		m.showHelp = false
		m.setup.open = false
		m.browser.open = false
		m.macroForm = nil
		return m, m.armEmergencyStop()
	}

	if m.setup.open {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m, m.handleSetupKey(msg)
	}

	// The macro form and browser are full screens that own the keys.
	if m.macroForm != nil {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m, m.handleMacroFormKey(msg)
	}
	if m.browser.open {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m, m.handleBrowserKey(msg)
	}

	if m.showHelp {
		switch {
		case msg.String() == "ctrl+c" || msg.String() == "f10":
			return m.quit()
		case key.Matches(msg, *keys.Help), msg.String() == "esc", msg.String() == "q":
			m.showHelp = false
		case msg.String() == "up" || msg.String() == "k":
			m.helpScroll--
		case msg.String() == "down" || msg.String() == "j":
			m.helpScroll++
		case msg.String() == "pgup":
			m.helpScroll -= m.height / 2
		case msg.String() == "pgdown" || msg.String() == " ":
			m.helpScroll += m.height / 2
		}
		m.clampHelpScroll()
		return m, nil
	}

	if m.confirmation != nil && len(m.confirmation.options) > 0 {
		for _, o := range m.confirmation.options {
			if msg.String() == o.key {
				m.confirmation = nil
				return m, o.run(&m)
			}
		}
		if msg.String() == "esc" || msg.String() == "n" {
			m.confirmation = nil
		}
		return m, nil
	}

	if m.confirmation != nil {
		switch {
		case key.Matches(msg, *keys.Confirm):
			cmd := m.confirmation.onYes
			m.confirmation = nil
			return m, m.requireConnected("action", cmd)
		case key.Matches(msg, *keys.Deny):
			m.confirmation = nil
		}
		return m, nil
	}

	// A one-line edit (AFC lane settings) owns the keyboard.
	if m.lineEdit != nil {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m, m.handleLineEditKey(msg)
	}

	// Typing a toolhead position or step, or a Tuning / Extruder
	// value, owns the keyboard too.
	if m.thEditing {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m, m.handleToolheadEditKey(msg)
	}
	if m.editingField() {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		fl, rows := m.fieldsFor(m.focus)
		return m, m.handleFieldEditKey(fl, rows, msg)
	}

	// Editing a heater target owns the keyboard (ctrl+c still quits).
	if m.tempEdit != "" {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m, m.handleTempEditKey(msg)
	}

	if !m.capturesText(msg) {
		switch {
		case key.Matches(msg, *keys.Quit):
			return m.quit()
		case key.Matches(msg, *keys.Help):
			m.showHelp = true
			m.helpScroll = 0
			return m, nil
		case key.Matches(msg, *keys.Setup):
			m.openSetup()
			return m, nil
		case key.Matches(msg, *keys.Macros):
			m.openBrowser()
			return m, nil
		case key.Matches(msg, *keys.NextPane):
			m.cycleFocus(1)
			return m, nil
		case key.Matches(msg, *keys.PrevPane):
			m.cycleFocus(-1)
			return m, nil
		case key.Matches(msg, *keys.JumpPane):
			if f := paneOrder[int(msg.Runes[0]-'1')]; !m.hiddenPanes()[f] {
				m.setFocus(f)
			}
			return m, nil
		case key.Matches(msg, *keys.ToggleFilter):
			m.showFiltered = !m.showFiltered
			m.refreshConsole()
			return m, nil
		}
		// A key the focused pane binds itself (c = Z down in Toolhead)
		// goes to the pane, not to the job or Klipper controls.
		if !m.paneClaims(msg) {
			if cmd, handled := m.handleJobControl(msg); handled {
				return m, cmd
			}
			if cmd, handled := m.handleKlippyControl(msg); handled {
				return m, cmd
			}
		}
	}

	switch m.focus {
	case focusConsole:
		return m, m.handleConsoleKey(msg)
	case focusTable:
		return m, m.handleTempsKey(msg)
	case focusJog:
		return m, m.handleJogKey(msg)
	case focusFiles:
		return m, m.handleFilesKey(msg)
	case focusTuning:
		return m, m.handleTuningKey(msg)
	case focusAFC:
		return m, m.handleAFCKey(msg)
	case focusExtruder:
		return m, m.handleExtruderKey(msg)
	case focusOutputs:
		return m, m.handleOutputsKey(msg)
	case focusMacros:
		return m, m.handleMacrosKey(msg)
	}
	return m, nil
}

func (m model) quit() (tea.Model, tea.Cmd) {
	m.client.Close()
	return m, tea.Quit
}

// ---------------------------------------------------------------------------
// Help overlay
// ---------------------------------------------------------------------------

// helpLines renders the registry grouped by scope. Keys that apply
// right now are bright; ones waiting on state are dimmed with the
// reason; the focused pane's group is marked.
func (m *model) helpLines() []string {
	keyW := 0
	for _, e := range registry {
		if w := lipgloss.Width(e.b.Help().Key); w > keyW {
			keyW = w
		}
	}
	var lines []string
	cur := keyScope(-1)
	focusScope := paneScope(m.focus)
	for _, e := range registry {
		if e.scope != cur {
			cur = e.scope
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			title := cur.title()
			if n := scopePaneNumber(cur); n > 0 {
				title = fmt.Sprintf("%d %s", n, title)
			}
			if cur == focusScope {
				title += "  ◂ focused"
			}
			lines = append(lines, helpGroupStyle.Render(title))
		}
		h := e.b.Help()
		k := fmt.Sprintf("  %-*s  ", keyW, h.Key)
		ok, why := e.active(m)
		if ok && (e.scope == scopeJob || e.scope == scopeKlipper) && m.paneShadows(e.b) {
			ok, why = false, "not from the "+paneName(m.focus)+" pane, which uses this key"
		}
		if ok {
			lines = append(lines, helpKeyStyle.Render(k)+h.Desc)
		} else {
			lines = append(lines, consoleInfoStyle.Render(k+h.Desc+" — "+why))
		}
	}
	lines = append(lines, "",
		consoleInfoStyle.Render("While the console, a file filter or a Tuning field is taking text,"),
		consoleInfoStyle.Render("printable keys type text; ctrl-keys, F-keys, tab and arrows still work."),
		consoleInfoStyle.Render("In Toolhead, Tuning, Extruder and Outputs, digits type a value; use tab to move on."))
	return lines
}

func scopePaneNumber(s keyScope) int {
	for _, f := range paneOrder {
		if paneScope(f) == s {
			return paneNumber(f)
		}
	}
	return 0
}

// helpBodyHeight is how many help lines fit under the overlay's
// title and above its footer.
func (m *model) helpBodyHeight() int {
	h := m.height - 4
	if h < 3 {
		h = 3
	}
	return h
}

func (m *model) clampHelpScroll() {
	max := len(m.helpLines()) - m.helpBodyHeight()
	if m.helpScroll > max {
		m.helpScroll = max
	}
	if m.helpScroll < 0 {
		m.helpScroll = 0
	}
}

func (m model) renderHelp() string {
	lines := m.helpLines()
	h := m.helpBodyHeight()
	start := m.helpScroll
	if start > len(lines) {
		start = len(lines)
	}
	end := start + h
	if end > len(lines) {
		end = len(lines)
	}
	shown := make([]string, 0, end-start)
	for _, l := range lines[start:end] {
		shown = append(shown, clipWidth(l, m.width-3)) // never wrap: see renderSetup
	}
	body := strings.Join(shown, "\n")
	more := ""
	if len(lines) > h {
		more = fmt.Sprintf("  •  %d–%d of %d", start+1, end, len(lines))
	}
	title := appTitleStyle.Render(appTitle + "  ·  Keys")
	foot := footerStyle.Render("↑↓ j k scroll  •  esc / ? close  •  ctrl+x e-stop" + more)
	return title + "\n\n" + bodyStyle.Render(body) + "\n" + foot + "\n"
}
