package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/macros"
)

func TestFilamentCommandDetection(t *testing.T) {
	m := readyModel(t, fakePrinter(t))
	cases := []struct {
		have         []string
		load, unload string
	}{
		{[]string{"LOAD_FILAMENT", "UNLOAD_FILAMENT", "M701"}, "LOAD_FILAMENT", "UNLOAD_FILAMENT"},
		{[]string{"M701", "M702"}, "M701", "M702"},
		// The Qidi Q2: a vendor-named load macro, M603 to unload.
		{[]string{"QIDI_LOAD_FILAMENT", "M603", "QIDI_CLEAN_NOZZLE"}, "QIDI_LOAD_FILAMENT", "M603"},
		{[]string{"G28", "M84"}, "", ""},
	}
	for _, c := range cases {
		m.gcodeCommands = map[string]bool{}
		m.macros = nil
		for _, n := range c.have {
			m.gcodeCommands[n] = true
			m.macros = append(m.macros, macros.Macro{Name: n})
		}
		if l, u := m.filamentCommand(false), m.filamentCommand(true); l != c.load || u != c.unload {
			t.Errorf("%v: load %q unload %q, want %q %q", c.have, l, u, c.load, c.unload)
		}
	}
	// The per-printer setting wins.
	m.printerCfg.LoadMacro = "MY_LOAD"
	if got := m.filamentCommand(false); got != "MY_LOAD" {
		t.Errorf("override: %q", got)
	}
}

func TestLoadUnloadKeys(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m.gcodeCommands = map[string]bool{"LOAD_FILAMENT": true, "M702": true}
	m.macros = []macros.Macro{{Name: "LOAD_FILAMENT", Params: []macros.Param{{Name: "TEMP", Default: "220"}}}}
	m = update(t, m, keyRunes("7"))
	if !strings.Contains(m.renderExtruder(), "l load  u unload") { // names too wide here; Setup shows them
		t.Fatalf("pane:\n%s", m.renderExtruder())
	}
	// Unload has no parameters: it runs. Load has: its form opens.
	m, cmd := updateCmd(t, m, keyRunes("u"))
	if g := sentGcode(t, cmd); g != "M702" || !logContains(m, ">>> M702") {
		t.Fatalf("u sent %q", g)
	}
	m = update(t, m, keyRunes("l"))
	if m.macroForm == nil || m.macroForm.macro.Name != "LOAD_FILAMENT" {
		t.Fatal("l didn't open LOAD_FILAMENT's form")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	// Not while printing.
	m.job.State = "printing"
	if m, cmd = updateCmd(t, m, keyRunes("u")); cmd != nil || !logContains(m, "unload is off not while printing") {
		t.Fatal("unload ran during a print")
	}
	m.job.State = "standby"

	// Setup → Extruder overrides it, per printer; unknown commands refused.
	m = openCategory(t, m, "Extruder")
	if v := m.View(); !strings.Contains(v, "detect: LOAD_FILAMENT") || !strings.Contains(v, "detect: M702") {
		t.Fatalf("setup:\n%s", v)
	}
	m = press(t, m, "down", "enter")
	m.setup.input.SetValue("nope")
	m = press(t, m, "enter")
	if !strings.Contains(m.setup.status, "isn't a command") {
		t.Fatalf("bad override accepted: %q", m.setup.status)
	}
	m.setup.input.SetValue("load_filament")
	m = press(t, m, "enter")
	if m.printerCfg.UnloadMacro != "LOAD_FILAMENT" || lastPrinterSave(t, srv).UnloadMacro != "LOAD_FILAMENT" {
		t.Fatalf("override not saved: %+v", m.printerCfg)
	}
}

func TestNoLoadMacro(t *testing.T) {
	m := readyModel(t, fakePrinter(t))
	m.gcodeCommands = map[string]bool{"G28": true}
	m = update(t, m, keyRunes("7"))
	if !strings.Contains(m.renderExtruder(), "no load macro") {
		t.Fatalf("pane:\n%s", m.renderExtruder())
	}
	if m, cmd := updateCmd(t, m, keyRunes("l")); cmd != nil || !logContains(m, "F2 → Extruder") {
		t.Fatal("l without a load macro didn't say how to set one")
	}
}

func TestLeveling(t *testing.T) {
	m := readyModel(t, fakePrinter(t))
	m = update(t, m, keyRunes("2"))
	if _, cmd := updateCmd(t, m, keyRunes("l")); cmd != nil {
		t.Fatal("l levelled a printer without z_tilt or QGL")
	}
	before := m.layout.topRowH

	m.raw.merge("quad_gantry_level", map[string]any{"applied": false})
	m = update(t, m, tea.WindowSizeMsg{Width: 160, Height: 50})
	if body := m.renderJog(); !strings.Contains(body, "l QGL") || !strings.Contains(body, "not applied") {
		t.Fatalf("pane:\n%s", body)
	}
	if m.layout.topRowH != before+1 {
		t.Errorf("top row %d, want %d (one line for QGL)", m.layout.topRowH, before+1)
	}
	m, cmd := updateCmd(t, m, keyRunes("l"))
	if g := sentGcode(t, cmd); g != "QUAD_GANTRY_LEVEL" || !logContains(m, ">>> QUAD_GANTRY_LEVEL") {
		t.Fatalf("l sent %q", g)
	}
	m.raw.merge("quad_gantry_level", map[string]any{"applied": true})
	if !strings.Contains(m.renderJog(), "applied ✓") {
		t.Fatal("applied state not shown")
	}

	// z_tilt, and never while printing.
	delete(m.raw, "quad_gantry_level")
	m.raw.merge("z_tilt", map[string]any{"applied": false})
	m.job.State = "printing"
	if m, cmd = updateCmd(t, m, keyRunes("l")); cmd != nil || !logContains(m, "not while printing") {
		t.Fatal("Z tilt ran during a print")
	}
	m.job.State = "standby"
	if _, cmd = updateCmd(t, m, keyRunes("l")); sentGcode(t, cmd) != "Z_TILT_ADJUST" {
		t.Fatal("l on z_tilt didn't send Z_TILT_ADJUST")
	}
}
