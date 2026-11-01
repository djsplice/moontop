package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Phase 16: the numeric field list
//
// One widget for every "label: value" pane (Tuning, Extruder), with the
// same keys as the Toolhead fields: ↑↓ pick a row, ←→ adjust it by its
// step (shift ×10), or type a number (or enter) to edit it in place and
// enter to set it. Each row says how to read its live value and how to
// change it; the widget never shows a value the printer hasn't reported
// (the display stays confirmed state).
// ---------------------------------------------------------------------------

// numField is one row.
type numField struct {
	label string
	unit  string
	prec  int // decimals shown

	// value is the live value; ok=false shows "—".
	value func(m *model) (v float64, ok bool)
	// show overrides the formatted value (e.g. "T1" for a tool); showM
	// does too, with the model at hand (an LED's colour swatch).
	show  func(v float64) string
	showM func(m *model, v float64) string

	step     float64   // ←→ change; shift+←→ is ×10. 0 = no arrows
	choices  []float64 // when set, ←→ step through these instead
	min, max float64   // bounds for typed and adjusted values
	negative bool      // a typed value may be negative

	// set applies an absolute value; nudge applies ←→ (default: set
	// value ± step).
	set   func(m *model, v float64) tea.Cmd
	nudge func(m *model, delta float64) tea.Cmd

	when func(m *model) (bool, string) // nil = always available
	hint string                        // shown dimmed after the value
}

func (f numField) format(v float64) string {
	if f.show != nil {
		return f.show(v)
	}
	s := strconv.FormatFloat(v, 'f', f.prec, 64)
	if f.unit != "" {
		s += " " + f.unit
	}
	return s
}

// fieldList is a pane's cursor and in-place edit.
type fieldList struct {
	cursor  int
	editing bool
	input   textinput.Model
}

func newFieldList() fieldList {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 10
	return fieldList{input: ti}
}

// fieldsFor returns the focused pane's list and rows, or nil.
func (m *model) fieldsFor(f focusArea) (*fieldList, []numField) {
	switch f {
	case focusTuning:
		return &m.tuneFields, tuningFields(m)
	case focusExtruder:
		return &m.extFields, extruderFields(m)
	case focusOutputs:
		return &m.outFields, outputFields(m)
	}
	return nil, nil
}

// editingField reports whether the focused pane is typing a value.
func (m *model) editingField() bool {
	fl, _ := m.fieldsFor(m.focus)
	return fl != nil && fl.editing
}

// handleFieldKey handles the shared keys; handled=false lets the pane
// try its own (extrude, retract).
func (m *model) handleFieldKey(fl *fieldList, rows []numField, msg tea.KeyMsg) (tea.Cmd, bool) {
	if len(rows) == 0 {
		return nil, false
	}
	if fl.cursor >= len(rows) {
		fl.cursor = len(rows) - 1
	}
	row := rows[fl.cursor]
	switch {
	case key.Matches(msg, *keys.FieldMove):
		switch msg.String() {
		case "up", "k":
			fl.cursor = (fl.cursor + len(rows) - 1) % len(rows)
		default:
			fl.cursor = (fl.cursor + 1) % len(rows)
		}
		return nil, true
	case key.Matches(msg, *keys.FieldAdjust):
		dir := 1.0
		if strings.HasSuffix(msg.String(), "left") {
			dir = -1
		}
		return m.adjustField(row, dir, strings.HasPrefix(msg.String(), "shift+")), true
	case key.Matches(msg, *keys.FieldEdit):
		if ok, why := row.available(m); !ok {
			return m.refuse(row.label + ": " + why), true
		}
		v, known := row.value(m)
		initial := ""
		if known {
			initial = strconv.FormatFloat(v, 'f', -1, 64)
			if row.prec >= 0 {
				initial = strconv.FormatFloat(v, 'f', row.prec, 64)
			}
		}
		fl.startEdit(initial)
		return nil, true
	case msg.Type == tea.KeyRunes && fieldChars(row, string(msg.Runes)):
		if ok, why := row.available(m); !ok {
			return m.refuse(row.label + ": " + why), true
		}
		fl.startEdit("")
		return m.handleFieldEditKey(fl, rows, msg), true
	}
	return nil, false
}

// fieldChars reports whether s is something a number in row can hold.
func fieldChars(row numField, s string) bool {
	allowed := "0123456789."
	if row.negative {
		allowed += "-"
	}
	return s != "" && strings.Trim(s, allowed) == ""
}

func (f numField) available(m *model) (bool, string) {
	if f.when == nil {
		return true, ""
	}
	return f.when(m)
}

func (fl *fieldList) startEdit(initial string) {
	fl.editing = true
	fl.input.SetValue(initial)
	fl.input.CursorEnd()
	fl.input.Focus()
}

func (fl *fieldList) endEdit() {
	fl.editing = false
	fl.input.Blur()
}

// handleFieldEditKey owns the keyboard while a value is typed.
func (m *model) handleFieldEditKey(fl *fieldList, rows []numField, msg tea.KeyMsg) tea.Cmd {
	if fl.cursor >= len(rows) {
		fl.endEdit()
		return nil
	}
	row := rows[fl.cursor]
	switch msg.String() {
	case "esc":
		fl.endEdit()
		return nil
	case "enter":
		raw := strings.TrimSpace(fl.input.Value())
		fl.endEdit()
		if raw == "" {
			return nil
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return m.refuse(fmt.Sprintf("%s: %q is not a number", row.label, raw))
		}
		return m.setField(row, v)
	case "up", "down":
		fl.endEdit()
		cmd, _ := m.handleFieldKey(fl, rows, msg)
		return cmd
	case "tab", "shift+tab":
		fl.endEdit()
		if msg.String() == "tab" {
			m.cycleFocus(1)
		} else {
			m.cycleFocus(-1)
		}
		return nil
	}
	if isTextKey(msg) && !fieldChars(row, string(msg.Runes)) {
		return nil
	}
	var cmd tea.Cmd
	fl.input, cmd = fl.input.Update(msg)
	return cmd
}

// setField validates and applies an absolute value.
func (m *model) setField(row numField, v float64) tea.Cmd {
	if ok, why := row.available(m); !ok {
		return m.refuse(row.label + ": " + why)
	}
	if v < row.min || (row.max > row.min && v > row.max) {
		return m.refuse(fmt.Sprintf("%s: %s is outside %s to %s", row.label, trimFloat(v), trimFloat(row.min), trimFloat(row.max)))
	}
	return row.set(m, v)
}

// adjustField applies ←→: the next choice, or ± step (×10 with shift).
func (m *model) adjustField(row numField, dir float64, big bool) tea.Cmd {
	if row.step == 0 && len(row.choices) == 0 {
		return nil
	}
	if ok, why := row.available(m); !ok {
		return m.refuse(row.label + ": " + why)
	}
	cur, known := row.value(m)
	if len(row.choices) > 0 {
		i := nearestIndex(row.choices, cur)
		switch {
		case !known:
		case dir > 0 && i < len(row.choices)-1 && row.choices[i] <= cur:
			i++
		case dir < 0 && i > 0 && row.choices[i] >= cur:
			i--
		}
		return m.setField(row, row.choices[i])
	}
	if !known {
		return m.refuse(row.label + ": the printer hasn't reported it yet; type a value")
	}
	delta := dir * row.step
	if big {
		delta *= 10
	}
	// Clamp to the bounds, so ← at the bottom stops there.
	next := roundTo(cur+delta, row.prec)
	if next < row.min {
		next = row.min
	}
	if row.max > row.min && next > row.max {
		next = row.max
	}
	if next == cur {
		return nil
	}
	if row.nudge != nil {
		return row.nudge(m, roundTo(next-cur, row.prec))
	}
	return m.setField(row, next)
}

func roundTo(v float64, prec int) float64 {
	p := math.Pow(10, float64(prec))
	return math.Round(v*p) / p
}

func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// renderFields draws the rows: "▸ Label   value unit".
func (m *model) renderFields(fl *fieldList, rows []numField, focused bool, width int) []string {
	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, lipgloss.Width(r.label))
	}
	// Long names (an output pin's section name) don't push values off:
	// labels get what the widest value leaves.
	valueW := 1
	for _, r := range rows {
		if v, ok := r.value(m); ok {
			txt := r.format(v)
			if r.showM != nil {
				txt = r.showM(m, v)
			}
			valueW = max(valueW, lipgloss.Width(txt))
		}
	}
	labelW = min(labelW, max(6, width-2-2-valueW))
	var out []string
	for i, r := range rows {
		mark := "  "
		lst := jogHintStyle
		if focused && i == fl.cursor {
			mark = setupCursorStyle.Render("▸ ")
			lst = jogStepStyle
		}
		// A label longer than the column keeps its full name when this
		// row's own value leaves room (it then sits a little right).
		labelText := fmt.Sprintf("%-*s", labelW, clipWidth(r.label, labelW))
		if lw := lipgloss.Width(r.label); lw > labelW {
			vw := 1
			if v, ok := r.value(m); ok {
				txt := r.format(v)
				if r.showM != nil {
					txt = r.showM(m, v)
				}
				vw = lipgloss.Width(txt)
			}
			if 2+lw+2+vw <= width {
				labelText = r.label
			}
		}
		label := lst.Render(labelText)
		var value string
		switch v, ok := r.value(m); {
		case focused && i == fl.cursor && fl.editing:
			value = jogStepStyle.Render("→ " + fl.input.Value() + "▏")
		case !ok:
			value = jogHintStyle.Render("—")
		case r.showM != nil:
			value = r.showM(m, v)
		default:
			txt := r.format(v)
			// Narrow pane: drop the unit before cutting digits.
			if r.show == nil && r.unit != "" && lipgloss.Width(mark)+labelW+2+lipgloss.Width(txt) > width {
				txt = strconv.FormatFloat(v, 'f', r.prec, 64)
			}
			value = jogPosStyle.Render(txt)
			if avail, _ := r.available(m); !avail {
				value = jogHintStyle.Render(txt)
			}
		}
		line := mark + label + "  " + value
		if r.hint != "" {
			line += jogHintStyle.Render("  " + r.hint)
		}
		out = append(out, clipWidth(line, width))
	}
	return out
}

// renderFieldsWindow renders at most maxRows rows (0 = all), scrolled
// so the cursor row is visible.
func (m *model) renderFieldsWindow(fl *fieldList, rows []numField, focused bool, width, maxRows int) []string {
	lines := m.renderFields(fl, rows, focused, width)
	if maxRows <= 0 || len(lines) <= maxRows {
		return lines
	}
	start := 0
	if fl.cursor >= maxRows {
		start = fl.cursor - maxRows + 1
	}
	return lines[start : start+maxRows]
}

// ---------------------------------------------------------------------------
// Tuning: live print adjustments and velocity limits
// ---------------------------------------------------------------------------

// gcodeMove reads one gcode_move field.
func (m *model) gcodeMove(field string) (float64, bool) {
	v, ok := m.raw["gcode_move"][field].(float64)
	return v, ok
}

func (m *model) zOffset() (float64, bool) {
	o, ok := floats(m.raw["gcode_move"]["homing_origin"])
	if !ok || len(o) < 3 {
		return 0, false
	}
	return o[2], true
}

// zMove is " MOVE=1" when Z is homed, so the nozzle follows the new
// offset at once (Mainsail does the same); unhomed, Klipper would refuse
// the move.
func (m *model) zMove() string {
	if m.axisHomed(2) {
		if _, ok := m.raw["toolhead"]["homed_axes"]; ok {
			return " MOVE=1"
		}
	}
	return ""
}

// tuningFields are the Tuning pane's rows. Every one applies at once,
// during a print too — that's what they're for.
func tuningFields(m *model) []numField {
	pct := func(field string) func(m *model) (float64, bool) {
		return func(m *model) (float64, bool) {
			v, ok := m.gcodeMove(field)
			return math.Round(v * 100), ok
		}
	}
	limit := func(name string) func(m *model, v float64) tea.Cmd {
		return func(m *model, v float64) tea.Cmd {
			return m.sendEchoed("set velocity limit", fmt.Sprintf("SET_VELOCITY_LIMIT %s=%s", name, trimFloat(v)))
		}
	}
	lim := func(get func(l limits) float64, known func(l limits) bool) func(m *model) (float64, bool) {
		return func(m *model) (float64, bool) {
			if known != nil && !known(m.limits) {
				return 0, false
			}
			// 0 means "not reported yet" for these (Klipper never reports 0).
			v := get(m.limits)
			return v, v > 0 || known != nil
		}
	}
	return []numField{
		{label: "Speed", unit: "%", prec: 0, value: pct("speed_factor"), step: 5, min: 1, max: 1000,
			set: func(m *model, v float64) tea.Cmd { return m.sendEchoed("speed factor", "M220 S"+trimFloat(v)) }},
		{label: "Flow", unit: "%", prec: 0, value: pct("extrude_factor"), step: 1, min: 1, max: 1000,
			set: func(m *model, v float64) tea.Cmd { return m.sendEchoed("flow", "M221 S"+trimFloat(v)) }},
		// Z offset: arrows babystep by 0.01 (shift: 0.1). A typed value
		// is limited to ±2 mm: a slip of "-1" for "-0.1" mid-print
		// would drive the nozzle into the bed.
		{label: "Z offset", unit: "mm", prec: 3, value: (*model).zOffset, step: 0.01, min: -2, max: 2, negative: true,
			set: func(m *model, v float64) tea.Cmd {
				return m.sendEchoed("Z offset", "SET_GCODE_OFFSET Z="+trimFloat(v)+m.zMove())
			},
			nudge: func(m *model, d float64) tea.Cmd {
				return m.sendEchoed("Z offset", "SET_GCODE_OFFSET Z_ADJUST="+trimFloat(d)+m.zMove())
			}},
		{label: "Velocity", unit: "mm/s", prec: 0, value: lim(func(l limits) float64 { return l.Velocity }, nil), step: 10, min: 1, max: 100000,
			set: limit("VELOCITY")},
		{label: "Accel", unit: "mm/s²", prec: 0, value: lim(func(l limits) float64 { return l.Accel }, nil), step: 500, min: 1, max: 1000000,
			set: limit("ACCEL")},
		{label: "Corner", unit: "mm/s", prec: 1, value: lim(func(l limits) float64 { return l.SquareCornerVelocity }, nil), step: 1, min: 0, max: 1000,
			set: limit("SQUARE_CORNER_VELOCITY")},
		{label: "Cruise", prec: 2, value: lim(func(l limits) float64 { return l.MinCruiseRatio }, func(l limits) bool { return l.MinCruiseRatioKnown }),
			step: 0.05, min: 0, max: 0.99, set: limit("MINIMUM_CRUISE_RATIO")},
	}
}

// tuningContentLines is what renderTuning draws: 7 rows, a blank line
// and the key hint.
const tuningContentLines = 9

func (m model) renderTuning() string {
	w := m.layout.w(focusTuning) - panelInnerXReserve()
	rows := tuningFields(&m)
	lines := m.renderFields(&m.tuneFields, rows, m.focus == focusTuning, w)
	hint := "←→ adjust  type a value"
	switch {
	case w < 19:
		hint = "←→ adj  0–9 type"
	case w < 23:
		hint = "←→ adjust  0–9 type"
	}
	lines = append(lines, "", jogHintStyle.Render(clipWidth(hint, w)))
	return strings.Join(lines, "\n")
}

func (m *model) handleTuningKey(msg tea.KeyMsg) tea.Cmd {
	cmd, _ := m.handleFieldKey(&m.tuneFields, tuningFields(m), msg)
	return cmd
}
