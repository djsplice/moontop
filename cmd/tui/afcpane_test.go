package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker/moonrakertest"
)

// afcPrinter is fakePrinter plus the U1 AFC-Lite status from
// testdata/afc-lite-u1.json. LANE_UNLOAD is deliberately not advertised,
// like a stub that doesn't implement it.
func afcPrinter(t *testing.T) *moonrakertest.Server {
	t.Helper()
	data, err := os.ReadFile("../../testdata/afc-lite-u1.json")
	if err != nil {
		t.Fatal(err)
	}
	var afcObjs map[string]any
	if err := json.Unmarshal(data, &afcObjs); err != nil {
		t.Fatal(err)
	}
	srv := fakePrinter(t)
	objects := []string{"webhooks", "toolhead", "extruder", "heater_bed", "print_stats", "virtual_sdcard"}
	status := map[string]any{
		"webhooks": map[string]any{"state": "ready"},
		"extruder": map[string]any{"temperature": 25.0, "target": 0.0},
	}
	for k, v := range afcObjs {
		objects = append(objects, k)
		status[k] = v
	}
	srv.Handle("printer.objects.list", moonrakertest.Result(map[string]any{"objects": objects}))
	srv.Handle("printer.objects.subscribe", moonrakertest.Result(map[string]any{"eventtime": 1.0, "status": status}))
	srv.Handle("printer.gcode.help", moonrakertest.Result(map[string]string{
		"CHANGE_TOOL": "", "TOOL_UNLOAD": "", "SET_MAP": "", "SET_MATERIAL": "", "SET_COLOR": "",
		"SET_WEIGHT": "", "SET_SPOOL_ID": "", "AFC_CLEAR_MESSAGE": "", "G28": "",
	}))
	return srv
}

func afcReady(t *testing.T) (model, *moonrakertest.Server) {
	t.Helper()
	srv := afcPrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, loadGcodeHelpCmd(m.client)())
	return m, srv
}

func afcStatus(t *testing.T, m model, objs map[string]any) model {
	t.Helper()
	return update(t, m, notificationMsg{Method: moonraker.MethodStatusUpdate, Params: rawParams(t, objs, 2.0)})
}

func TestAFCSubscribedAndShown(t *testing.T) {
	m, srv := afcReady(t)
	var params struct {
		Objects map[string]any `json:"objects"`
	}
	_ = json.Unmarshal(srv.CallsTo("printer.objects.subscribe")[0].Params, &params)
	for _, k := range []string{"AFC", "AFC_unit U1", "AFC_lane E0", "AFC_lane E3"} {
		if v, ok := params.Objects[k]; !ok || v != nil {
			t.Errorf("%s subscribed as %v, want all fields (null)", k, v)
		}
	}
	v := m.View()
	if !strings.Contains(v, "6 AFC") {
		t.Fatal("AFC pane missing")
	}
	m = press(t, m, "6")
	body := m.renderAFC()
	for _, want := range []string{"idle", "active E1", "E0", "ASA", "In tool", "E1*", "Empty"} {
		if !strings.Contains(body, want) {
			t.Errorf("AFC pane lacks %q:\n%s", want, body)
		}
	}
}

func TestAFCLoadUnloadEject(t *testing.T) {
	m, srv := afcReady(t)
	m = press(t, m, "6") // cursor on E0: in tool, not active

	// enter loads, after a y.
	m = press(t, m, "enter")
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "Load lane E0 into T0") {
		t.Fatalf("confirmation %+v", m.confirmation)
	}
	m, cmd := updateCmd(t, m, keyRunes("y"))
	runCmd(cmd)
	srv.WaitForCall("printer.gcode.script", 1, wait)
	if got := scripts(srv)[0]; got != "CHANGE_TOOL LANE=E0" {
		t.Fatalf("sent %q", got)
	}

	// E0 is in its tool: unload confirms. (Eject-before-unload is
	// refused too; see internal/afc. Here LANE_UNLOAD isn't advertised,
	// which is checked below.)
	m = press(t, m, "u")
	m, cmd = updateCmd(t, m, keyRunes("y"))
	runCmd(cmd)
	srv.WaitForCall("printer.gcode.script", 2, wait)
	if got := scripts(srv)[1]; got != "TOOL_UNLOAD LANE=E0" {
		t.Fatalf("sent %q", got)
	}

	// E1 is the active loaded lane: load refused.
	m = press(t, m, "j", "enter")
	if m.confirmation != nil || !logContains(m, "already the active loaded lane") {
		t.Fatal("reloading the active lane not refused")
	}
	// E3: LANE_UNLOAD isn't advertised by this printer.
	m = press(t, m, "j", "j", "e")
	if m.confirmation != nil || !logContains(m, "LANE_UNLOAD isn't available on this printer") {
		t.Fatal("unadvertised command offered")
	}
}

func TestAFCBlockedWhilePrintingOrBusy(t *testing.T) {
	m, _ := afcReady(t)
	m = press(t, m, "6")
	m.job.State = "printing"
	m = press(t, m, "u")
	if m.confirmation != nil || !logContains(m, "finish the current print first") {
		t.Fatal("AFC action allowed during a print")
	}
	m.job.State = "standby"
	m = afcStatus(t, m, map[string]any{"AFC": map[string]any{"current_state": "Loading"}})
	m = press(t, m, "u")
	if m.confirmation != nil || !logContains(m, "AFC is busy (Loading)") {
		t.Fatal("AFC action allowed while busy")
	}
	if ok, _ := afcIdle(&m); ok {
		t.Error("help/footer should show AFC keys as unavailable while busy")
	}
}

func TestAFCMessageAndClear(t *testing.T) {
	m, srv := afcReady(t)
	m = press(t, m, "6")
	m = afcStatus(t, m, map[string]any{"AFC": map[string]any{"error_state": true, "message": map[string]any{"message": "Lane E2 jammed", "type": "error"}}})
	if !strings.Contains(m.renderAFC(), "Lane E2 jammed") {
		t.Fatal("message not shown")
	}
	m = press(t, m, "u")
	if m.confirmation != nil || !logContains(m, "AFC reports an error") {
		t.Fatal("action allowed in error state")
	}
	_, cmd := updateCmd(t, m, keyRunes("x"))
	runCmd(cmd)
	srv.WaitForCall("printer.gcode.script", 1, wait)
	if got := scripts(srv)[0]; got != "AFC_CLEAR_MESSAGE" {
		t.Fatalf("sent %q", got)
	}
}

func TestAFCLaneEdits(t *testing.T) {
	m, srv := afcReady(t)
	m = press(t, m, "6", "j", "j", "j") // E3
	m = press(t, m, "t")
	if m.lineEdit == nil || !strings.Contains(m.View(), "Lane E3 tool") {
		t.Fatal("tool prompt not open")
	}
	m.lineEdit.input.SetValue("tool5")
	m = press(t, m, "enter")
	if m.lineEdit == nil || !strings.Contains(m.lineEdit.err, "T0") {
		t.Fatalf("bad tool accepted: %+v", m.lineEdit)
	}
	m.lineEdit.input.SetValue("t5")
	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(cmd)
	m = press(t, m, "m")
	m.lineEdit.input.SetValue("PLA Silk")
	m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(cmd)
	m = press(t, m, "m")
	m.lineEdit.input.SetValue("PLA; M112")
	m = press(t, m, "enter")
	if m.lineEdit == nil || m.lineEdit.err == "" {
		t.Fatal("material with a gcode delimiter accepted")
	}
	m = press(t, m, "esc")
	srv.WaitForCall("printer.gcode.script", 2, wait)
	if got := strings.Join(scripts(srv), "|"); got != `SET_MAP LANE=E3 MAP=T5|SET_MATERIAL LANE=E3 MATERIAL="PLA Silk"` {
		t.Fatalf("sent %q", got)
	}
	// Display waits for AFC to confirm.
	if strings.Contains(m.renderAFC(), "T5") {
		t.Fatal("optimistic update")
	}
	m = afcStatus(t, m, map[string]any{"AFC_lane E3": map[string]any{"map": "T5", "load": true, "prep": true}})
	if body := m.renderAFC(); !strings.Contains(body, "T5") || !strings.Contains(body, "Ready") {
		t.Fatalf("delta not merged:\n%s", body)
	}
}

func TestAFCLayoutAndSetup(t *testing.T) {
	m, _ := afcReady(t)
	l := m.layout
	if l.w(focusAFC)+l.w(focusExtruder)+l.w(focusConsole) != 160-6 || l.h(focusAFC) == 0 || l.h(focusExtruder) != l.h(focusAFC) {
		t.Fatalf("bottom row: afc %d extruder %d console %d h %d", l.w(focusAFC), l.w(focusExtruder), l.w(focusConsole), l.h(focusAFC))
	}
	// Tab order: top panes, then AFC, Extruder, console.
	m.setFocus(focusTable)
	m = press(t, m, "tab")
	if m.focus != focusAFC {
		t.Fatalf("tab after Temperatures → %v", m.focus)
	}
	if m = press(t, m, "tab"); m.focus != focusExtruder {
		t.Fatalf("tab after AFC → %v", m.focus)
	}
	m.cfgPath = t.TempDir() + "/config.toml"
	m = openCategory(t, m, "Panes")
	if !strings.Contains(m.View(), "6 AFC") {
		t.Fatal("AFC missing from setup Panes")
	}
	m = press(t, m, "down", "down", "down", "down", "space") // AFC is after the 4 top panes
	if !m.cfg.Panes.PaneHidden("AFC") {
		t.Fatalf("AFC not hidden: %v", m.cfg.Panes.Hidden)
	}
	m = press(t, m, "esc", "esc")
	if strings.Contains(m.View(), "6 AFC") || m.layout.w(focusExtruder)+m.layout.w(focusConsole) != 160-6 {
		t.Fatal("hidden AFC still takes space")
	}
}
