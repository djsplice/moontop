package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func meshModel(t *testing.T) model {
	t.Helper()
	m := readyModel(t, fakePrinter(t))
	m.raw.merge("toolhead", map[string]any{"homed_axes": "xyz"})
	m.raw.merge("bed_mesh", map[string]any{
		"profile_name": "default",
		"mesh_min":     []any{10.0, 10.0}, "mesh_max": []any{220.0, 220.0},
		"probed_matrix": []any{ // front row first, as Klipper stores it
			[]any{-0.05, 0.0, 0.025},
			[]any{0.01, 0.02, 0.03},
			[]any{0.1, 0.075, 0.06},
		},
		"profiles": map[string]any{"default": map[string]any{}, "pei_hot": map[string]any{}},
	})
	return m
}

func TestBedMeshGrid(t *testing.T) {
	m := meshModel(t)
	if m = update(t, m, keyRunes("B")); !m.mesh.open {
		t.Fatal("B didn't open the mesh")
	}
	v := stripANSI(m.View())
	for _, s := range []string{"Bed mesh", "Profile default", "range -0.050 to +0.100 (0.150 mm)", "X 10–220  Y 10–220",
		"             0        1        2",
		"     ┌────────┬────────┬────────┐", "     ├────────┼────────┼────────┤", "     └────────┴────────┴────────┘", "back of the bed on top", "default  loaded", "pei_hot"} {
		if !strings.Contains(v, s) {
			t.Errorf("mesh view lacks %q:\n%s", s, v)
		}
	}
	// Marlin-style: the back row (2) on top, the front row (0) last.
	back, front := strings.Index(v, "   2 │ +0.100 │ +0.075 │ +0.060 │"), strings.Index(v, "   0 │ -0.050 │ +0.000 │ +0.025 │")
	if back < 0 || front < 0 || back > front {
		t.Fatalf("rows out of order (back %d, front %d):\n%s", back, front, v)
	}

	// Profiles: enter loads, x removes after a confirmation.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if g := sentGcode(t, cmd); g != "BED_MESH_PROFILE LOAD=pei_hot" {
		t.Fatalf("load sent %q", g)
	}
	m = update(t, m, keyRunes("x"))
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "Remove mesh profile pei_hot") {
		t.Fatal("no remove confirmation")
	}
	m = update(t, m, keyRunes("n"))
	// s: a name on the prompt line; bad names refused.
	m = update(t, m, keyRunes("s"))
	if m.lineEdit == nil || m.lineEdit.input.Value() != "default" || !strings.Contains(m.View(), "Save the current mesh") {
		t.Fatal("s didn't prompt for a name")
	}
	m.lineEdit.input.SetValue("bad name")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.lineEdit == nil || m.lineEdit.err == "" {
		t.Fatal("bad profile name accepted")
	}
	m.lineEdit.input.SetValue("textured")
	m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if g := sentGcode(t, cmd); g != "BED_MESH_PROFILE SAVE=textured" || !logContains(m, "SAVE_CONFIG") {
		t.Fatalf("save sent %q", g)
	}
	// c calibrates after a confirmation; refused unhomed and mid-print.
	m = update(t, m, keyRunes("c"))
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "BED_MESH_CALIBRATE") {
		t.Fatal("no calibrate confirmation")
	}
	m = update(t, m, keyRunes("n"))
	m.raw.merge("toolhead", map[string]any{"homed_axes": "xy"})
	if m = update(t, m, keyRunes("c")); m.confirmation != nil || !logContains(m, "home all axes first") {
		t.Fatal("calibrated unhomed")
	}
	m.job.State = "printing"
	if m = update(t, m, keyRunes("w")); m.confirmation != nil {
		t.Fatal("SAVE_CONFIG offered mid-print")
	}
	m.job.State = "standby"
	if _, cmd = updateCmd(t, m, keyRunes("C")); sentGcode(t, cmd) != "BED_MESH_CLEAR" {
		t.Fatal("C didn't clear")
	}
}

func TestScrewsTilt(t *testing.T) {
	m := meshModel(t)
	m.mesh.open = true
	if m = update(t, m, keyRunes("t")); !logContains(m, "no [screws_tilt_adjust]") {
		t.Fatal("t without screws_tilt_adjust")
	}
	m.raw.merge("screws_tilt_adjust", map[string]any{"error": false, "results": map[string]any{
		"screw1": map[string]any{"name": "front left", "z": 2.5, "sign": "CW", "adjust": "00:00", "is_base": true},
		"screw2": map[string]any{"name": "front right", "z": 2.4875, "sign": "CCW", "adjust": "00:15", "is_base": false},
	}})
	v := stripANSI(m.View())
	if !strings.Contains(v, "front left") || !strings.Contains(v, "base") || !strings.Contains(v, "00:15 counter-clockwise") || !strings.Contains(v, "t screws tilt") {
		t.Fatalf("screws:\n%s", v)
	}
	if _, cmd := updateCmd(t, m, keyRunes("t")); sentGcode(t, cmd) != "SCREWS_TILT_CALCULATE" {
		t.Fatal("t didn't run SCREWS_TILT_CALCULATE")
	}
}

func TestManualProbe(t *testing.T) {
	m := readyModel(t, fakePrinter(t))
	m.raw.merge("manual_probe", map[string]any{"is_active": true, "z_position": 0.25, "z_position_lower": 0.2, "z_position_upper": 0.3})
	v := stripANSI(m.View())
	if !strings.Contains(v, "paper test") || !strings.Contains(v, "0.250") || !strings.Contains(v, "between 0.200 and 0.300") || !strings.Contains(v, "[0.05]") {
		t.Fatalf("probe dialog:\n%s", v)
	}
	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if g := sentGcode(t, cmd); g != "TESTZ Z=-0.05" {
		t.Fatalf("down sent %q", g)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if _, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyUp}); sentGcode(t, cmd) != "TESTZ Z=0.025" {
		t.Fatal("smaller step not used")
	}
	if _, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter}); sentGcode(t, cmd) != "ACCEPT" {
		t.Fatal("enter didn't ACCEPT")
	}
	if _, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc}); sentGcode(t, cmd) != "ABORT" {
		t.Fatal("esc didn't ABORT")
	}
	// It ends when Klipper says so.
	m.raw.merge("manual_probe", map[string]any{"is_active": false})
	if strings.Contains(m.View(), "paper test") {
		t.Fatal("dialog stayed after the probe ended")
	}
}

func TestMeshGridColoursAndFit(t *testing.T) {
	// Colours: zero green, the most negative dark blue, the most positive red.
	if bg := meshCellStyle(0).GetBackground(); bg != lipgloss.Color("46") {
		t.Errorf("zero: %v", bg)
	}
	if bg := meshCellStyle(-1).GetBackground(); bg != lipgloss.Color("27") {
		t.Errorf("lowest: %v", bg)
	}
	if bg := meshCellStyle(1).GetBackground(); bg != lipgloss.Color("196") {
		t.Errorf("highest: %v", bg)
	}
	// A 9×9 mesh in a small space: no lines between rows, two decimals.
	var mat [][]float64
	for r := 0; r < 9; r++ {
		row := make([]float64, 9)
		for c := range row {
			row[c] = float64(r*9+c) / 100
		}
		mat = append(mat, row)
	}
	lines := renderMeshGrid(mat, 80, 15) // full cells need 87, compact 78
	if len(lines) != 9+3 {
		t.Fatalf("%d lines, want 12 (header, top, 9 rows, bottom)", len(lines))
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > 80 {
			t.Fatalf("line %d wide: %q", w, stripANSI(l))
		}
		if strings.Contains(stripANSI(l), "├") {
			t.Fatal("row separators kept in a short space")
		}
	}
	if !strings.Contains(stripANSI(lines[2]), "│ +0.72 │") {
		t.Fatalf("back row: %q", stripANSI(lines[2]))
	}
}
