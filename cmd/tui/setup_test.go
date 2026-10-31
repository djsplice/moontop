package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moonraker-tui/internal/config"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker/moonrakertest"
)

// setupModel is a ready dashboard with a temp config file.
func setupModel(t *testing.T, srv *moonrakertest.Server) model {
	t.Helper()
	m := readyModel(t, srv)
	m.cfgPath = filepath.Join(t.TempDir(), "config.toml")
	return m
}

// key sends one key and runs (non-blocking) Cmds it returns, feeding
// their messages back — enough for saves and sends.
func press(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
		case "f2":
			msg = tea.KeyMsg{Type: tea.KeyF2}
		case "ctrl+x":
			msg = tea.KeyMsg{Type: tea.KeyCtrlX}
		default:
			msg = keyRunes(k)
		}
		var cmd tea.Cmd
		m, cmd = updateCmd(t, m, msg)
		for _, out := range collect(cmd) {
			if _, ok := out.(settingsSavedMsg); ok {
				m = update(t, m, out)
			}
		}
	}
	return m
}

func typeText(t *testing.T, m model, s string) model {
	for _, r := range s {
		m = update(t, m, keyRunes(string(r)))
	}
	return m
}

// openCategory opens setup on the named category with the item column focused.
func openCategory(t *testing.T, m model, name string) model {
	t.Helper()
	m = press(t, m, "f2")
	for i, c := range setupCategories {
		if c.name == name {
			m.setup.cat = i
		}
	}
	return press(t, m, "tab")
}

func fileText(t *testing.T, m model) string {
	data, _ := os.ReadFile(m.cfgPath)
	return string(data)
}

func lastPrinterSave(t *testing.T, srv *moonrakertest.Server) config.Printer {
	t.Helper()
	calls := srv.CallsTo("server.database.post_item")
	if len(calls) == 0 {
		t.Fatal("nothing saved to the printer")
	}
	var p struct {
		Namespace, Key string
		Value          config.Printer
	}
	_ = json.Unmarshal(calls[len(calls)-1].Params, &p)
	if p.Namespace != "moontop" || p.Key != "printer" {
		t.Fatalf("saved to %s/%s", p.Namespace, p.Key)
	}
	return p.Value
}

func TestSetupOpensAndCloses(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m = press(t, m, ",")
	if !m.setup.open || !strings.Contains(m.View(), "Setup") {
		t.Fatal(", did not open setup")
	}
	for _, c := range setupCategories {
		if !strings.Contains(m.View(), c.name) {
			t.Errorf("category %q not shown", c.name)
		}
	}
	m = press(t, m, "esc")
	if m.setup.open {
		t.Fatal("esc did not close")
	}
	// In the console "," is text; F2 still opens.
	m = press(t, m, "5", ",")
	if m.setup.open || m.input.Value() != "," {
		t.Fatalf("',' in console: open=%v input=%q", m.setup.open, m.input.Value())
	}
	m = press(t, m, "f2")
	if !m.setup.open {
		t.Fatal("F2 in console did not open setup")
	}
	if !strings.Contains(m.View(), "F2") {
		t.Error("view lacks key hints")
	}
}

func TestHidePane(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m = openCategory(t, m, "Panes")
	m = press(t, m, "down", "down", "space") // 3 Tuning
	if !m.cfg.Panes.PaneHidden("Tuning") || !strings.Contains(fileText(t, m), `hidden = ["Tuning"]`) {
		t.Fatalf("not saved: %q", fileText(t, m))
	}
	if !strings.HasPrefix(m.setup.status, "saved to "+m.cfgPath) {
		t.Errorf("status %q", m.setup.status)
	}
	m = press(t, m, "esc", "esc")
	if strings.Contains(m.View(), "3 Tuning") {
		t.Error("hidden pane still drawn")
	}
	// Tab skips it, its number does nothing.
	m.setFocus(focusJog)
	m = press(t, m, "tab")
	if m.focus != focusTable {
		t.Fatalf("tab from Toolhead went to %v", m.focus)
	}
	m = press(t, m, "3")
	if m.focus != focusTable {
		t.Fatalf("3 jumped to a hidden pane")
	}
}

func TestCannotHideEveryPane(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m = openCategory(t, m, "Panes")
	for i := 0; i < len(paneOrder); i++ {
		m = press(t, m, "space", "down")
	}
	if len(m.cfg.Panes.Hidden) != len(paneOrder)-1 || !strings.Contains(m.setup.status, "at least one pane") {
		t.Fatalf("hidden %v status %q", m.cfg.Panes.Hidden, m.setup.status)
	}
	m = press(t, m, "esc", "esc")
	_ = m.View() // must not panic with four panes hidden
}

func TestHideAndRenameHeatersSaveToPrinter(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m = openCategory(t, m, "Temperatures")
	// Items are in table order: extruder, heater_bed.
	m = press(t, m, "down", "space")
	if p := lastPrinterSave(t, srv); !p.Temperatures.IsHidden("heater_bed") {
		t.Fatalf("saved %+v", p)
	}
	m = press(t, m, "up", "enter")
	m = typeText(t, m, "T0 PLA")
	m = press(t, m, "enter")
	if p := lastPrinterSave(t, srv); p.Temperatures.Label("extruder") != "T0 PLA" || !p.Temperatures.IsHidden("heater_bed") {
		t.Fatalf("saved %+v", p)
	}
	m = press(t, m, "esc", "esc")
	tv := m.table.View()
	if !strings.Contains(tv, "T0 PLA") || strings.Contains(tv, "Bed") {
		t.Fatalf("table:\n%s", tv)
	}
	// File untouched by per-printer settings.
	if fileText(t, m) != "" {
		t.Errorf("heater settings leaked into the file: %q", fileText(t, m))
	}
}

func TestPrinterSettingsLoadAndFileFallback(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m.cfg.Temperatures = config.Temperatures{Rename: map[string]string{"extruder": "From file"}}
	m.applySettings()
	m.rebuildRows()
	if !strings.Contains(m.table.View(), "From file") {
		t.Fatal("file renames not used when the printer has none")
	}
	m = update(t, m, printerCfgMsg{cfg: config.Printer{Temperatures: config.Temperatures{Hidden: []string{"extruder"}}}})
	if strings.Contains(m.table.View(), "From file") || strings.Contains(m.table.View(), "Extruder") {
		t.Fatalf("printer settings did not win:\n%s", m.table.View())
	}
}

func TestConsoleFilterSettings(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m.appendLog("B:26.3 /0.0 T0:168.1 /140.0")
	m.appendLog("echo: Nozzle cleared")
	if m.hiddenCount != 1 {
		t.Fatalf("hidden %d", m.hiddenCount)
	}
	m = openCategory(t, m, "Console")
	m = press(t, m, "space") // heat-wait filter off
	if m.hiddenCount != 0 || !strings.Contains(fileText(t, m), "hide_wait_temperatures = false") {
		t.Fatalf("hidden %d file %q", m.hiddenCount, fileText(t, m))
	}
	// Bad regex: error, still editing.
	m = press(t, m, "a")
	m = typeText(t, m, "(")
	m = press(t, m, "enter")
	if m.setup.editing == nil || !strings.HasPrefix(m.setup.status, "!! ") {
		t.Fatalf("bad pattern accepted: status %q", m.setup.status)
	}
	m = press(t, m, "esc", "a")
	m = typeText(t, m, "^echo:")
	m = press(t, m, "enter")
	if m.hiddenCount != 1 || !strings.Contains(fileText(t, m), `pattern = "^echo:"`) {
		t.Fatalf("custom filter: hidden %d file %q", m.hiddenCount, fileText(t, m))
	}
	// d d deletes it (it's the third item).
	m = press(t, m, "down", "down", "d")
	if len(m.cfg.Console.Filters) != 1 {
		t.Fatal("one d deleted")
	}
	m = press(t, m, "d")
	if len(m.cfg.Console.Filters) != 0 || m.hiddenCount != 0 {
		t.Fatalf("filters %v hidden %d", m.cfg.Console.Filters, m.hiddenCount)
	}
}

func TestToolheadSettings(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m = openCategory(t, m, "Toolhead")
	m = press(t, m, "enter")
	m.setup.input.SetValue("")
	m = typeText(t, m, "5, 0.5, nope")
	m = press(t, m, "enter")
	if m.setup.editing == nil || !strings.Contains(m.setup.status, "nope") {
		t.Fatalf("bad step accepted: %q", m.setup.status)
	}
	m.setup.input.SetValue("5, 0.5")
	m = press(t, m, "enter")
	if len(m.steps) != 2 || m.steps[0] != 0.5 || m.steps[m.stepIndex] != 5 {
		t.Fatalf("steps %v index %d", m.steps, m.stepIndex)
	}
	m = press(t, m, "down", "down", "enter")
	m.setup.input.SetValue("300")
	m = press(t, m, "enter")
	m = press(t, m, "esc", "esc", "2")
	_, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if sent := cmd().(gcodeSentMsg); !strings.Contains(sent.cmd, "G1 Z5 F300") {
		t.Fatalf("jog sent %q", sent.cmd)
	}
	if !strings.Contains(fileText(t, m), "z_feedrate = 300") {
		t.Fatalf("file %q", fileText(t, m))
	}
}

func TestSafetySettings(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m = openCategory(t, m, "Safety")
	m = press(t, m, "space", "down", "space")
	m = press(t, m, "esc", "esc")

	// E-stop without confirmation.
	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.confirmation != nil || cmd == nil {
		t.Fatal("e-stop still confirms")
	}
	cmd()
	srv.WaitForCall("printer.emergency_stop", 1, wait)

	// Idle restart sends at once; during a print it still asks.
	m, cmd = updateCmd(t, m, keyRunes("K"))
	if m.confirmation != nil || cmd == nil {
		t.Fatal("idle restart still confirms")
	}
	m.job.State = "printing"
	m = update(t, m, keyRunes("K"))
	if m.confirmation == nil || !m.confirmation.danger {
		t.Fatal("restart during a print must confirm")
	}
}

func TestSaveCurrentTargetsAsPreset(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m = openCategory(t, m, "Presets")
	m = press(t, m, "a")
	m = typeText(t, m, "PLA")
	m = press(t, m, "enter")
	if !strings.Contains(m.setup.status, "no heater has a target") {
		t.Fatalf("status %q", m.setup.status)
	}
	m = press(t, m, "esc")
	m.sensors["extruder"].Target = 210
	m.sensors["heater_bed"].Target = 60
	m = press(t, m, "a")
	m = typeText(t, m, "PLA")
	m = press(t, m, "enter")
	p := lastPrinterSave(t, srv)
	if len(p.Presets) != 1 || p.Presets[0].Targets["extruder"] != 210 || p.Presets[0].Targets["heater_bed"] != 60 {
		t.Fatalf("saved %+v", p.Presets)
	}
	// It's offered by t, first.
	m = press(t, m, "esc", "esc", "4", "t")
	if m.confirmation == nil || !strings.HasPrefix(m.confirmation.options[0].label, "PLA") {
		t.Fatalf("picker %+v", m.confirmation)
	}
	m = press(t, m, "esc")
	// d d deletes it.
	m = openCategory(t, m, "Presets")
	m = press(t, m, "d", "d")
	if p := lastPrinterSave(t, srv); len(p.Presets) != 0 {
		t.Fatalf("not deleted: %+v", p.Presets)
	}
}

func TestUnreadableConfigIsNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	bad := "[panes\nhidden = 3"
	_ = os.WriteFile(path, []byte(bad), 0o644)
	cfg, got := loadConfig(path)
	if got != "" || len(cfg.Panes.Hidden) != 0 {
		t.Fatalf("path %q cfg %+v", got, cfg)
	}
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m.cfgPath = got
	m = openCategory(t, m, "Safety")
	m = press(t, m, "space")
	if !strings.Contains(m.setup.status, "had errors") {
		t.Fatalf("status %q", m.setup.status)
	}
	if data, _ := os.ReadFile(path); string(data) != bad {
		t.Fatal("broken file was overwritten")
	}
}

func layoutWith(w, h int, hidden ...focusArea) layout {
	in := defaultLayoutInput(w, h, 3)
	in.hidden = map[focusArea]bool{}
	for _, f := range hidden {
		in.hidden[f] = true
	}
	return computeLayout(in)
}

func TestLayoutWithHiddenPanes(t *testing.T) {
	l := layoutWith(160, 50, focusConsole)
	if l.topRowH != 50-5 {
		t.Errorf("console hidden: top row %d, want all %d rows", l.topRowH, 50-5)
	}
	l = layoutWith(160, 50, focusFiles, focusTable)
	if l.w(focusJog)+l.w(focusTuning) != 160-6 {
		t.Errorf("natural-width panes should fill the row: %d + %d", l.w(focusJog), l.w(focusTuning))
	}
	l = layoutWith(160, 50, focusFiles)
	if l.w(focusFiles) != 0 || l.w(focusTable) != 160-6-l.w(focusJog)-l.w(focusTuning) {
		t.Errorf("table should take Files' space: %+v", l.boxW)
	}
	l = layoutWith(70, 60, focusJog, focusTuning)
	if l.mode != layoutStacked || l.h(focusJog) != 0 || l.h(focusTuning) != 0 {
		t.Errorf("stacked hidden: %+v", l.boxH)
	}
}

// Every setup page and the help overlay must fit the terminal exactly:
// a single wrapped line makes the view taller than the screen, which
// scrolls the title away (and back) as you move between categories.
func TestOverlaysFitTheTerminal(t *testing.T) {
	srv := fakePrinter(t)
	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 36}, {160, 50}, {237, 60}} {
		m := setupModel(t, srv)
		m = update(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		check := func(what string) {
			t.Helper()
			v := strings.TrimSuffix(m.View(), "\n")
			lines := strings.Split(v, "\n")
			if len(lines) > size[1] {
				t.Errorf("%dx%d %s: %d lines", size[0], size[1], what, len(lines))
			}
			for _, l := range lines {
				if lipgloss.Width(l) > size[0] {
					t.Errorf("%dx%d %s: line %d wide: %q", size[0], size[1], what, lipgloss.Width(l), l)
					break
				}
			}
			if !strings.Contains(lines[0], "Moontop") {
				t.Errorf("%dx%d %s: title not on the first line", size[0], size[1], what)
			}
		}
		m = press(t, m, "f2", "tab")
		for i := range setupCategories {
			m.setup.cat, m.setup.item = i, 0
			check("setup/" + setupCategories[i].name)
		}
		m.setup.cat = 0
		m = press(t, m, "enter") // editing: prompt + input line
		check("setup/editing")
		m = press(t, m, "esc", "esc", "esc", "?")
		check("help")
	}
}
