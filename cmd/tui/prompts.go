package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moonraker-tui/internal/afc"
	"github.com/justinh-rahb/moonraker-tui/internal/prompt"
)

// ---------------------------------------------------------------------------
// Phase 19: macro prompts and exclude object
//
// Macro prompts (internal/prompt) arrive as "// action:prompt_*" console
// lines. They're protocol, not output, so they're kept out of the
// console; when a macro shows one it takes the screen until the printer
// ends it (a button's gcode usually does). Esc closes it the way
// Mainsail's ✕ does: by sending the prompt_end itself. E-stop still
// works; ctrl+c still quits.
//
// Exclude object: during a print, O (or F4) lists the objects the slicer
// labelled — the one printing now, the ones already excluded — and
// enter excludes one, after a confirmation.
// ---------------------------------------------------------------------------

// feedPromptLines passes lines through the prompt parser and returns the
// rest (the ones that belong in the console).
func (m *model) feedPromptLines(lines []string) []string {
	var keep []string
	for _, l := range lines {
		if prompt.IsPromptLine(l) {
			if m.prompt.Feed(l) {
				m.promptCursor = 0
			}
			continue
		}
		keep = append(keep, l)
	}
	return keep
}

// promptButtons is every button in focus order: body, then footer.
func promptButtons(p *prompt.Prompt) []prompt.Button {
	return append(append([]prompt.Button{}, p.Buttons...), p.Footer...)
}

func (m *model) handlePromptKey(msg tea.KeyMsg) tea.Cmd {
	p := m.prompt.Shown
	btns := promptButtons(p)
	switch msg.String() {
	case "esc":
		return m.sendEchoed("close prompt", prompt.Close)
	case "left", "up", "shift+tab", "k", "h":
		if len(btns) > 0 {
			m.promptCursor = (m.promptCursor + len(btns) - 1) % len(btns)
		}
	case "right", "down", "tab", "j", "l":
		if len(btns) > 0 {
			m.promptCursor = (m.promptCursor + 1) % len(btns)
		}
	case "enter", " ":
		if len(btns) == 0 {
			return nil
		}
		// The prompt stays until the printer ends it.
		return m.sendEchoed("prompt button", btns[min(m.promptCursor, len(btns)-1)].Gcode)
	}
	return nil
}

func buttonStyle(color string, focused bool) lipgloss.Style {
	st := lipgloss.NewStyle().Padding(0, 1)
	fg := map[string]lipgloss.TerminalColor{
		"primary": colorAccentCool, "secondary": colorTextMuted, "info": colorBorderBlurred,
		"warning": colorAccentWarm, "error": colorErr,
	}[color]
	if fg == nil {
		fg = colorTitle
	}
	if focused {
		return st.Bold(true).Foreground(lipgloss.Color("0")).Background(fg)
	}
	return st.Foreground(fg)
}

func (m model) renderMacroPrompt() string {
	p := m.prompt.Shown
	w := min(72, max(40, m.width*2/3), m.width-4)
	inner := w - 4
	var lines []string
	for _, t := range p.Text {
		lines = append(lines, lipgloss.NewStyle().Width(inner).Render(t))
	}
	idx := 0
	button := func(b prompt.Button) string {
		s := buttonStyle(b.Color, idx == m.promptCursor).Render("[ " + b.Label + " ]")
		idx++
		return s
	}
	row := -1
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			lines = append(lines, "", strings.Join(cur, " "))
			cur = nil
		}
	}
	for _, b := range p.Buttons {
		if b.Row != row {
			flush()
			row = b.Row
		}
		cur = append(cur, button(b))
	}
	flush()
	if len(p.Footer) > 0 {
		var f []string
		for _, b := range p.Footer {
			f = append(f, button(b))
		}
		lines = append(lines, "", lipgloss.PlaceHorizontal(inner, lipgloss.Right, strings.Join(f, " ")))
	}
	for i, l := range lines {
		lines[i] = clipWidth(l, inner)
	}
	box := renderPanel(p.Title, strings.Join(lines, "\n"), true, w, 0)
	title := appTitleStyle.Render(appTitle + "  ·  Macro prompt")
	foot := footerStyle.Render("←→ tab select  •  enter press  •  esc close  •  ctrl+x e-stop")
	return title + "\n\n" + lipgloss.PlaceHorizontal(m.width, lipgloss.Center, box) + "\n\n" + clipWidth(foot, m.width) + "\n"
}

// --- exclude object --------------------------------------------------------

type exObject struct {
	name     string
	center   []float64
	excluded bool
	current  bool
}

func (m *model) excludeObjects() []exObject {
	eo := m.raw["exclude_object"]
	list, _ := eo["objects"].([]any)
	excluded := map[string]bool{}
	for _, n := range stringList(eo["excluded_objects"]) {
		excluded[n] = true
	}
	current, _ := eo["current_object"].(string)
	var out []exObject
	for _, o := range list {
		obj, _ := o.(map[string]any)
		name, _ := obj["name"].(string)
		if name == "" {
			continue
		}
		c, _ := floats(obj["center"])
		out = append(out, exObject{name: name, center: c, excluded: excluded[name], current: name == current})
	}
	return out
}

func stringList(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func canExclude(m *model) (bool, string) {
	return jobActive(m.job.State) && len(m.excludeObjects()) > 0, "during a print with labelled objects (exclude_object)"
}

func (m *model) openExclude() {
	m.exclude.open = true
	m.exclude.cursor = 0
	for i, o := range m.excludeObjects() {
		if o.current {
			m.exclude.cursor = i
		}
	}
}

func (m *model) handleExcludeKey(msg tea.KeyMsg) tea.Cmd {
	objs := m.excludeObjects()
	switch msg.String() {
	case "esc", "q":
		m.exclude.open = false
		return nil
	case "up", "k":
		if len(objs) > 0 {
			m.exclude.cursor = (m.exclude.cursor + len(objs) - 1) % len(objs)
		}
	case "down", "j":
		if len(objs) > 0 {
			m.exclude.cursor = (m.exclude.cursor + 1) % len(objs)
		}
	case "enter", "x":
		if len(objs) == 0 {
			return nil
		}
		o := objs[min(m.exclude.cursor, len(objs)-1)]
		if o.excluded {
			return m.refuse(o.name + " is already excluded")
		}
		if !jobActive(m.job.State) {
			return m.refuse("objects can only be excluded during a print")
		}
		name, err := afc.Param(o.name)
		if err != nil {
			return m.refuse("can't exclude " + o.name + ": " + err.Error())
		}
		m.confirmation = &pendingConfirmation{
			prompt: fmt.Sprintf("Exclude %s? Klipper skips it for the rest of this print.", o.name),
			onYes:  m.sendEchoedCmd("EXCLUDE_OBJECT NAME=" + name),
		}
	}
	return nil
}

// sendEchoedCmd is a deferred sendEchoed for confirmations: the echo
// happens when the Cmd runs (on yes), not when the prompt is armed.
func (m *model) sendEchoedCmd(gcode string) tea.Cmd {
	send := m.sendGcode(gcode)
	return func() tea.Msg {
		return echoThen{line: ">>> " + gcode, msg: send()}
	}
}

// echoThen logs a line, then delivers the wrapped message.
type echoThen struct {
	line string
	msg  tea.Msg
}

func (m model) renderExclude() string {
	objs := m.excludeObjects()
	title := appTitleStyle.Render(fmt.Sprintf("%s  ·  Objects (%d)", appTitle, len(objs)))
	var lines []string
	nameW := 0
	for _, o := range objs {
		nameW = max(nameW, lipgloss.Width(o.name))
	}
	nameW = min(nameW, max(10, m.width-40))
	for i, o := range objs {
		mark := "  "
		ns := footerText
		if i == m.exclude.cursor {
			mark = setupCursorStyle.Render("▸ ")
			ns = jogStepStyle
		}
		state := ""
		switch {
		case o.excluded:
			ns = consoleInfoStyle.Strikethrough(true)
			state = consoleInfoStyle.Render("excluded")
		case o.current:
			state = consoleNoticeStyle.Render("printing now")
		}
		at := ""
		if len(o.center) >= 2 {
			at = consoleInfoStyle.Render(fmt.Sprintf("  at %.0f, %.0f", o.center[0], o.center[1]))
		}
		lines = append(lines, clipWidth(mark+ns.Render(fmt.Sprintf("%-*s", nameW, clipWidth(o.name, nameW)))+at+"  "+state, m.width-4))
	}
	if len(objs) == 0 {
		lines = append(lines, consoleInfoStyle.Render("No labelled objects in this print."))
	}
	out := title + "\n\n" + bodyStyle.Render(strings.Join(lines, "\n")) + "\n\n"
	if m.confirmation != nil {
		out += renderPrompt(m.confirmation, m.width) + "\n"
	}
	return out + clipWidth(footerStyle.Render("↑↓ select  •  enter exclude (confirms)  •  esc close"), m.width) + "\n"
}
