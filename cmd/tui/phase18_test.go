package main

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker/moonrakertest"
)

func macrosPrinter(t *testing.T) *moonrakertest.Server {
	t.Helper()
	srv := fakePrinter(t)
	srv.Handle("printer.objects.list", moonrakertest.Result(map[string]any{"objects": []string{
		"webhooks", "toolhead", "extruder", "heater_bed", "print_stats", "virtual_sdcard",
		"gcode_macro PRINT_START", "gcode_macro PRINT_END", "gcode_macro LOAD_FILAMENT",
		"gcode_macro _PRIVATE", "gcode_macro CLEAN_NOZZLE",
	}}))
	srv.Handle("printer.objects.query", moonrakertest.Result(map[string]any{
		"eventtime": 1.0,
		"status": map[string]any{"configfile": map[string]any{"settings": map[string]any{
			"gcode_macro print_start":   map[string]any{"description": "Start a print", "gcode": "M190 S{params.BED|default(60)}\nM109 S{params.EXTRUDER|default(210)}"},
			"gcode_macro print_end":     map[string]any{"description": "G-Code macro", "gcode": "TURN_OFF_HEATERS"},
			"gcode_macro load_filament": map[string]any{"description": "Load filament", "gcode": "G1 E{params.LENGTH|default(50)} F300"},
			"gcode_macro clean_nozzle":  map[string]any{"gcode": "G28"},
		}}},
	}))
	return srv
}

func TestMacrosBrowsePinRun(t *testing.T) {
	srv := macrosPrinter(t)
	m := readyModel(t, srv)
	if len(m.macros) != 4 {
		t.Fatalf("macros %+v", m.macros)
	}
	// Nothing pinned: the pane says how to pin.
	if body := m.renderMacros(); !strings.Contains(body, "No macros pinned") || !strings.Contains(body, "all 4") {
		t.Fatalf("empty pane:\n%s", body)
	}

	// F3 from anywhere opens the browser; typing filters.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyF3})
	if !m.browser.open {
		t.Fatal("F3 did not open the browser")
	}
	m = update(t, m, keyRunes("p"), keyRunes("r"), keyRunes("i"))
	if rows := m.filteredMacros(); len(rows) != 2 {
		t.Fatalf("filter 'pri' → %+v", rows)
	}
	m = update(t, m, keyRunes("n"), keyRunes("t"), keyRunes("_"), keyRunes("s"))
	if rows := m.filteredMacros(); len(rows) != 1 || rows[0].Name != "PRINT_START" {
		t.Fatalf("filter 'print_s' → %+v", rows)
	}
	if v := m.View(); !strings.Contains(v, "PRINT_START") || !strings.Contains(v, "Start a print") || strings.Contains(v, "LOAD_FILAMENT") {
		t.Fatalf("browser view:\n%s", v)
	}
	// Space pins (saved to the printer's database).
	m, cmd := updateCmd(t, m, keyRunes(" "))
	if !m.printerCfg.MacroPinned("PRINT_START") || cmd == nil {
		t.Fatal("space did not pin")
	}
	cmd()
	srv.WaitForCall("server.database.post_item", 1, wait)
	var posted struct {
		Value struct {
			Macros []string `json:"macros"`
		} `json:"value"`
	}
	_ = json.Unmarshal(srv.CallsTo("server.database.post_item")[0].Params, &posted)
	if strings.Join(posted.Value.Macros, ",") != "PRINT_START" {
		t.Fatalf("saved %+v", posted.Value)
	}

	// esc clears the filter, then closes.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.browser.open || m.browser.filter.Value() != "" {
		t.Fatal("esc should clear the filter first")
	}
	// Enter on a macro without parameters runs it.
	m = update(t, m, keyRunes("e"), keyRunes("n"), keyRunes("d"))
	m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if g := sentGcode(t, cmd); g != "PRINT_END" || m.browser.open || !logContains(m, ">>> PRINT_END") {
		t.Fatalf("enter sent %q, browser open %v", g, m.browser.open)
	}

	// The pane shows the pinned macro; enter opens its form.
	m = update(t, m, keyRunes("9"))
	if m.focus != focusMacros || !strings.Contains(m.renderMacros(), "PRINT_START") {
		t.Fatalf("focus %v pane:\n%s", m.focus, m.renderMacros())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.macroForm == nil || len(m.macroForm.inputs) != 2 {
		t.Fatal("enter on a macro with parameters did not open its form")
	}
	if v := m.View(); !strings.Contains(v, "Run PRINT_START") || !strings.Contains(v, "default 60") || !strings.Contains(v, "sends  PRINT_START") {
		t.Fatalf("form view:\n%s", v)
	}
	// Only filled fields are sent; delimiters are refused.
	m = update(t, m, keyRunes("6"), keyRunes("5"), tea.KeyMsg{Type: tea.KeyDown}, keyRunes("2"), keyRunes(";"))
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.macroForm == nil || !strings.Contains(m.macroForm.err, "EXTRUDER") {
		t.Fatalf("a ; in a value was accepted (form %+v)", m.macroForm)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyBackspace}, tea.KeyMsg{Type: tea.KeyBackspace})
	m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if g := sentGcode(t, cmd); g != "PRINT_START BED=65" || m.macroForm != nil {
		t.Fatalf("form sent %q", g)
	}

	// x unpins.
	m, _ = updateCmd(t, m, keyRunes("x"))
	if m.printerCfg.MacroPinned("PRINT_START") {
		t.Fatal("x did not unpin")
	}
}

func TestMacroKeysStayTextInTheConsole(t *testing.T) {
	m := readyModel(t, macrosPrinter(t))
	m.setFocus(focusConsole)
	m = update(t, m, keyRunes("g"))
	if m.browser.open || m.input.Value() != "g" {
		t.Fatalf("g in the console: browser %v input %q", m.browser.open, m.input.Value())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyF3})
	if !m.browser.open {
		t.Fatal("F3 in the console did not open the browser")
	}
	// E-stop closes it.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.browser.open || m.confirmation == nil {
		t.Fatal("ctrl+x from the browser did not arm e-stop")
	}
}

func TestMacrosShareTheAFCColumn(t *testing.T) {
	m := readyModel(t, macrosPrinter(t)) // no AFC: Macros has column 1
	l := m.layout
	if l.w(focusMacros) == 0 || l.w(focusMacros)+l.w(focusExtruder)+l.w(focusConsole) != 160-6 {
		t.Fatalf("widths macros %d extruder %d console %d", l.w(focusMacros), l.w(focusExtruder), l.w(focusConsole))
	}
	if v := m.View(); !strings.Contains(v, "9 Macros") || len(strings.Split(strings.TrimRight(v, "\n"), "\n")) > 50 {
		t.Fatal("Macros pane missing or the view overflows")
	}
}
