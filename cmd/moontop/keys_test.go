package main

import (
	"fmt"
	"github.com/charmbracelet/bubbles/key"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moontop/internal/moonraker"
)

func TestRegistryEntriesAreComplete(t *testing.T) {
	for _, e := range registry {
		h := e.b.Help()
		if h.Key == "" || h.Desc == "" {
			t.Errorf("entry %+v missing help", h)
		}
	}
}

// A pane key must never shadow a global key (those are checked first, so
// the pane binding would be dead). It may reuse a job or Klipper key
// only deliberately: the pane then wins while focused (paneClaims).
var allowedPaneShadows = map[string]bool{
	"Toolhead c": true, // crouch = Z down; cancel works from any other pane
}

func TestPaneKeysDoNotCollideWithGlobalKeys(t *testing.T) {
	global := map[string]string{}
	globalBinding := map[*key.Binding]bool{}
	for _, e := range registry {
		switch e.scope {
		case scopeGlobal, scopeJob, scopeKlipper:
			globalBinding[e.b] = true
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
		if globalBinding[e.b] {
			continue // a global key repeated as a pane's footer hint (F3)
		}
		for _, k := range e.b.Keys() {
			if g, ok := global[k]; ok && !allowedPaneShadows[e.scope.title()+" "+k] {
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
	// A row is selected from the start, not only after the first key.
	if m.table.Cursor() != 0 {
		t.Fatalf("initial cursor %d, want 0", m.table.Cursor())
	}
	m = update(t, m, keyRunes("j"))
	if m.table.Cursor() != 1 {
		t.Fatalf("cursor after j = %d, want 1", m.table.Cursor())
	}
	// Resizing (which rebuilds the columns) keeps the selection.
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.table.Cursor() != 1 {
		t.Fatalf("cursor after resize = %d, want 1", m.table.Cursor())
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
		if want == focusAFC || want == focusOutputs || want == focusMacros {
			continue // no AFC or outputs on this fake printer
		}
		m.setFocus(focusFiles)
		m = update(t, m, keyRunes(string(rune('1'+i))))
		if m.focus != want {
			t.Fatalf("key %d → %v, want %v", i+1, m.focus, want)
		}
	}
	// In the console, digits type.
	m.setFocus(focusConsole)
	m = update(t, m, keyRunes("1"))
	if m.focus != focusConsole || m.input.Value() != "1" {
		t.Fatalf("focus=%v input=%q", m.focus, m.input.Value())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != focusExtruder {
		t.Fatalf("shift+tab from console → %v", m.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != focusTable {
		t.Fatalf("shift+tab from Extruder → %v", m.focus)
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
		if f == focusAFC || f == focusOutputs || f == focusMacros {
			if strings.Contains(v, "6 AFC") || strings.Contains(v, "8 Outputs") || strings.Contains(v, "9 Macros") {
				t.Error("AFC or Outputs drawn on a printer without them")
			}
			continue
		}
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
	if m.focus != focusTuning || !m.tuneFields.editing || m.tuneFields.input.Value() != "12" {
		t.Fatalf("focus=%v editing=%v field=%q", m.focus, m.tuneFields.editing, m.tuneFields.input.Value())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc}, keyRunes("j"))
	if m.tuneFields.cursor != 1 {
		t.Fatalf("j moved to row %d", m.tuneFields.cursor)
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
	// c is Z down in Toolhead, so cancel isn't advertised there.
	if strings.Join(job, "|") != "p pause" {
		t.Fatalf("toolhead printing job hints = %q", job)
	}
	m.setFocus(focusFiles) // digits type a position in Toolhead
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
	// Klipper prints nothing for a successful M84 or G28, so the
	// console shows what was sent.
	m, _ = updateCmd(t, m, keyRunes("m"))
	if !logContains(m, ">>> M84") {
		t.Fatal("M84 not echoed to the console")
	}
	m.job.State = "printing"
	m, cmd := updateCmd(t, m, keyRunes("m"))
	if cmd != nil || !logContains(m, "motors off only works when idle") {
		t.Fatal("motors off allowed during a print")
	}
}

// sentGcode runs a key's Cmd and returns the gcode it sent ("" if none).
func sentGcode(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		return ""
	}
	sent, ok := cmd().(gcodeSentMsg)
	if !ok {
		t.Fatalf("Cmd returned %T, not a gcode send", cmd())
	}
	return sent.cmd
}

func TestToolheadWASD(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, keyRunes("2"))
	step := m.steps[m.stepIndex]
	for k, want := range map[string]string{
		"w": fmt.Sprintf("G1 Y%g ", step), "s": fmt.Sprintf("G1 Y%g ", -step),
		"a": fmt.Sprintf("G1 X%g ", -step), "d": fmt.Sprintf("G1 X%g ", step),
		" ": fmt.Sprintf("G1 Z%g ", step), "c": fmt.Sprintf("G1 Z%g ", -step),
	} {
		msg := keyRunes(k)
		if k == " " {
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
		}
		_, cmd := updateCmd(t, m, msg)
		got := sentGcode(t, cmd)
		if !strings.Contains(got, want) || !strings.Contains(got, "G91") || !strings.Contains(got, "RESTORE_GCODE_STATE") {
			t.Errorf("%q sent %q, want %q", k, got, want)
		}
	}

	// ← → change the step; they no longer jog.
	i := m.stepIndex
	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if cmd != nil || m.stepIndex != i+1 {
		t.Fatalf("→: step index %d (was %d), cmd %v", m.stepIndex, i, cmd != nil)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.stepIndex != i {
		t.Fatalf("←: step index %d, want %d", m.stepIndex, i)
	}

	// While printing: c jogs (refused), never arms cancel.
	m.job.State = "printing"
	m, cmd = updateCmd(t, m, keyRunes("c"))
	if cmd != nil || m.confirmation != nil || !logContains(m, "not while printing") {
		t.Fatalf("c while printing: cmd=%v confirmation=%v", cmd != nil, m.confirmation)
	}
	// Digits type a position here rather than jump panes.
	m = update(t, m, keyRunes("1"))
	if m.focus != focusJog || !m.thEditing {
		t.Fatal("1 in Toolhead did not start typing")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc}, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus == focusJog {
		t.Fatal("shift+tab did not leave Toolhead")
	}
	// From another pane, c is still cancel.
	m = update(t, m, keyRunes("c"))
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "Cancel") {
		t.Fatal("c outside Toolhead did not arm cancel")
	}
	// Paused: moves are allowed again.
	m.confirmation = nil
	m.job.State = "paused"
	m = update(t, m, keyRunes("2"))
	if _, cmd = updateCmd(t, m, keyRunes("c")); cmd == nil {
		t.Fatal("jog refused while paused")
	}
}

func TestToolheadAbsoluteMove(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m.raw.merge("toolhead", map[string]any{
		"homed_axes":   "xy",
		"axis_minimum": []any{0.0, -5.0, 0.0, 0.0},
		"axis_maximum": []any{235.0, 235.0, 250.0, 0.0},
	})
	m.raw.merge("gcode_move", map[string]any{
		"gcode_position": []any{10.0, 20.0, 30.0, 0.0},
		"homing_origin":  []any{0.0, 0.0, 0.0, 0.0},
	})
	m = update(t, m, keyRunes("2"))
	if m.thCursor != thX {
		t.Fatalf("cursor starts on %d", m.thCursor)
	}
	if !strings.Contains(m.renderJog(), "10.00") || !strings.Contains(m.renderJog(), "30.00") {
		t.Fatalf("gcode position not shown:\n%s", m.renderJog())
	}

	// Typing starts an edit (digits don't jump panes here); enter moves.
	m = update(t, m, keyRunes("1"), keyRunes("2"), keyRunes("0"))
	if m.focus != focusJog || !m.thEditing || m.thInput.Value() != "120" {
		t.Fatalf("focus=%v editing=%v value=%q", m.focus, m.thEditing, m.thInput.Value())
	}
	if !strings.Contains(m.renderJog(), "→ 120") {
		t.Fatal("edit not shown in place")
	}
	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	got := sentGcode(t, cmd)
	if !strings.Contains(got, "G90\nG1 X120 F") || m.thEditing {
		t.Fatalf("absolute move sent %q (editing=%v)", got, m.thEditing)
	}

	// Letters are refused while typing; esc cancels.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}, keyRunes("5"), keyRunes("w"))
	if m.thCursor != thY || m.thInput.Value() != "5" {
		t.Fatalf("cursor %d value %q", m.thCursor, m.thInput.Value())
	}
	m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || m.thEditing {
		t.Fatal("esc did not cancel")
	}

	// Out of range is refused, and so is an unhomed axis.
	m = update(t, m, keyRunes("3"), keyRunes("0"), keyRunes("0"))
	if m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || !logContains(m, "Y 300 is outside -5 to 235") {
		t.Fatal("out-of-range move not refused")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}, keyRunes("5"))
	if m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || !logContains(m, "home Z first") {
		t.Fatal("move on an unhomed axis not refused")
	}
	if !strings.Contains(m.renderJog(), "?") && !strings.Contains(m.renderJog(), "unhomed") {
		t.Error("unhomed Z not marked")
	}
	if _, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyPgUp}); cmd != nil {
		t.Error("jog on an unhomed axis not refused")
	}

	// The step row takes a custom step for the session.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}, keyRunes("2"), keyRunes("."), keyRunes("5"), tea.KeyMsg{Type: tea.KeyEnter})
	if m.thCursor != thStep || m.steps[m.stepIndex] != 2.5 {
		t.Fatalf("custom step: cursor %d steps %v index %d", m.thCursor, m.steps, m.stepIndex)
	}
	m.raw.merge("toolhead", map[string]any{"homed_axes": "xyz"})
	_, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	if got := sentGcode(t, cmd); !strings.Contains(got, "G1 Z2.5 ") {
		t.Fatalf("jog with custom step sent %q", got)
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
	m := readyModel(t, srv) // 160 columns: State drops first, History stays
	if strings.Join(m.tempCols, ",") != "current,target,history" || len(m.table.Columns()) != 4 {
		t.Fatalf("wide: cols=%v table cols=%d", m.tempCols, len(m.table.Columns()))
	}
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if strings.Join(m.tempCols, ",") != "current,target" || len(m.table.Columns()) != 3 {
		t.Fatalf("narrow: cols=%v table cols=%d", m.tempCols, len(m.table.Columns()))
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
		limit := m.layout.w(focusJog) - panelInnerXReserve()
		for _, l := range strings.Split(m.renderJog(), "\n") {
			if lipgloss.Width(l) > limit {
				t.Errorf("%d cols: toolhead line %q is %d wide, pane fits %d", w, l, lipgloss.Width(l), limit)
			}
		}
	}
}
