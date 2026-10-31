package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Phase 16: the Extruder pane (pane 7, bottom row beside AFC and console)
//
// Rows: the tool (when the printer has T<n> commands), extrude length and
// speed (this session's choice), pressure advance and smooth time for the
// active extruder. + extrudes, - retracts. Tool changes and extrusion are
// refused while printing (paused is fine) and, for extrusion, while the
// active extruder is below its minimum extrude temperature (Klipper's
// can_extrude) — Klipper refuses those too, but less legibly.
// ---------------------------------------------------------------------------

var toolCommand = regexp.MustCompile(`^T(\d+)$`)

// tools lists the printer's T<n> commands (macros on a toolchanger such
// as the U1), sorted by number.
func (m *model) tools() []int {
	var out []int
	for c := range m.gcodeCommands {
		if g := toolCommand.FindStringSubmatch(c); g != nil {
			n, _ := strconv.Atoi(g[1])
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

// activeExtruder is toolhead.extruder ("extruder", "extruder1", …).
func (m *model) activeExtruder() string {
	if e, ok := m.raw["toolhead"]["extruder"].(string); ok && e != "" {
		return e
	}
	return "extruder"
}

// activeTool maps the active extruder to its tool number: extruder → 0,
// extruderN → N, the convention multi-extruder and toolchanger configs
// (the U1 among them) follow.
func (m *model) activeTool() (int, bool) {
	e, ok := m.raw["toolhead"]["extruder"].(string)
	if !ok || !extruderHeater.MatchString(e) {
		return 0, false
	}
	if e == "extruder" {
		return 0, true
	}
	n, err := strconv.Atoi(strings.TrimPrefix(e, "extruder"))
	return n, err == nil
}

func (m *model) extruderField(field string) (float64, bool) {
	v, ok := m.raw[m.activeExtruder()][field].(float64)
	return v, ok
}

var (
	extrudeLengths = []float64{1, 5, 10, 25, 50, 100}
	extrudeSpeeds  = []float64{1, 2, 5, 10, 15, 20, 30}
)

func extruderFields(m *model) []numField {
	var rows []numField
	if tools := m.tools(); len(tools) > 1 {
		rows = append(rows, numField{
			label: "Tool", prec: 0, min: 0, max: float64(tools[len(tools)-1]),
			value: func(m *model) (float64, bool) {
				n, ok := m.activeTool()
				return float64(n), ok
			},
			show: func(v float64) string { return fmt.Sprintf("T%d", int(v)) },
			// No arrows: a tool change moves the toolhead, so it is
			// typed (digit, enter) rather than stepped into.
			when: canMove,
			set: func(m *model, v float64) tea.Cmd {
				t := fmt.Sprintf("T%d", int(v))
				if !m.gcodeCommands[t] || v != float64(int(v)) {
					return m.refuse(fmt.Sprintf("this printer has no %s", trimFloatTool(v)))
				}
				return m.sendEchoed("tool change", t)
			},
		})
	}
	ext := m.activeExtruder()
	rows = append(rows,
		numField{label: "Length", unit: "mm", prec: 1, choices: extrudeLengths, min: 0.1, max: 500,
			value: func(m *model) (float64, bool) { return m.extLen, true },
			set:   func(m *model, v float64) tea.Cmd { m.extLen = v; return nil }},
		numField{label: "Speed", unit: "mm/s", prec: 1, choices: extrudeSpeeds, min: 0.1, max: 100,
			value: func(m *model) (float64, bool) { return m.extSpeed, true },
			set:   func(m *model, v float64) tea.Cmd { m.extSpeed = v; return nil }},
		numField{label: "Pressure adv", prec: 4, step: 0.005, min: 0, max: 2,
			value: func(m *model) (float64, bool) { return m.extruderField("pressure_advance") },
			set: func(m *model, v float64) tea.Cmd {
				return m.sendEchoed("pressure advance", fmt.Sprintf("SET_PRESSURE_ADVANCE EXTRUDER=%s ADVANCE=%s", ext, trimFloat(v)))
			}},
		numField{label: "Smooth time", unit: "s", prec: 3, step: 0.005, min: 0, max: 0.2,
			value: func(m *model) (float64, bool) { return m.extruderField("smooth_time") },
			set: func(m *model, v float64) tea.Cmd {
				return m.sendEchoed("smooth time", fmt.Sprintf("SET_PRESSURE_ADVANCE EXTRUDER=%s SMOOTH_TIME=%s", ext, trimFloat(v)))
			}},
	)
	return rows
}

func trimFloatTool(v float64) string {
	if v == float64(int(v)) {
		return fmt.Sprintf("T%d", int(v))
	}
	return "tool " + trimFloat(v)
}

// extrudeRefusal says why extruding can't happen now ("" = it can).
func (m *model) extrudeRefusal() string {
	if ok, why := canMove(m); !ok {
		return "extruding is off " + why
	}
	if c, ok := m.raw[m.activeExtruder()]["can_extrude"].(bool); ok && !c {
		return fmt.Sprintf("%s is below its minimum extrude temperature; heat it first", m.label(m.activeExtruder()))
	}
	return ""
}

func canExtrude(m *model) (bool, string) {
	why := m.extrudeRefusal()
	return why == "", strings.TrimPrefix(why, "extruding is off ")
}

func (m *model) handleExtruderKey(msg tea.KeyMsg) tea.Cmd {
	rows := extruderFields(m)
	if cmd, handled := m.handleFieldKey(&m.extFields, rows, msg); handled {
		return cmd
	}
	sign := 0.0
	switch {
	case key.Matches(msg, *keys.Extrude):
		sign = 1
	case key.Matches(msg, *keys.Retract):
		sign = -1
	default:
		return nil
	}
	if why := m.extrudeRefusal(); why != "" {
		return m.refuse(why)
	}
	return m.sendExtrude(sign * m.extLen)
}

// sendExtrude moves the active extruder by e mm (negative retracts), in
// relative extrusion inside SAVE/RESTORE_GCODE_STATE.
func (m *model) sendExtrude(e float64) tea.Cmd {
	move := fmt.Sprintf("G1 E%s F%s", trimFloat(e), trimFloat(m.extSpeed*60))
	if m.connected() {
		verb := "extrude"
		if e < 0 {
			verb = "retract"
		}
		m.appendLog(fmt.Sprintf(">>> %s (%s %s mm at %s mm/s)", move, verb, trimFloat(abs(e)), trimFloat(m.extSpeed)))
	}
	script := strings.Join([]string{
		"SAVE_GCODE_STATE NAME=_ui_extrude",
		"M83",
		move,
		"RESTORE_GCODE_STATE NAME=_ui_extrude",
	}, "\n")
	return m.requireConnected("extrude", m.sendGcode(script))
}

// extruderContentLines: up to 5 rows, a blank line, 2 hint lines.
func extruderContentLines(m *model) int { return len(extruderFields(m)) + 3 }

func (m model) renderExtruder() string {
	w := m.layout.w(focusExtruder) - panelInnerXReserve()
	rows := extruderFields(&m)
	lines := m.renderFields(&m.extFields, rows, m.focus == focusExtruder, w)
	act := "+ extrude  - retract"
	if ok, why := canExtrude(&m); !ok {
		act = "+ - off: " + why
		if strings.Contains(why, "minimum extrude temperature") {
			act = "+ - off: nozzle too cold"
		}
	}
	lines = append(lines, "",
		jogHintStyle.Render(clipWidth(act, w)),
		jogHintStyle.Render(clipWidth("←→ adjust  type a value", w)))
	return strings.Join(lines, "\n")
}
