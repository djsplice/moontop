package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/console"
	"github.com/justinh-rahb/moontop/internal/moonraker"
	"github.com/justinh-rahb/moontop/internal/moonraker/moonrakertest"
)

func newTestRepl(t *testing.T, srv *moonrakertest.Server) replModel {
	t.Helper()
	c, err := moonraker.New(srv.Host())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	path := filepath.Join(t.TempDir(), "hist")
	m := newReplModel(c, srv.Host(), console.NewHistory(0), path)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return out.(replModel)
}

func replUpdate(t *testing.T, m replModel, msg tea.Msg) (replModel, tea.Cmd) {
	t.Helper()
	out, cmd := m.Update(msg)
	return out.(replModel), cmd
}

// collect runs cmd and every Cmd nested in batches/sequences, returning
// the leaf messages. Only use it on Cmds that don't include a blocking
// listener.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
		var out []tea.Msg
		for i := 0; i < v.Len(); i++ {
			out = append(out, collect(v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// printed returns the text of every tea.Println among msgs, ANSI intact.
func printed(msgs []tea.Msg) string {
	var b strings.Builder
	for _, m := range msgs {
		if t := reflect.TypeOf(m); t != nil && t.String() == "tea.printLineMessage" {
			b.WriteString(fmt.Sprint(m))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func TestReplSubmitEchoesSendsAndSavesHistory(t *testing.T) {
	srv := moonrakertest.New(t)
	m := newTestRepl(t, srv)
	m.input.SetValue("  G28  ")
	m, cmd := replUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.inflight != 1 || m.running != "G28" || m.input.Value() != "" {
		t.Fatalf("inflight=%d running=%q input=%q", m.inflight, m.running, m.input.Value())
	}
	msgs := collect(cmd)
	if !strings.Contains(printed(msgs), ">>> G28") {
		t.Fatalf("no echo in %q", printed(msgs))
	}
	srv.WaitForCall("printer.gcode.script", 1, wait)
	data, err := os.ReadFile(m.histPath)
	if err != nil || string(data) != "G28\n" {
		t.Fatalf("history file = %q, %v", data, err)
	}
	// The send result clears the running indicator.
	var sent gcodeSentMsg
	for _, msg := range msgs {
		if s, ok := msg.(gcodeSentMsg); ok {
			sent = s
		}
	}
	m, _ = replUpdate(t, m, sent)
	if m.inflight != 0 || m.running != "" {
		t.Fatalf("after reply inflight=%d running=%q", m.inflight, m.running)
	}
}

func TestReplDoesNotSendWhileDisconnected(t *testing.T) {
	srv := moonrakertest.New(t)
	m := newTestRepl(t, srv)
	m.connState = stateReconnecting
	m.input.SetValue("G28")
	_, cmd := replUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if out := printed(collect(cmd)); !strings.Contains(out, "disconnected — not sent") {
		t.Fatalf("printed %q", out)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(srv.CallsTo("printer.gcode.script")); n != 0 {
		t.Fatalf("sent %d while disconnected", n)
	}
}

func TestReplCtrlCClearsLineThenQuits(t *testing.T) {
	srv := moonrakertest.New(t)
	m := newTestRepl(t, srv)
	m.input.SetValue("G1 X1")
	m, cmd := replUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.input.Value() != "" || m.quitting || cmd != nil {
		t.Fatalf("first ctrl+c: input=%q quitting=%v", m.input.Value(), m.quitting)
	}
	m, cmd = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.quitting || cmd == nil {
		t.Fatal("second ctrl+c did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("quit Cmd is not tea.Quit")
	}
}

func TestReplCtrlDQuitsOnlyOnEmptyLine(t *testing.T) {
	srv := moonrakertest.New(t)
	m := newTestRepl(t, srv)
	m.input.SetValue("M1")
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.quitting {
		t.Fatal("ctrl+d quit with text on the line")
	}
	m.input.SetValue("")
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if !m.quitting {
		t.Fatal("ctrl+d on empty line did not quit")
	}
}

func TestReplHistoryKeys(t *testing.T) {
	srv := moonrakertest.New(t)
	m := newTestRepl(t, srv)
	m.hist.Add("G28")
	m.hist.Add("M115")
	m.input.SetValue("dra")
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Value() != "G28" {
		t.Fatalf("after 2×up: %q", m.input.Value())
	}
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.input.Value() != "dra" {
		t.Fatalf("draft not restored: %q", m.input.Value())
	}
}

func TestReplEStopConfirmFlow(t *testing.T) {
	srv := moonrakertest.New(t)
	m := newTestRepl(t, srv)
	m.input.SetValue("G1 X5")
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.confirmation == nil || !m.confirmation.danger {
		t.Fatal("ctrl+x did not arm")
	}
	if !strings.Contains(m.View(), "EMERGENCY STOP") {
		t.Fatal("prompt not shown")
	}
	// Typing other keys while armed doesn't edit the line or send.
	m, _ = replUpdate(t, m, keyRunes("x"))
	if m.input.Value() != "G1 X5" || m.confirmation == nil {
		t.Fatalf("input=%q confirmation=%v", m.input.Value(), m.confirmation)
	}
	m, cmd := replUpdate(t, m, keyRunes("y"))
	msgs := collect(cmd)
	srv.WaitForCall("printer.emergency_stop", 1, wait)
	if !strings.Contains(printed(msgs), "emergency stop sent") {
		t.Fatalf("printed %q", printed(msgs))
	}

	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.confirmation != nil {
		t.Fatal("esc did not cancel")
	}
}

func TestReplKlippyStatePrintsOnlyWhatsNew(t *testing.T) {
	srv := moonrakertest.New(t)
	m := newTestRepl(t, srv)
	m.klippy = klippyStatus{State: "ready"}

	// The shutdown notification prints the state line itself…
	print, follow := m.handleNotification(notificationMsg{Method: moonraker.MethodKlippyShutdown})
	if out := printed(collect(print)); !strings.Contains(out, "!! Klipper shutdown") {
		t.Fatalf("notification printed %q", out)
	}
	if follow == nil {
		t.Fatal("no printer.info fetch after shutdown")
	}
	// …so the printer.info answer adds only the reason.
	m, cmd := replUpdate(t, m, klippyStateMsg{state: "shutdown", message: "Shutdown due to M112 command"})
	out := printed(collect(cmd))
	if strings.Contains(out, "Klipper shutdown") || !strings.Contains(out, "!! Shutdown due to M112 command") {
		t.Fatalf("state msg printed %q", out)
	}
	// Back to ready: a notice, and completions reload.
	m, cmd = replUpdate(t, m, klippyStateMsg{state: "ready"})
	msgs := collect(cmd)
	if !strings.Contains(printed(msgs), "Klipper ready") {
		t.Fatalf("printed %q", printed(msgs))
	}
	var help bool
	for _, msg := range msgs {
		if _, ok := msg.(gcodeHelpMsg); ok {
			help = true
		}
	}
	if !help {
		t.Fatal("completions not reloaded on ready")
	}
	if m.klippy.bannerVisible() {
		t.Fatal("still not ready")
	}
}

func TestReplCompletionsFromGcodeHelp(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.Handle("printer.gcode.help", moonrakertest.Result(map[string]string{
		"bed_mesh_calibrate": "Perform Mesh Bed Leveling", "G28": "Home",
	}))
	m := newTestRepl(t, srv)
	msg := loadGcodeHelpCmd(m.client)()
	h := msg.(gcodeHelpMsg)
	if strings.Join(h.commands, ",") != "BED_MESH_CALIBRATE,G28" {
		t.Fatalf("commands = %v", h.commands)
	}
	m, _ = replUpdate(t, m, h)
	m, _ = replUpdate(t, m, keyRunes("bed"))
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !strings.EqualFold(m.input.Value(), "BED_MESH_CALIBRATE") {
		t.Fatalf("tab completed to %q", m.input.Value())
	}
}

func TestReplFiltersAndRedundancy(t *testing.T) {
	srv := moonrakertest.New(t)
	m := newTestRepl(t, srv)
	if strings.Contains(m.input.Placeholder, "ctrl+d") {
		t.Error("placeholder repeats the quit hint from the status line")
	}
	params := []json.RawMessage{json.RawMessage(`"B:26.3 /0.0 T0:168.1 /140.0\necho: Nozzle cleared"`)}
	print, _ := m.handleNotification(notificationMsg{Method: moonraker.MethodGcodeResponse, Params: params})
	out := printed(collect(print))
	if strings.Contains(out, "T0:168.1") || !strings.Contains(out, "Nozzle cleared") {
		t.Fatalf("printed %q", out)
	}
	if m.hidden != 1 || !strings.Contains(m.statusLine(), "1 hidden (ctrl+o)") {
		t.Fatalf("hidden=%d status=%q", m.hidden, m.statusLine())
	}
	m, _ = replUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	print, _ = m.handleNotification(notificationMsg{Method: moonraker.MethodGcodeResponse, Params: params})
	if !strings.Contains(printed(collect(print)), "T0:168.1") {
		t.Fatal("ctrl+o did not show filtered lines")
	}

	// The live marker no longer repeats the host.
	m2 := newTestRepl(t, srv)
	_, cmd := replUpdate(t, m2, gcodeStoreMsg{})
	if out := printed(collect(cmd)); !strings.Contains(out, "── live ──") || strings.Contains(out, srv.Host()) {
		t.Fatalf("live marker %q", out)
	}
}
