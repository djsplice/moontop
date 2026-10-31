package main

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker/moonrakertest"
)

// outputsPrinter is the fake printer plus one of every output kind.
func outputsPrinter(t *testing.T) *moonrakertest.Server {
	t.Helper()
	srv := moonrakertest.New(t)
	srv.Handle("printer.objects.list", moonrakertest.Result(map[string]any{"objects": []string{
		"webhooks", "toolhead", "extruder", "heater_bed", "print_stats", "virtual_sdcard",
		"fan", "fan_generic aux_fan", "heater_fan hotend_fan", "controller_fan board",
		"output_pin caselight", "output_pin beeper", "output_pin _private", "output_pin dimmer",
		"neopixel bar", "led status",
	}}))
	srv.Handle("printer.objects.subscribe", moonrakertest.Result(map[string]any{
		"eventtime": 1.0,
		"status": map[string]any{
			"webhooks":              map[string]any{"state": "ready"},
			"extruder":              map[string]any{"temperature": 25.0, "target": 0.0},
			"heater_bed":            map[string]any{"temperature": 24.0, "target": 0.0},
			"print_stats":           map[string]any{"state": "standby"},
			"fan":                   map[string]any{"speed": 0.5, "rpm": nil},
			"fan_generic aux_fan":   map[string]any{"speed": 0.0},
			"heater_fan hotend_fan": map[string]any{"speed": 1.0, "rpm": 6900.0},
			"controller_fan board":  map[string]any{"speed": 0.3},
			"output_pin caselight":  map[string]any{"value": 0.4},
			"output_pin beeper":     map[string]any{"value": 0.0},
			"output_pin dimmer":     map[string]any{"value": 0.5},
			"neopixel bar":          map[string]any{"color_data": []any{[]any{0.0, 0.0, 0.0, 0.0}}},
			"led status":            map[string]any{"color_data": []any{[]any{1.0, 0.5, 0.0, 0.0}}},
		},
	}))
	srv.Handle("printer.objects.query", moonrakertest.Result(map[string]any{
		"eventtime": 1.0,
		"status": map[string]any{"configfile": map[string]any{"settings": map[string]any{
			"output_pin caselight": map[string]any{"pwm": true, "scale": 1.0},
			"output_pin beeper":    map[string]any{"pwm": false},
			"output_pin dimmer":    map[string]any{"pwm": true, "scale": 10.0},
			"neopixel bar":         map[string]any{"color_order": []any{"GRBW"}},
			"led status":           map[string]any{},
		}}},
	}))
	return srv
}

func outputsModel(t *testing.T) (model, *moonrakertest.Server) {
	t.Helper()
	srv := outputsPrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, keyRunes("8"))
	if m.focus != focusOutputs {
		t.Fatalf("8 → %v", m.focus)
	}
	return m, srv
}

func TestOutputsDiscovery(t *testing.T) {
	m, srv := outputsModel(t)
	var labels []string
	for _, o := range m.outputs {
		labels = append(labels, outputLabel(o))
	}
	// Part fan first, then fans, auto fans, pins, LEDs; "_" names hidden.
	want := "Part fan,aux fan,board,hotend fan,beeper,caselight,dimmer,bar,status"
	if got := strings.Join(labels, ","); got != want {
		t.Fatalf("outputs %s, want %s", got, want)
	}
	var params struct {
		Objects map[string][]string `json:"objects"`
	}
	_ = json.Unmarshal(srv.CallsTo("printer.objects.subscribe")[0].Params, &params)
	for obj, fields := range map[string]string{"fan": "speed,rpm", "output_pin caselight": "value", "neopixel bar": "color_data"} {
		if got := strings.Join(params.Objects[obj], ","); got != fields {
			t.Errorf("%s subscribed %q, want %q", obj, got, fields)
		}
	}
	if _, ok := params.Objects["output_pin _private"]; ok {
		t.Error("a _-prefixed pin was subscribed")
	}
	body := m.renderOutputs()
	for _, s := range []string{"Part fan", "50 %", "hotend fan", "100% 6900rpm", "beeper", "off", "caselight", "40 %", "dimmer", "5.00", "status", "100 %"} {
		if !strings.Contains(body, s) {
			t.Errorf("Outputs lacks %q:\n%s", s, body)
		}
	}
}

func TestOutputsControl(t *testing.T) {
	m, _ := outputsModel(t)
	at := func(label string) {
		for i, o := range m.outputs {
			if outputLabel(o) == label {
				m.outFields.cursor = i
				return
			}
		}
		t.Fatalf("no output %q", label)
	}
	m.job.State = "printing" // all of these work mid-print

	at("Part fan")
	m, g := send16(t, m, "right")
	if g != "M106 S140" || !logContains(m, ">>> M106 S140") {
		t.Fatalf("part fan → sent %q", g)
	}
	if m, g = send16(t, m, "x"); g != "M107" {
		t.Fatalf("part fan x sent %q", g)
	}
	at("aux fan")
	if m, g = send16(t, m, "6", "0", "enter"); g != "SET_FAN_SPEED FAN=aux_fan SPEED=0.6" {
		t.Fatalf("aux fan typed sent %q", g)
	}
	at("hotend fan")
	if m, g = send16(t, m, "left"); g != "" || !logContains(m, "set by Klipper") {
		t.Fatalf("heater fan ← sent %q", g)
	}
	at("caselight")
	if m, g = send16(t, m, "7", "5", "enter"); g != "SET_PIN PIN=caselight VALUE=0.75" {
		t.Fatalf("pwm pin sent %q", g)
	}
	at("beeper")
	if m, g = send16(t, m, "right"); g != "SET_PIN PIN=beeper VALUE=1" {
		t.Fatalf("digital pin → sent %q", g)
	}
	if m, g = send16(t, m, "0", ".", "5", "enter"); g != "" || !logContains(m, "is on/off: type 1 or 0") {
		t.Fatalf("digital pin 0.5 sent %q", g)
	}
	at("dimmer") // scale 10: shown and set in its own units
	if m, g = send16(t, m, "7", "enter"); g != "SET_PIN PIN=dimmer VALUE=7" {
		t.Fatalf("scaled pin sent %q", g)
	}
	at("bar") // off GRBW strip: brightness up starts white
	if m, g = send16(t, m, "right"); g != "SET_LED LED=bar RED=0 GREEN=0 BLUE=0 WHITE=0.1" {
		t.Fatalf("LED from off sent %q", g)
	}
	at("status") // RGB at full: brightness scales the colour
	if m, g = send16(t, m, "5", "0", "enter"); g != "SET_LED LED=status RED=0.5 GREEN=0.25 BLUE=0" {
		t.Fatalf("LED 50 %% sent %q", g)
	}
	// o: colour on the prompt line.
	m = update(t, m, keyRunes("o"))
	if m.lineEdit == nil {
		t.Fatal("o did not open the colour prompt")
	}
	m.lineEdit.input.SetValue("zz")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.lineEdit == nil || !strings.Contains(m.lineEdit.err, "hex") {
		t.Fatal("bad colour accepted")
	}
	m.lineEdit.input.SetValue("FF8800")
	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if g := sentGcode(t, cmd); g != "SET_LED LED=status RED=1 GREEN=0.533 BLUE=0" {
		t.Fatalf("colour sent %q", g)
	}
	at("caselight")
	if m = update(t, m, keyRunes("o")); m.lineEdit != nil {
		t.Fatal("o opened a colour prompt on a pin")
	}
}

// A long name keeps its full label when the values are short (the U1's
// "e0 nozzle fan" was cut to "e0 nozzle fa").
func TestOutputLabelsUseTheSpaceValuesLeave(t *testing.T) {
	m, _ := outputsModel(t)
	m.outputs = append(m.outputs, output{object: "heater_fan e0_nozzle_fan", name: "e0_nozzle_fan", kind: outAutoFan, scale: 1})
	m.raw.merge("heater_fan e0_nozzle_fan", map[string]any{"speed": 0.0, "rpm": 0.0})
	m = update(t, m, tea.WindowSizeMsg{Width: 160, Height: 60})
	if body := m.renderOutputs(); !strings.Contains(body, "e0 nozzle fan") {
		t.Fatalf("label cut:\n%s", body)
	}
}

func TestOutputsStackUnderExtruder(t *testing.T) {
	m, _ := outputsModel(t)
	l := m.layout
	if l.w(focusExtruder) != l.w(focusOutputs) || l.w(focusExtruder)+l.w(focusConsole) != 160-6 {
		t.Fatalf("widths: extruder %d outputs %d console %d", l.w(focusExtruder), l.w(focusOutputs), l.w(focusConsole))
	}
	bottomH := m.layout.h(focusConsole)
	if bottomH == 0 {
		bottomH = 50 - 5 - m.layout.topRowH
	}
	if l.h(focusExtruder)+l.h(focusOutputs) != 50-5-l.topRowH {
		t.Fatalf("heights: extruder %d outputs %d, bottom %d", l.h(focusExtruder), l.h(focusOutputs), 50-5-l.topRowH)
	}
	v := m.View()
	if !strings.Contains(v, "7 Extruder") || !strings.Contains(v, "8 Outputs") {
		t.Fatal("Extruder or Outputs missing from the view")
	}
	if n := len(strings.Split(strings.TrimRight(v, "\n"), "\n")); n > 50 {
		t.Fatalf("view is %d lines on a 50-line terminal", n)
	}
	// Short terminal: Outputs scrolls to keep the cursor in view.
	m = update(t, m, tea.WindowSizeMsg{Width: 160, Height: 34})
	m.outFields.cursor = len(m.outputs) - 1
	body := m.renderOutputs()
	if !strings.Contains(body, "status") || !strings.Contains(body, "▸") || len(strings.Split(body, "\n")) > m.layout.h(focusOutputs)-3 {
		t.Fatalf("cursor row not visible in a %d-line box:\n%s", m.layout.h(focusOutputs), body)
	}
	if n := len(strings.Split(strings.TrimRight(m.View(), "\n"), "\n")); n > 34 {
		t.Fatalf("view is %d lines on a 34-line terminal", n)
	}
}
