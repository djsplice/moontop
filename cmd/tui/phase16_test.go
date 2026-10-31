package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// live16 seeds the state Phase 16 reads: a homed toolchanger with T0–T3,
// T1 active, speed 100 %, flow 95 %, Z offset 0.05, PA 0.04.
func live16(t *testing.T) model {
	t.Helper()
	m := readyModel(t, fakePrinter(t))
	m.gcodeCommands = map[string]bool{"T0": true, "T1": true, "T2": true, "T3": true, "G28": true}
	m.raw.merge("toolhead", map[string]any{"extruder": "extruder1", "homed_axes": "xyz"})
	m.raw.merge("gcode_move", map[string]any{
		"speed_factor": 1.0, "extrude_factor": 0.95,
		"homing_origin": []any{0.0, 0.0, 0.05, 0.0},
	})
	m.raw.merge("extruder1", map[string]any{"can_extrude": true, "pressure_advance": 0.04, "smooth_time": 0.04})
	m.limits = limits{Velocity: 300, Accel: 5000, SquareCornerVelocity: 5}
	return m
}

func key16(k string) tea.KeyMsg {
	switch k {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "shift+right":
		return tea.KeyMsg{Type: tea.KeyShiftRight}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return keyRunes(k)
}

// send16 presses keys and returns the gcode the last one sent.
func send16(t *testing.T, m model, ks ...string) (model, string) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range ks {
		m, cmd = updateCmd(t, m, key16(k))
	}
	return m, sentGcode(t, cmd)
}

func TestTuningLiveAdjustments(t *testing.T) {
	m := live16(t)
	m.setFocus(focusTuning)
	body := m.renderTuning()
	for _, want := range []string{"Speed", "100 %", "Flow", "95 %", "Z offset", "0.050 mm", "300", "Cruise", "—"} {
		if !strings.Contains(body, want) {
			t.Errorf("Tuning lacks %q:\n%s", want, body)
		}
	}

	// ←→ apply at once, also during a print — that's what they're for.
	m.job.State = "printing"
	m, g := send16(t, m, "right")
	if g != "M220 S105" || !logContains(m, ">>> M220 S105") {
		t.Fatalf("speed → sent %q", g)
	}
	if m, g = send16(t, m, "down", "left"); g != "M221 S94" {
		t.Fatalf("flow ← sent %q", g)
	}
	// Z offset babysteps with Z_ADJUST, moving the nozzle when homed.
	if m, g = send16(t, m, "down", "left"); g != "SET_GCODE_OFFSET Z_ADJUST=-0.01 MOVE=1" {
		t.Fatalf("Z ← sent %q", g)
	}
	if m, g = send16(t, m, "shift+right"); g != "SET_GCODE_OFFSET Z_ADJUST=0.1 MOVE=1" {
		t.Fatalf("Z shift+→ sent %q", g)
	}
	// A typed Z offset is absolute and limited to ±2 mm.
	if m, g = send16(t, m, "-", "0", ".", "1", "2", "enter"); g != "SET_GCODE_OFFSET Z=-0.12 MOVE=1" {
		t.Fatalf("typed Z sent %q", g)
	}
	if m, g = send16(t, m, "-", "1", "5", "enter"); g != "" || !logContains(m, "-15 is outside -2 to 2") {
		t.Fatalf("Z -15 sent %q", g)
	}
	// Velocity limits.
	if m, g = send16(t, m, "down", "4", "0", "0", "enter"); g != "SET_VELOCITY_LIMIT VELOCITY=400" {
		t.Fatalf("velocity sent %q", g)
	}
	if _, g = send16(t, m, "down", "right"); g != "SET_VELOCITY_LIMIT ACCEL=5500" {
		t.Fatalf("accel → sent %q", g)
	}
	// Unknown value: arrows refuse, typing still works.
	m.tuneFields.cursor = 6 // Cruise ratio
	if m, g = send16(t, m, "right"); g != "" || !logContains(m, "hasn't reported it yet") {
		t.Fatalf("cruise → with no value sent %q", g)
	}
	if _, g = send16(t, m, "0", ".", "5", "enter"); g != "SET_VELOCITY_LIMIT MINIMUM_CRUISE_RATIO=0.5" {
		t.Fatalf("typed cruise ratio sent %q", g)
	}
}

func TestExtruderPane(t *testing.T) {
	m := live16(t)
	m = update(t, m, keyRunes("7"))
	if m.focus != focusExtruder {
		t.Fatalf("7 → %v", m.focus)
	}
	body := m.renderExtruder()
	for _, want := range []string{"Tool", "T1", "Length", "10.0 mm", "Speed", "5.0 mm/s", "Pressure adv", "0.0400", "+ extrude  - retract"} {
		if !strings.Contains(body, want) {
			t.Errorf("Extruder lacks %q:\n%s", want, body)
		}
	}

	// + / - use the length and speed, relative, state restored.
	m, g := send16(t, m, "+")
	if !strings.Contains(g, "M83\nG1 E10 F300\nRESTORE_GCODE_STATE") || !logContains(m, "extrude 10 mm at 5 mm/s") {
		t.Fatalf("+ sent %q", g)
	}
	// Length steps through its choices; retract uses it.
	m, _ = send16(t, m, "down", "right")
	if m.extLen != 25 {
		t.Fatalf("length → %v", m.extLen)
	}
	if m, g = send16(t, m, "-"); !strings.Contains(g, "G1 E-25 F300") {
		t.Fatalf("- sent %q", g)
	}
	// Pressure advance goes to the active extruder.
	m.extFields.cursor = 3
	if m, g = send16(t, m, "right"); g != "SET_PRESSURE_ADVANCE EXTRUDER=extruder1 ADVANCE=0.045" {
		t.Fatalf("PA → sent %q", g)
	}

	// Tool change: typed (no arrows), echoed, only real tools.
	m.extFields.cursor = 0
	if m, g = send16(t, m, "right"); g != "" {
		t.Fatalf("→ on Tool sent %q", g)
	}
	if m, g = send16(t, m, "2", "enter"); g != "T2" || !logContains(m, ">>> T2") {
		t.Fatalf("tool 2 sent %q", g)
	}
	if m, g = send16(t, m, "3", "enter"); g != "T3" {
		t.Fatalf("tool 3 sent %q", g)
	}
	if m, g = send16(t, m, "9", "enter"); g != "" || !logContains(m, "outside 0 to 3") {
		t.Fatalf("tool 9 sent %q", g)
	}

	// Cold: no extrusion, and the pane says why.
	m.raw.merge("extruder1", map[string]any{"can_extrude": false})
	if m, g = send16(t, m, "+"); g != "" || !logContains(m, "below its minimum extrude temperature") {
		t.Fatalf("cold + sent %q", g)
	}
	if !strings.Contains(m.renderExtruder(), "off: nozzle too cold") {
		t.Error("pane doesn't say extrusion is off")
	}
	// Printing: no extrusion or tool change; PA still adjusts.
	m.raw.merge("extruder1", map[string]any{"can_extrude": true})
	m.job.State = "printing"
	if m, g = send16(t, m, "-"); g != "" || !logContains(m, "extruding is off not while printing") {
		t.Fatalf("printing - sent %q", g)
	}
	if m, g = send16(t, m, "1", "enter"); g != "" {
		t.Fatalf("tool change while printing sent %q", g)
	}
	m.extFields.cursor = 3
	if _, g = send16(t, m, "left"); !strings.HasPrefix(g, "SET_PRESSURE_ADVANCE") {
		t.Fatalf("PA ← while printing sent %q", g)
	}
}

func TestExtruderWithoutToolsOrPA(t *testing.T) {
	m := readyModel(t, fakePrinter(t)) // single extruder, nothing reported yet
	m.setFocus(focusExtruder)
	rows := extruderFields(&m)
	if rows[0].label != "Length" {
		t.Fatalf("first row %q on a single-tool printer", rows[0].label)
	}
	body := m.renderExtruder()
	if strings.Contains(body, "Tool") || !strings.Contains(body, "—") {
		t.Fatalf("body:\n%s", body)
	}
}

func TestBottomRowStacksWhenConsoleWouldBeTooNarrow(t *testing.T) {
	in := defaultLayoutInput(100, 40, 3)
	in.hidden = map[focusArea]bool{} // AFC and Extruder shown
	in.afcLines, in.extLines = 6, 7
	if l := computeLayout(in); l.mode != layoutStacked {
		t.Fatalf("100 cols with AFC + Extruder: mode %v", l.mode)
	}
	in.hidden[focusAFC] = true
	l := computeLayout(in)
	if l.mode != layoutWide || l.w(focusExtruder)+l.w(focusConsole) != 100-6 || l.w(focusConsole) < consoleMinW {
		t.Fatalf("100 cols, Extruder only: mode %v ext %d console %d", l.mode, l.w(focusExtruder), l.w(focusConsole))
	}
}

func TestFieldPanesFitTheirWidth(t *testing.T) {
	m := live16(t)
	for _, w := range []int{160, 120, 100} {
		m = update(t, m, tea.WindowSizeMsg{Width: w, Height: 45})
		for f, render := range map[focusArea]func() string{focusTuning: m.renderTuning, focusExtruder: m.renderExtruder} {
			if m.layout.hidden[f] {
				continue
			}
			limit := m.layout.w(f) - panelInnerXReserve()
			for _, l := range strings.Split(render(), "\n") {
				if lw := len([]rune(stripANSI(l))); lw > limit {
					t.Errorf("%d cols, %s: %q is %d wide, fits %d", w, paneName(f), stripANSI(l), lw, limit)
				}
			}
		}
	}
}

func TestExtruderCanBeHiddenInSetup(t *testing.T) {
	m := setupModel(t, fakePrinter(t))
	m = openCategory(t, m, "Panes")
	if !strings.Contains(m.View(), "7 Extruder") {
		t.Fatal("Extruder missing from setup Panes")
	}
	m = press(t, m, "down", "down", "down", "down", "space") // after the 4 top panes (no AFC here)
	if !m.cfg.Panes.PaneHidden("Extruder") || !strings.Contains(fileText(t, m), "Extruder") {
		t.Fatalf("Extruder not hidden: %v", m.cfg.Panes.Hidden)
	}
	m = press(t, m, "esc", "esc")
	if strings.Contains(m.View(), "7 Extruder") || m.layout.w(focusConsole) != 160-6 {
		t.Fatal("hidden Extruder still takes space")
	}
}
