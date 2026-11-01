package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Toolhead pane (pane 2)
//
// WASD jogs X and Y, space "jumps" (Z up) and c "crouches" (Z down), by
// the current step. The arrows are a cursor: ↑↓ pick X, Y, Z or the
// step; typing a number (or enter) edits the field in place, and enter
// sends an absolute move there — or, on the step row, uses that step.
// ← → change the step from any row.
//
// Positions are gcode coordinates (gcode_move.gcode_position), so what
// you type is what G1 moves to. Moves are refused while printing (pause
// first), on an axis Klipper reports as not homed, and outside the
// axis limits; Klipper would refuse those too, but less legibly.
// ---------------------------------------------------------------------------

type thField int

const (
	thX thField = iota
	thY
	thZ
	thStep
	thFieldCount
)

var axisNames = [3]string{"X", "Y", "Z"}

func newToolheadInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 9
	return ti
}

// canMove: no toolhead moves while a print is running (paused is fine,
// e.g. to clear the nozzle).
func canMove(m *model) (bool, string) {
	return m.job.State != "printing", "not while printing (pause first)"
}

// gcodePosition is the position in gcode coordinates when the printer
// reports it, otherwise the raw toolhead position.
func (m *model) gcodePosition() [3]float64 {
	if p, ok := floats(m.raw["gcode_move"]["gcode_position"]); ok && len(p) >= 3 {
		return [3]float64{p[0], p[1], p[2]}
	}
	return m.position
}

// axisHomed reports whether Klipper has homed an axis. Unknown (no
// homed_axes reported yet) counts as homed; Klipper still checks.
func (m *model) axisHomed(i int) bool {
	h, ok := m.raw["toolhead"]["homed_axes"].(string)
	return !ok || strings.Contains(strings.ToLower(h), strings.ToLower(axisNames[i]))
}

// checkAxisTarget validates an absolute gcode-coordinate target against
// the axis limits (which are in toolhead coordinates: gcode + offset).
func (m *model) checkAxisTarget(i int, target float64) error {
	lo, okLo := floats(m.raw["toolhead"]["axis_minimum"])
	hi, okHi := floats(m.raw["toolhead"]["axis_maximum"])
	if !okLo || !okHi || len(lo) <= i || len(hi) <= i {
		return nil
	}
	off := 0.0
	if o, ok := floats(m.raw["gcode_move"]["homing_origin"]); ok && len(o) > i {
		off = o[i]
	}
	if t := target + off; t < lo[i]-1e-6 || t > hi[i]+1e-6 {
		return fmt.Errorf("%s %g is outside %g to %g", axisNames[i], target, lo[i]-off, hi[i]-off)
	}
	return nil
}

func floats(v any) ([]float64, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]float64, 0, len(list))
	for _, x := range list {
		f, ok := x.(float64)
		if !ok {
			return nil, false
		}
		out = append(out, f)
	}
	return out, true
}

// moveRefusal says why an axis can't move now ("" = it can).
func (m *model) moveRefusal(axis int) string {
	if ok, why := canMove(m); !ok {
		return "moves are off " + why
	}
	if axis >= 0 && !m.axisHomed(axis) {
		return fmt.Sprintf("home %s first (%s, or H for all)", axisNames[axis], strings.ToLower(axisNames[axis]))
	}
	return ""
}

func (m *model) refuse(why string) tea.Cmd {
	m.appendLog("!! " + why)
	return nil
}

// handleJogKey routes a key while the Toolhead pane is focused (and not
// editing a field — see handleToolheadEditKey).
func (m *model) handleJogKey(msg tea.KeyMsg) tea.Cmd {
	step := m.steps[m.stepIndex]
	jog := func(axis int, sign float64) tea.Cmd {
		if why := m.moveRefusal(axis); why != "" {
			return m.refuse(why)
		}
		return m.sendJog(axisNames[axis], sign*step)
	}
	switch {
	case key.Matches(msg, *keys.JogX):
		if msg.String() == "a" {
			return jog(0, -1)
		}
		return jog(0, 1)
	case key.Matches(msg, *keys.JogY):
		if msg.String() == "s" {
			return jog(1, -1)
		}
		return jog(1, 1)
	case key.Matches(msg, *keys.JogZ):
		if msg.String() == "c" || msg.String() == "pgdown" {
			return jog(2, -1)
		}
		return jog(2, 1)
	case key.Matches(msg, *keys.ThMove):
		switch msg.String() {
		case "up", "k":
			m.thCursor = (m.thCursor + thFieldCount - 1) % thFieldCount
		default:
			m.thCursor = (m.thCursor + 1) % thFieldCount
		}
	case key.Matches(msg, *keys.StepDown):
		if m.stepIndex > 0 {
			m.stepIndex--
		}
	case key.Matches(msg, *keys.StepUp):
		if m.stepIndex < len(m.steps)-1 {
			m.stepIndex++
		}
	case key.Matches(msg, *keys.ThEdit):
		m.startToolheadEdit(m.fieldValue(m.thCursor))
	case msg.Type == tea.KeyRunes && strings.Trim(string(msg.Runes), "0123456789.-") == "":
		// Typing a number starts a fresh value in the selected field.
		m.startToolheadEdit("")
		return m.handleToolheadEditKey(msg)
	case key.Matches(msg, *keys.HomeAll):
		return m.home("G28", "home")
	case key.Matches(msg, *keys.HomeX):
		return m.home("G28 X", "home X")
	case key.Matches(msg, *keys.HomeY):
		return m.home("G28 Y", "home Y")
	case key.Matches(msg, *keys.HomeZ):
		return m.home("G28 Z", "home Z")
	case key.Matches(msg, *keys.Endstops):
		return m.requireConnected("endstops", queryEndstopsCmd(m.client))
	case key.Matches(msg, *keys.Level):
		return m.runLeveling()
	case key.Matches(msg, *keys.MotorsOff):
		if ok, why := whileIdle(m); !ok {
			return m.refuse("motors off only works " + why)
		}
		return m.sendEchoed("motors off", "M84")
	}
	return nil
}

// sendEchoed sends a one-off command and shows it in the console, as if
// typed: Klipper prints nothing for a successful M84 or G28, so without
// the echo a key press would leave no trace. (Jogs aren't echoed; they'd
// flood the console.)
func (m *model) sendEchoed(what, gcode string) tea.Cmd {
	cmd := m.requireConnected(what, m.sendGcode(gcode))
	if cmd != nil && m.connected() {
		m.appendLog(">>> " + gcode)
	}
	return cmd
}

func (m *model) home(gcode, what string) tea.Cmd {
	if why := m.moveRefusal(-1); why != "" {
		return m.refuse(why)
	}
	return m.sendEchoed(what, gcode)
}

// fieldValue is a field's current value as editable text.
func (m *model) fieldValue(f thField) string {
	if f == thStep {
		return strconv.FormatFloat(m.steps[m.stepIndex], 'f', -1, 64)
	}
	return strconv.FormatFloat(m.gcodePosition()[f], 'f', 2, 64)
}

func (m *model) startToolheadEdit(initial string) {
	m.thEditing = true
	m.thInput.SetValue(initial)
	m.thInput.CursorEnd()
	m.thInput.Focus()
}

func (m *model) endToolheadEdit() {
	m.thEditing = false
	m.thInput.Blur()
}

// handleToolheadEditKey owns the keyboard while a field is being typed.
func (m *model) handleToolheadEditKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.endToolheadEdit()
		return nil
	case "enter":
		raw := strings.TrimSpace(m.thInput.Value())
		m.endToolheadEdit()
		if raw == "" {
			return nil
		}
		return m.applyToolheadField(m.thCursor, raw)
	case "up", "down":
		m.endToolheadEdit()
		return m.handleJogKey(msg)
	case "tab", "shift+tab":
		m.endToolheadEdit()
		if msg.String() == "tab" {
			m.cycleFocus(1)
		} else {
			m.cycleFocus(-1)
		}
		return nil
	}
	allowed := "0123456789.-"
	if m.thCursor == thStep {
		allowed = "0123456789."
	}
	if isTextKey(msg) && strings.Trim(string(msg.Runes), allowed) != "" {
		return nil
	}
	var cmd tea.Cmd
	m.thInput, cmd = m.thInput.Update(msg)
	return cmd
}

// applyToolheadField acts on a typed value: an absolute move for an
// axis, a custom step for the step row.
func (m *model) applyToolheadField(f thField, raw string) tea.Cmd {
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return m.refuse(fmt.Sprintf("%q is not a number", raw))
	}
	if f == thStep {
		if v <= 0 || v > 1000 {
			return m.refuse("a step must be more than 0 and at most 1000 mm")
		}
		m.useStep(v)
		return nil
	}
	axis := int(f)
	if why := m.moveRefusal(axis); why != "" {
		return m.refuse(why)
	}
	if err := m.checkAxisTarget(axis, v); err != nil {
		return m.refuse(err.Error())
	}
	return m.sendMoveTo(axisNames[axis], v)
}

// useStep selects step v, adding it to this session's steps if it isn't
// one already (permanent steps live in setup → Toolhead).
func (m *model) useStep(v float64) {
	for i, s := range m.steps {
		if s == v {
			m.stepIndex = i
			return
		}
	}
	steps := append(append([]float64(nil), m.steps...), v)
	sort.Float64s(steps)
	m.steps = steps
	m.stepIndex = nearestIndex(steps, v)
}

// sendJog emits a relative move on one axis, wrapped in
// SAVE/RESTORE_GCODE_STATE so the printer's G90/G91 mode is untouched.
func (m *model) sendJog(axis string, delta float64) tea.Cmd {
	return m.sendMove(axis, "G91", delta, "jog")
}

// sendMoveTo moves one axis to an absolute gcode coordinate.
func (m *model) sendMoveTo(axis string, pos float64) tea.Cmd {
	if m.connected() {
		m.appendLog(fmt.Sprintf(">>> G1 %s%g (absolute)", axis, pos))
	}
	return m.sendMove(axis, "G90", pos, "move")
}

func (m *model) sendMove(axis, mode string, v float64, what string) tea.Cmd {
	feed := m.cfg.Toolhead.XY()
	if axis == "Z" {
		feed = m.cfg.Toolhead.Z()
	}
	script := strings.Join([]string{
		"SAVE_GCODE_STATE NAME=_ui_movement",
		mode,
		fmt.Sprintf("G1 %s%g F%d", axis, v, feed),
		"RESTORE_GCODE_STATE NAME=_ui_movement",
	}, "\n")
	return m.requireConnected(what, m.sendGcode(script))
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// renderJog draws the pane: the X/Y/Z/step fields (cursor-selectable),
// the WASD pad, and the remaining keys. Always jogContentMin lines.
func (m model) renderJog() string {
	contentW := m.layout.w(focusJog) - panelInnerXReserve()
	if contentW < 10 {
		contentW = 10
	}
	focused := m.focus == focusJog
	pos := m.gcodePosition()

	field := func(f thField, label, value, note string) string {
		mark := "  "
		lst := jogHintStyle
		if focused && f == m.thCursor {
			mark = setupCursorStyle.Render("▸ ")
			lst = jogStepStyle
		}
		if focused && m.thEditing && f == m.thCursor {
			value = jogStepStyle.Render("→ " + m.thInput.Value() + "▏")
			note = ""
		}
		return clipWidth(mark+lst.Render(label)+" "+value+note, contentW)
	}
	// "▸ X   " + value + note: the value column shrinks with the pane
	// (8 wide normally, never cut), so narrow panes lose padding, not
	// digits.
	valW := contentW - 6 - 2
	if valW > 8 {
		valW = 8
	}
	var rows []string
	for i := 0; i < 3; i++ {
		txt := fmt.Sprintf("%*.2f", valW, pos[i])
		v := jogPosStyle.Render(txt)
		note := ""
		if !m.axisHomed(i) {
			v = jogHintStyle.Render(txt)
			note = jogHintStyle.Render(" ?")
			if contentW >= 6+len(txt)+8 {
				note = jogHintStyle.Render(" unhomed")
			}
		}
		rows = append(rows, field(thField(i), axisNames[i]+"  ", v, note))
	}
	rows = append(rows, field(thStep, "step", jogStepStyle.Render(fmt.Sprintf("%g mm", m.steps[m.stepIndex])), ""))

	padStyle := jogPadStyle
	if ok, _ := canMove(&m); !ok {
		padStyle = jogHintStyle
	}
	pad := padStyle.Render(strings.Join([]string{
		"  w      ␣ Z↑",
		"a + d    c Z↓",
		"  s          ",
	}, "\n"))
	pad = lipgloss.PlaceHorizontal(contentW, lipgloss.Center, pad)

	// Every Toolhead action is on screen with its key — the pane has
	// no hidden controls. Narrower panes get tighter wording, always 3
	// lines (jogContentMin depends on it). Full text is in ? help.
	idle, _ := whileIdle(&m)
	motors := "m motors off"
	if !idle {
		motors = "m motors off (idle)"
	}
	hints := []string{"↑↓ field  ← → step", "H home   x y z axis", motors}
	switch {
	case contentW >= 21:
	case contentW >= 18:
		if !idle {
			motors = "m off (idle)"
		}
		hints = []string{"↑↓ field ← → step", "H home xyz axis", motors}
	default:
		motors = "m off  ? keys"
		if !idle {
			motors = "? all keys"
		}
		hints = []string{"↑↓ fld ←→ step", "H home  x y z", motors}
	}
	for i, h := range hints {
		hints[i] = jogHintStyle.Render(clipWidth(h, contentW))
	}

	out := append(rows, "")
	out = append(out, pad, "")
	out = append(out, hints...)
	if lv := m.levelingLine(contentW); lv != "" {
		out = append(out, lv)
	}
	return strings.Join(out, "\n")
}
