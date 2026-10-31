package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
)

func TestRegistryEntriesAreComplete(t *testing.T) {
	for _, e := range registry {
		h := e.b.Help()
		if h.Key == "" || h.Desc == "" {
			t.Errorf("entry %+v missing help", h)
		}
	}
}

// A pane key must never shadow a global, job or Klipper key: those are
// checked first, so the pane binding would be dead.
func TestPaneKeysDoNotCollideWithGlobalKeys(t *testing.T) {
	global := map[string]string{}
	for _, e := range registry {
		switch e.scope {
		case scopeGlobal, scopeJob, scopeKlipper:
			for _, k := range e.b.Keys() {
				global[k] = e.b.Help().Desc
			}
		}
	}
	for _, e := range registry {
		switch e.scope {
		case scopeGlobal, scopeJob, scopeKlipper, scopeHelp:
			continue
		}
		for _, k := range e.b.Keys() {
			if g, ok := global[k]; ok {
				t.Errorf("%s key %q (%s) collides with global %q", e.scope.title(), k, e.b.Help().Desc, g)
			}
		}
	}
}

func TestTemperaturesCursorWorks(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, keyRunes("4"))
	if m.focus != focusTable || !m.table.Focused() {
		t.Fatalf("focus=%v table focused=%v", m.focus, m.table.Focused())
	}
	before := m.table.Cursor()
	m = update(t, m, keyRunes("j"))
	if m.table.Cursor() != before+1 {
		t.Fatalf("cursor %d → %d, want +1", before, m.table.Cursor())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.table.Focused() {
		t.Fatal("table still focused after leaving the pane")
	}
}

func TestPaneJumpsAndShiftTab(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	// Jump to every pane from Files. (From Tuning or the console,
	// digits are text, so jump from a non-text pane each time.)
	for i, want := range paneOrder {
		m.setFocus(focusFiles)
		m = update(t, m, keyRunes(string(rune('1'+i))))
		if m.focus != want {
			t.Fatalf("key %d → %v, want %v", i+1, m.focus, want)
		}
	}
	// In the console, digits type.
	m = update(t, m, keyRunes("1"))
	if m.focus != focusConsole || m.input.Value() != "1" {
		t.Fatalf("focus=%v input=%q", m.focus, m.input.Value())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != focusTable {
		t.Fatalf("shift+tab from console → %v", m.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != focusConsole {
		t.Fatalf("shift+tab wraps to %v, want console", m.focus)
	}
}

func TestPaneTitlesAreNumbered(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	v := m.View()
	for i, f := range paneOrder {
		want := string(rune('1'+i)) + " " + paneName(f)
		if !strings.Contains(v, want) {
			t.Errorf("view missing title %q", want)
		}
	}
}

func TestFilesFilterTextDoesNotQuitOrJump(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, filesLoadedMsg{files: []moonraker.FileInfo{{Path: "cube.gcode"}, {Path: "q2.gcode"}}})
	m = update(t, m, keyRunes("/"))
	m, cmd := updateCmd(t, m, keyRunes("q"))
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q while filtering quit the app")
		}
	}
	m = update(t, m, keyRunes("2"))
	if m.focus != focusFiles {
		t.Fatalf("digit while filtering jumped to %v", m.focus)
	}
	if !strings.Contains(m.files.FilterInput.Value(), "q2") {
		t.Fatalf("filter text = %q", m.files.FilterInput.Value())
	}
}

func TestTuningDigitsTypeButLettersAreCommands(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, keyRunes("3"))
	m = update(t, m, keyRunes("1"))
	m = update(t, m, keyRunes("2"))
	if m.focus != focusTuning || !strings.HasSuffix(m.tuningInputs[m.tuningFocus].Value(), "12") {
		t.Fatalf("focus=%v field=%q", m.focus, m.tuningInputs[m.tuningFocus].Value())
	}
	m = update(t, m, keyRunes("j"))
	if m.tuningFocus != 1 {
		t.Fatalf("j moved to field %d", m.tuningFocus)
	}
	m.job.State = "printing"
	_, cmd := updateCmd(t, m, keyRunes("p"))
	if cmd == nil {
		t.Fatal("p in Tuning did not pause")
	}
	cmd()
	srv.WaitForCall("printer.print.pause", 1, wait)
}

func TestHelpOverlayListsEveryKey(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, keyRunes("?"))
	if !m.showHelp {
		t.Fatal("? did not open help")
	}
	all := strings.Join(m.helpLines(), "\n")
	for _, e := range registry {
		if !strings.Contains(all, e.b.Help().Desc) {
			t.Errorf("help missing %q", e.b.Help().Desc)
		}
	}
	// Job keys are listed but marked as waiting while idle.
	if !strings.Contains(all, "pause print — while printing") {
		t.Error("inactive job key not annotated")
	}
	if !strings.Contains(m.View(), "Keys") {
		t.Error("overlay not rendered")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.showHelp {
		t.Fatal("esc did not close help")
	}
	// In the console, ? is text; F1 still opens help.
	m = update(t, m, keyRunes("5"))
	m = update(t, m, keyRunes("?"))
	if m.showHelp || m.input.Value() != "?" {
		t.Fatalf("? in console: help=%v input=%q", m.showHelp, m.input.Value())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyF1})
	if !m.showHelp {
		t.Fatal("F1 in console did not open help")
	}
	// E-stop still arms from the overlay.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.showHelp || m.confirmation == nil || !m.confirmation.danger {
		t.Fatal("ctrl+x from help did not arm e-stop")
	}
}

func TestFooterFollowsFocusAndJobState(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, keyRunes("2"))
	pane, job := footerHints(&m)
	if !strings.Contains(strings.Join(pane, "|"), "H home") || len(job) != 0 {
		t.Fatalf("toolhead footer pane=%q job=%q", pane, job)
	}
	m.job = printJob{State: "printing", Filename: "a-rather-long-file-name.gcode", Progress: 0.4, Duration: 600}
	_, job = footerHints(&m)
	if strings.Join(job, "|") != "p pause|c cancel" {
		t.Fatalf("printing job hints = %q", job)
	}
	// On a narrow terminal the footer sheds pane hints but keeps help,
	// e-stop and the job keys.
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	f := m.fitFooter("")
	if lipgloss.Width(f) > 120 {
		t.Fatalf("footer %d wide on a 120-column terminal", lipgloss.Width(f))
	}
	for _, want := range []string{"? help", "ctrl+x e-stop", "p pause", "c cancel"} {
		if !strings.Contains(f, want) {
			t.Errorf("narrow footer lost %q: %s", want, f)
		}
	}
}

func TestToolheadButtons(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, keyRunes("2"))
	for k, want := range map[string]string{"x": "G28 X", "y": "G28 Y", "z": "G28 Z", "m": "M84"} {
		_, cmd := updateCmd(t, m, keyRunes(k))
		if cmd == nil {
			t.Fatalf("%s produced no Cmd", k)
		}
		if sent, ok := cmd().(gcodeSentMsg); !ok || sent.cmd != want {
			t.Fatalf("%s sent %+v, want %q", k, sent, want)
		}
	}
	m.job.State = "printing"
	m, cmd := updateCmd(t, m, keyRunes("m"))
	if cmd != nil || !logContains(m, "motors off only works when idle") {
		t.Fatal("motors off allowed during a print")
	}
}

func TestDashboardConsoleFilters(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	for _, l := range []string{"B:26.3 /0.0 T0:168.1 /140.0", "ok B:26.3 /0.0 T0:150.0 /140.0", "// Run Current: 0.80A Hold Current: 0.80A", "echo: Nozzle cleared"} {
		m.appendLog(l)
	}
	if m.hiddenCount != 3 {
		t.Fatalf("hidden = %d, want 3", m.hiddenCount)
	}
	if !strings.Contains(m.View(), "3 hidden (ctrl+o shows)") {
		t.Error("console title lacks hidden count")
	}
	if strings.Contains(m.viewport.View(), "T0:168.1") {
		t.Error("filtered line rendered")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if !m.showFiltered || m.hiddenCount != 0 || !strings.Contains(m.viewport.View(), "Run Current") {
		t.Fatalf("ctrl+o: showFiltered=%v hidden=%d", m.showFiltered, m.hiddenCount)
	}
}

func TestTemperaturesDropStateColumnWhenNarrow(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv) // 160 columns: room for State
	if !m.tableShowState || len(m.table.Columns()) != 4 {
		t.Fatalf("wide: showState=%v cols=%d", m.tableShowState, len(m.table.Columns()))
	}
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.tableShowState || len(m.table.Columns()) != 3 {
		t.Fatalf("narrow: showState=%v cols=%d", m.tableShowState, len(m.table.Columns()))
	}
	for _, r := range m.table.Rows() {
		if len(r) != 3 {
			t.Fatalf("row has %d cells for 3 columns", len(r))
		}
	}
	for _, l := range strings.Split(m.table.View(), "\n")[:1] {
		if !strings.Contains(l, "Target") {
			t.Fatalf("header wrapped: %q", l)
		}
	}
}

func TestMultiExtruderHeatersAreListed(t *testing.T) {
	for name, want := range map[string]bool{
		"extruder": true, "extruder1": true, "extruder3": true,
		"extruder_stepper belt": false, "extruder1_stepper": false,
		"heater_bed": true, "temperature_sensor chamber": true,
	} {
		if got := isSensorObject(name); got != want {
			t.Errorf("isSensorObject(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestToolheadHintsFitNarrowPane(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	for _, w := range []int{160, 120, 100} {
		m = update(t, m, tea.WindowSizeMsg{Width: w, Height: 40})
		if m.layout.mode != layoutWide {
			continue
		}
		limit := m.layout.jogBoxW - panelInnerXReserve()
		for _, l := range strings.Split(m.renderJog(), "\n") {
			if lipgloss.Width(l) > limit {
				t.Errorf("%d cols: toolhead line %q is %d wide, pane fits %d", w, l, lipgloss.Width(l), limit)
			}
		}
	}
}
