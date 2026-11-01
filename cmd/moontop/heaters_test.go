package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/moonraker/moonrakertest"
)

func TestHeaterCommand(t *testing.T) {
	cases := map[string]string{
		"extruder":                   "SET_HEATER_TEMPERATURE HEATER=extruder TARGET=210",
		"extruder3":                  "SET_HEATER_TEMPERATURE HEATER=extruder3 TARGET=210",
		"heater_bed":                 "SET_HEATER_TEMPERATURE HEATER=heater_bed TARGET=210",
		"heater_generic chamber":     "SET_HEATER_TEMPERATURE HEATER=chamber TARGET=210",
		"temperature_fan exhaust":    "SET_TEMPERATURE_FAN_TARGET TEMPERATURE_FAN=exhaust TARGET=210",
		"temperature_sensor mcu":     "",
		"extruder_stepper something": "",
	}
	for obj, want := range cases {
		got, ok := heaterCommand(obj, 210)
		if got != want || ok != (want != "") {
			t.Errorf("heaterCommand(%q) = %q, %v; want %q", obj, got, ok, want)
		}
	}
	if got, _ := heaterCommand("extruder", 212.5); !strings.HasSuffix(got, "TARGET=212.5") {
		t.Errorf("fractional target: %q", got)
	}
}

// scripts returns every printer.gcode.script the fake received.
func scripts(srv *moonrakertest.Server) []string {
	var out []string
	for _, c := range srv.CallsTo("printer.gcode.script") {
		var p struct {
			Script string `json:"script"`
		}
		_ = json.Unmarshal(c.Params, &p)
		out = append(out, p.Script)
	}
	return out
}

func tempsFocused(t *testing.T, srv *moonrakertest.Server) model {
	t.Helper()
	m := readyModel(t, srv)
	return update(t, m, keyRunes("4"))
}

func runCmd(cmd tea.Cmd) {
	if cmd != nil {
		cmd()
	}
}

func TestEditTargetInPlace(t *testing.T) {
	srv := fakePrinter(t)
	m := tempsFocused(t, srv) // cursor on Extruder (sorted first)

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.tempEdit != "extruder" {
		t.Fatalf("editing %q", m.tempEdit)
	}
	// Digits are the value, not pane jumps; letters are rejected; q
	// doesn't quit.
	for _, k := range []string{"2", "1", "q", "0"} {
		var cmd tea.Cmd
		m, cmd = updateCmd(t, m, keyRunes(k))
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatal("q quit while editing")
			}
		}
	}
	if m.focus != focusTable || m.tempInput.Value() != "210" {
		t.Fatalf("focus=%v input=%q", m.focus, m.tempInput.Value())
	}
	if !strings.Contains(m.table.View(), "→ 210▏") {
		t.Fatalf("edited cell not shown:\n%s", m.table.View())
	}
	pane, _ := footerHints(&m)
	if !strings.Contains(strings.Join(pane, "|"), "esc cancel") {
		t.Fatalf("footer while editing: %q", pane)
	}

	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(cmd)
	srv.WaitForCall("printer.gcode.script", 1, wait)
	if got := scripts(srv); len(got) != 1 || got[0] != "SET_HEATER_TEMPERATURE HEATER=extruder TARGET=210" {
		t.Fatalf("sent %q", got)
	}
	// Display stays on confirmed state until Klipper reports it.
	if m.tempEdit != "" || strings.Contains(m.table.View(), "210") {
		t.Fatalf("optimistic display:\n%s", m.table.View())
	}
	if !logContains(m, ">>> SET_HEATER_TEMPERATURE HEATER=extruder TARGET=210") {
		t.Error("command not echoed to the console")
	}
}

func TestEditPrefillsCurrentTargetAndEscCancels(t *testing.T) {
	srv := fakePrinter(t)
	m := tempsFocused(t, srv)
	m.sensors["extruder"].Target = 215
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.tempInput.Value() != "215" {
		t.Fatalf("prefill %q", m.tempInput.Value())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	time.Sleep(30 * time.Millisecond)
	if m.tempEdit != "" || len(scripts(srv)) != 0 {
		t.Fatalf("esc: editing=%q sent=%q", m.tempEdit, scripts(srv))
	}
}

func TestEditOnSensorIsRefused(t *testing.T) {
	srv := fakePrinter(t)
	srv.Handle("printer.objects.list", moonrakertest.Result(map[string]any{
		"objects": []string{"webhooks", "toolhead", "temperature_sensor mcu"},
	}))
	m := tempsFocused(t, srv)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.tempEdit != "" || !logContains(m, "is a sensor; it has no target") {
		t.Fatalf("editing=%q", m.tempEdit)
	}
}

func TestHeaterOffAndCooldown(t *testing.T) {
	srv := fakePrinter(t)
	m := tempsFocused(t, srv)

	_, cmd := updateCmd(t, m, keyRunes("x"))
	runCmd(cmd)
	_, cmd = updateCmd(t, m, keyRunes("X"))
	runCmd(cmd)
	srv.WaitForCall("printer.gcode.script", 2, wait)
	got := strings.Join(scripts(srv), "|")
	if got != "SET_HEATER_TEMPERATURE HEATER=extruder TARGET=0|TURN_OFF_HEATERS" {
		t.Fatalf("sent %q", got)
	}

	// During a print both confirm, in red.
	m.job.State = "printing"
	for _, k := range []string{"x", "X"} {
		mm := update(t, m, keyRunes(k))
		if mm.confirmation == nil || !mm.confirmation.danger {
			t.Fatalf("%s during print: no danger confirmation", k)
		}
	}
}

func mainsailPresetsJSON() map[string]any {
	return map[string]any{
		"cooldownGcode": "TURN_OFF_HEATERS\nM107",
		"presets": map[string]any{
			"b-id": map[string]any{"name": "PETG", "gcode": "", "values": map[string]any{
				"extruder":   map[string]any{"bool": true, "type": "heater", "value": 240},
				"heater_bed": map[string]any{"bool": true, "type": "heater", "value": 80},
			}},
			"a-id": map[string]any{"name": "PLA", "gcode": "M106 S0", "values": map[string]any{
				"extruder":               map[string]any{"bool": true, "type": "heater", "value": 210},
				"heater_bed":             map[string]any{"bool": false, "type": "heater", "value": 60},
				"heater_generic chamber": map[string]any{"bool": true, "type": "heater", "value": 40},
			}},
		},
	}
}

func TestPresetsFromMainsail(t *testing.T) {
	srv := fakePrinter(t)
	srv.Handle("server.database.get_item", func(raw json.RawMessage) (any, error) {
		var p struct{ Namespace, Key string }
		_ = json.Unmarshal(raw, &p)
		if p.Namespace != "mainsail" || p.Key != "presets" {
			t.Errorf("read %s/%s", p.Namespace, p.Key)
		}
		return map[string]any{"namespace": "mainsail", "key": "presets", "value": mainsailPresetsJSON()}, nil
	})
	m := tempsFocused(t, srv)
	m = update(t, m, loadPresetsCmd(m.client)())
	if len(m.presets) != 2 || m.presets[0].Name != "PETG" && m.presets[0].Name != "PLA" {
		t.Fatalf("presets %+v", m.presets)
	}
	if m.presets[1].Name != "PLA" {
		t.Fatalf("not sorted by name: %+v", m.presets)
	}

	m = update(t, m, keyRunes("t"))
	if m.confirmation == nil || len(m.confirmation.options) != 2 {
		t.Fatalf("picker = %+v", m.confirmation)
	}
	if v := m.View(); !strings.Contains(v, "PETG (extruder 240 · bed 80)") {
		t.Errorf("picker not rendered with summaries")
	}
	m, cmd := updateCmd(t, m, keyRunes("2")) // PLA
	runCmd(cmd)
	srv.WaitForCall("printer.gcode.script", 1, wait)
	// The disabled bed value is ignored; the chamber heater this
	// printer doesn't have is skipped with a note; the gcode runs last.
	if got := scripts(srv)[0]; got != "SET_HEATER_TEMPERATURE HEATER=extruder TARGET=210\nM106 S0" {
		t.Fatalf("script %q", got)
	}
	if !logContains(m, "has no heater_generic chamber; skipped") {
		t.Error("skipped heater not reported")
	}

	// Cooldown uses Mainsail's configured gcode.
	_, cmd = updateCmd(t, m, keyRunes("X"))
	runCmd(cmd)
	srv.WaitForCall("printer.gcode.script", 2, wait)
	if got := scripts(srv)[1]; got != "TURN_OFF_HEATERS\nM107" {
		t.Fatalf("cooldown %q", got)
	}
}

func TestPresetPickerEscAndNoPresets(t *testing.T) {
	srv := fakePrinter(t)
	srv.Handle("server.database.get_item", func(json.RawMessage) (any, error) {
		return nil, errNoNamespace{}
	})
	m := tempsFocused(t, srv)
	m = update(t, m, loadPresetsCmd(m.client)())
	m = update(t, m, keyRunes("t"))
	if m.confirmation != nil || !logContains(m, "no preheat presets") {
		t.Fatal("t without presets should explain, not open a picker")
	}
	if ok, why := havePresets(&m); ok || why == "" {
		t.Fatal("preheat key should be marked unavailable")
	}

	m.presets = []preset{{Name: "PLA", Values: []presetValue{{Object: "extruder", Target: 200}}}}
	m = update(t, m, keyRunes("t"))
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	time.Sleep(30 * time.Millisecond)
	if m.confirmation != nil || len(scripts(srv)) != 0 {
		t.Fatal("esc did not cancel the picker")
	}
}

type errNoNamespace struct{}

func (errNoNamespace) Error() string { return "Namespace 'mainsail' not found" }
