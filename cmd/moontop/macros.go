package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/afc"
	"github.com/justinh-rahb/moontop/internal/macros"
)

// ---------------------------------------------------------------------------
// Phase 18: macros
//
// The printer's macros (internal/macros: names, descriptions, parameters
// found in the templates) appear in two places:
//
//   - the Macros pane (9, under AFC in the bottom-left column) holds only
//     the macros pinned for this printer (stored in its Moonraker
//     database), since a printer can have dozens;
//   - the macro browser (F3 or g, from anywhere) lists them all: type to
//     filter, enter runs, space pins or unpins.
//
// A macro with parameters opens a form first, the defaults shown as
// placeholders; only the fields filled in are sent (as Mainsail does).
// Values that could add a gcode command are refused.
// ---------------------------------------------------------------------------

type macroBrowser struct {
	open   bool
	filter textinput.Model
	cursor int
}

type macroForm struct {
	macro  macros.Macro
	inputs []textinput.Model
	cursor int
	err    string
}

func newMacroFilter() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "filter › "
	ti.Placeholder = "type to filter"
	ti.CharLimit = 40
	return ti
}

// pinnedMacros are the pinned macros the printer still has.
func (m *model) pinnedMacros() []macros.Macro {
	var out []macros.Macro
	for _, mc := range m.macros {
		if m.printerCfg.MacroPinned(mc.Name) {
			out = append(out, mc)
		}
	}
	return out
}

// filteredMacros are the browser's rows: name or description contains
// the filter (case-insensitive). Space pins in the browser, so the
// filter is one word — macro names have no spaces anyway.
func (m *model) filteredMacros() []macros.Macro {
	words := strings.Fields(strings.ToLower(m.browser.filter.Value()))
	var out []macros.Macro
	for _, mc := range m.macros {
		hay := strings.ToLower(mc.Name + " " + mc.Description)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, mc)
		}
	}
	return out
}

// runMacro runs a macro without parameters, or opens its form.
func (m *model) runMacro(mc macros.Macro) tea.Cmd {
	if len(mc.Params) > 0 {
		m.openMacroForm(mc)
		return nil
	}
	return m.sendEchoed("macro "+mc.Name, mc.Name)
}

func (m *model) togglePin(mc macros.Macro) tea.Cmd {
	pinned := !m.printerCfg.MacroPinned(mc.Name)
	m.printerCfg.SetMacroPinned(mc.Name, pinned)
	return m.savePrinter()
}

// --- pane ------------------------------------------------------------------

func (m *model) handleMacrosKey(msg tea.KeyMsg) tea.Cmd {
	pinned := m.pinnedMacros()
	if len(pinned) == 0 {
		return nil
	}
	m.macroCursor = min(max(m.macroCursor, 0), len(pinned)-1)
	switch {
	case key.Matches(msg, *keys.MacroMove):
		switch msg.String() {
		case "up", "k":
			m.macroCursor = (m.macroCursor + len(pinned) - 1) % len(pinned)
		default:
			m.macroCursor = (m.macroCursor + 1) % len(pinned)
		}
	case key.Matches(msg, *keys.MacroRun):
		return m.runMacro(pinned[m.macroCursor])
	case key.Matches(msg, *keys.MacroUnpin):
		return m.togglePin(pinned[m.macroCursor])
	}
	return nil
}

// macrosContentLines: one per pinned macro, or the two-line hint.
func macrosContentLines(m *model) int { return max(2, len(m.pinnedMacros())) }

// macroLine is "▸ NAME …  description", clipped to width.
func macroLine(mc macros.Macro, cursor, pinned bool, width int) string {
	mark := "  "
	nameStyle := footerText
	if cursor {
		mark = setupCursorStyle.Render("▸ ")
		nameStyle = jogStepStyle
	}
	star := ""
	if pinned {
		star = jogStepStyle.Render("★ ")
	}
	line := mark + star + nameStyle.Render(mc.Name)
	if len(mc.Params) > 0 {
		line += jogHintStyle.Render(" …")
	}
	if mc.Description != "" {
		line += jogHintStyle.Render("  " + mc.Description)
	}
	return clipWidth(line, width)
}

func (m model) renderMacros() string {
	w := m.layout.w(focusMacros) - panelInnerXReserve()
	pinned := m.pinnedMacros()
	if len(pinned) == 0 {
		return strings.Join([]string{
			jogHintStyle.Render(clipWidth("No macros pinned here.", w)),
			jogHintStyle.Render(clipWidth(fmt.Sprintf("F3 or g: all %d, space pins", len(m.macros)), w)),
		}, "\n")
	}
	cur := min(max(m.macroCursor, 0), len(pinned)-1)
	lines := make([]string, len(pinned))
	for i, mc := range pinned {
		lines[i] = macroLine(mc, m.focus == focusMacros && i == cur, false, w)
	}
	// Scroll to the cursor when the box is short.
	if h := m.layout.h(focusMacros) - 3; h > 0 && len(lines) > h {
		start := max(0, cur-h+1)
		lines = lines[start : start+h]
	}
	return strings.Join(lines, "\n")
}

// --- browser ---------------------------------------------------------------

func (m *model) openBrowser() {
	m.browser.open = true
	m.browser.cursor = 0
	m.browser.filter.SetValue("")
	m.browser.filter.Focus()
}

func (m *model) handleBrowserKey(msg tea.KeyMsg) tea.Cmd {
	rows := m.filteredMacros()
	switch msg.String() {
	case "esc":
		if m.browser.filter.Value() != "" {
			m.browser.filter.SetValue("")
			m.browser.cursor = 0
			return nil
		}
		m.browser.open = false
		m.browser.filter.Blur()
		return nil
	case "up", "ctrl+p":
		if len(rows) > 0 {
			m.browser.cursor = (m.browser.cursor + len(rows) - 1) % len(rows)
		}
		return nil
	case "down", "ctrl+n":
		if len(rows) > 0 {
			m.browser.cursor = (m.browser.cursor + 1) % len(rows)
		}
		return nil
	case "pgup":
		m.browser.cursor = max(0, m.browser.cursor-m.browserRows())
		return nil
	case "pgdown":
		m.browser.cursor = min(max(0, len(rows)-1), m.browser.cursor+m.browserRows())
		return nil
	case "enter":
		if len(rows) == 0 {
			return nil
		}
		mc := rows[min(m.browser.cursor, len(rows)-1)]
		m.browser.open = false
		m.browser.filter.Blur()
		return m.runMacro(mc)
	case " ":
		// Macro names have no spaces, so space pins rather than filters.
		if len(rows) == 0 {
			return nil
		}
		return m.togglePin(rows[min(m.browser.cursor, len(rows)-1)])
	}
	var cmd tea.Cmd
	m.browser.filter, cmd = m.browser.filter.Update(msg)
	m.browser.cursor = 0
	return cmd
}

// browserRows is how many macros fit under the browser's header.
func (m *model) browserRows() int { return max(3, m.height-6) }

func (m model) renderBrowser() string {
	rows := m.filteredMacros()
	title := appTitleStyle.Render(fmt.Sprintf("%s  ·  Macros (%d)", appTitle, len(m.macros)))
	var lines []string
	lines = append(lines, "  "+m.browser.filter.View())
	n := m.browserRows()
	cur := min(m.browser.cursor, max(0, len(rows)-1))
	start := 0
	if cur >= n {
		start = cur - n + 1
	}
	for i := start; i < len(rows) && i < start+n; i++ {
		mc := rows[i]
		line := macroLine(mc, i == cur, m.printerCfg.MacroPinned(mc.Name), m.width-6)
		if !m.printerCfg.MacroPinned(mc.Name) {
			line = "  " + line // align with starred rows
		}
		lines = append(lines, line)
	}
	if len(rows) == 0 {
		lines = append(lines, consoleInfoStyle.Render("  no macro matches"))
	}
	more := ""
	if len(rows) > n {
		more = fmt.Sprintf("  •  %d–%d of %d", start+1, min(len(rows), start+n), len(rows))
	}
	foot := footerStyle.Render("type to filter  •  ↑↓ select  •  enter run  •  space pin to the Macros pane  •  esc close" + more)
	return title + "\n\n" + bodyStyle.Render(strings.Join(lines, "\n")) + "\n" + clipWidth(foot, m.width) + "\n"
}

// --- parameter form --------------------------------------------------------

func (m *model) openMacroForm(mc macros.Macro) {
	f := &macroForm{macro: mc}
	for i := range mc.Params {
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 80
		ti.Width = 30
		if i == 0 {
			ti.Focus()
		}
		f.inputs = append(f.inputs, ti)
	}
	m.macroForm = f
}

func (f *macroForm) focus(i int) {
	f.inputs[f.cursor].Blur()
	f.cursor = (i + len(f.inputs)) % len(f.inputs)
	f.inputs[f.cursor].Focus()
}

// macroCommand builds "NAME P=v …" from the filled fields.
func macroCommand(f *macroForm) (string, error) {
	cmd := f.macro.Name
	for i, p := range f.macro.Params {
		v := strings.TrimSpace(f.inputs[i].Value())
		if v == "" {
			continue // Klipper's (or the template's) default applies
		}
		q, err := afc.Param(v)
		if err != nil {
			return "", fmt.Errorf("%s: %v", p.Name, err)
		}
		cmd += " " + p.Name + "=" + q
	}
	return cmd, nil
}

func (m *model) handleMacroFormKey(msg tea.KeyMsg) tea.Cmd {
	f := m.macroForm
	switch msg.String() {
	case "esc":
		m.macroForm = nil
		return nil
	case "up", "shift+tab":
		f.focus(f.cursor - 1)
		return nil
	case "down", "tab":
		f.focus(f.cursor + 1)
		return nil
	case "enter":
		cmd, err := macroCommand(f)
		if err != nil {
			f.err = err.Error()
			return nil
		}
		m.macroForm = nil
		return m.sendEchoed("macro "+f.macro.Name, cmd)
	}
	f.err = ""
	var cmd tea.Cmd
	f.inputs[f.cursor], cmd = f.inputs[f.cursor].Update(msg)
	return cmd
}

func (m model) renderMacroForm() string {
	f := m.macroForm
	title := appTitleStyle.Render(appTitle + "  ·  Run " + f.macro.Name)
	var lines []string
	if f.macro.Description != "" {
		lines = append(lines, consoleInfoStyle.Render(f.macro.Description), "")
	}
	nameW := 0
	for _, p := range f.macro.Params {
		nameW = max(nameW, len(p.Name))
	}
	for i, p := range f.macro.Params {
		mark := "  "
		ns := jogHintStyle
		if i == f.cursor {
			mark = setupCursorStyle.Render("▸ ")
			ns = jogStepStyle
		}
		def := ""
		if p.Default != "" {
			def = consoleInfoStyle.Render("  default " + p.Default)
		}
		lines = append(lines, clipWidth(mark+ns.Render(fmt.Sprintf("%-*s", nameW, p.Name))+"  "+f.inputs[i].View()+def, m.width-6))
	}
	lines = append(lines, "", consoleInfoStyle.Render("Empty fields are left out, so the macro's own defaults apply."))
	if f.err != "" {
		lines = append(lines, consoleErrStyle.Render(f.err))
	}
	if cmd, err := macroCommand(f); err == nil {
		lines = append(lines, "", footerText.Render("sends  ")+consoleCmdStyle.Render(cmd))
	}
	foot := footerStyle.Render("↑↓ tab field  •  type a value  •  enter run  •  esc cancel")
	body := make([]string, len(lines))
	for i, l := range lines {
		body[i] = clipWidth(l, m.width-3)
	}
	return title + "\n\n" + bodyStyle.Render(strings.Join(body, "\n")) + "\n\n" + clipWidth(foot, m.width) + "\n"
}
