package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker/moonrakertest"
)

const wait = 2 * time.Second

// fakePrinter returns a fake Moonraker with a small ready printer.
func fakePrinter(t *testing.T) *moonrakertest.Server {
	srv := moonrakertest.New(t)
	srv.Handle("printer.objects.list", moonrakertest.Result(map[string]any{
		"objects": []string{"webhooks", "toolhead", "extruder", "heater_bed", "print_stats", "virtual_sdcard"},
	}))
	srv.Handle("printer.objects.subscribe", moonrakertest.Result(map[string]any{
		"eventtime": 1.0,
		"status": map[string]any{
			"webhooks":    map[string]any{"state": "ready", "state_message": "Printer is ready"},
			"extruder":    map[string]any{"temperature": 25.0, "target": 0.0},
			"heater_bed":  map[string]any{"temperature": 24.0, "target": 0.0},
			"toolhead":    map[string]any{"position": []any{1.0, 2.0, 3.0, 0.0}},
			"print_stats": map[string]any{"state": "standby"},
		},
	}))
	return srv
}

func newTestModel(t *testing.T, srv *moonrakertest.Server) model {
	t.Helper()
	c, err := moonraker.New(srv.Host())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	m := initialModel(c)
	m = update(t, m, tea.WindowSizeMsg{Width: 160, Height: 50})
	return m
}

// readyModel runs initialize against the fake and applies the result.
func readyModel(t *testing.T, srv *moonrakertest.Server) model {
	t.Helper()
	m := newTestModel(t, srv)
	msg := m.initialize()()
	if _, ok := msg.(initMsg); !ok {
		t.Fatalf("initialize returned %T, want initMsg", msg)
	}
	m = update(t, m, msg)
	if !m.ready {
		t.Fatal("model not ready after initMsg")
	}
	return m
}

func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	out, _ := m.Update(msg)
	return out.(model)
}

func updateCmd(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	out, cmd := m.Update(msg)
	return out.(model), cmd
}

func rawParams(t *testing.T, vs ...any) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	for _, v := range vs {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func logContains(m model, sub string) bool {
	for _, l := range m.logLines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// --- initialize ------------------------------------------------------------

func TestInitializeIdentifiesThenSubscribesWithWebhooks(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)

	calls := srv.Calls()
	if len(calls) < 3 || calls[0].Method != "server.connection.identify" || calls[1].Method != "server.info" {
		t.Fatalf("call order = %v", methods(calls))
	}
	sub := srv.CallsTo("printer.objects.subscribe")
	if len(sub) != 1 {
		t.Fatalf("subscribe calls = %d", len(sub))
	}
	var params struct {
		Objects map[string]any `json:"objects"`
	}
	_ = json.Unmarshal(sub[0].Params, &params)
	for _, want := range []string{"webhooks", "toolhead", "print_stats", "virtual_sdcard", "extruder", "heater_bed"} {
		if _, ok := params.Objects[want]; !ok {
			t.Errorf("subscribe map missing %q", want)
		}
	}
	if m.klippy.State != "ready" || m.klippy.bannerVisible() {
		t.Fatalf("klippy = %+v", m.klippy)
	}
	if m.position != [3]float64{1, 2, 3} {
		t.Fatalf("position = %v", m.position)
	}
}

func TestInitializeStopsAtKlippyNotReady(t *testing.T) {
	srv := fakePrinter(t)
	srv.Handle("server.info", moonrakertest.Result(map[string]any{
		"klippy_connected": true, "klippy_state": "error",
	}))
	srv.Handle("printer.info", moonrakertest.Result(map[string]any{
		"state": "error", "state_message": "Section 'foo' is not valid\nOnce fixed, RESTART",
	}))
	m := newTestModel(t, srv)

	msg := m.initialize()()
	ks, ok := msg.(klippyStateMsg)
	if !ok {
		t.Fatalf("initialize returned %T, want klippyStateMsg", msg)
	}
	if ks.state != "error" || !strings.HasPrefix(ks.message, "Section 'foo'") {
		t.Fatalf("klippyStateMsg = %+v", ks)
	}
	if n := len(srv.CallsTo("printer.objects.subscribe")); n != 0 {
		t.Fatalf("subscribed %d times while Klippy not ready", n)
	}

	m = update(t, m, msg)
	if m.ready {
		t.Fatal("should not be ready")
	}
	view := m.View()
	for _, want := range []string{"Klipper error", "Section 'foo'", "K: restart", "F: firmware restart"} {
		if !strings.Contains(view, want) {
			t.Errorf("not-ready view missing %q", want)
		}
	}
	// The full multi-line reason lands in the console once.
	if !logContains(m, "Once fixed, RESTART") {
		t.Error("full state message not logged")
	}
}

func TestInitializeKlippyHostDisconnected(t *testing.T) {
	srv := fakePrinter(t)
	srv.Handle("server.info", moonrakertest.Result(map[string]any{
		"klippy_connected": false, "klippy_state": "disconnected",
	}))
	m := newTestModel(t, srv)
	ks, ok := m.initialize()().(klippyStateMsg)
	if !ok || ks.state != "disconnected" {
		t.Fatalf("got %+v", ks)
	}
	if n := len(srv.CallsTo("printer.info")); n != 0 {
		t.Fatalf("printer.info called %d times while Klippy disconnected", n)
	}
}

func TestInitErrorIsNotFatal(t *testing.T) {
	srv := fakePrinter(t)
	srv.Handle("printer.objects.list", moonrakertest.Result(map[string]any{"objects": []string{"toolhead"}}))
	m := newTestModel(t, srv)
	m = update(t, m, m.initialize()())
	if m.err != nil {
		t.Fatalf("init error became fatal: %v", m.err)
	}
	if m.initErr == nil || !strings.Contains(m.View(), "init:") {
		t.Fatal("init error not shown on the not-ready screen")
	}
}

// --- notification routing --------------------------------------------------

func TestKlippyReadyNotificationReinitializes(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)

	// FIRMWARE_RESTART: Klippy drops away…
	m = update(t, m, notificationMsg{Method: moonraker.MethodKlippyDisconnected})
	if !m.klippy.bannerVisible() || m.klippy.State != "disconnected" {
		t.Fatalf("klippy = %+v", m.klippy)
	}
	// …and comes back. The handler must re-run initialize.
	cmd := m.onKlippyReady(nil)
	if cmd == nil {
		t.Fatal("klippy_ready produced no Cmd")
	}
	msg := cmd()
	if _, ok := msg.(initMsg); !ok {
		t.Fatalf("klippy_ready Cmd returned %T, want initMsg", msg)
	}
	if n := len(srv.CallsTo("printer.objects.subscribe")); n != 2 {
		t.Fatalf("subscribe calls = %d, want 2 (startup + after ready)", n)
	}
	m = update(t, m, msg)
	if m.klippy.bannerVisible() {
		t.Fatalf("banner still visible after ready: %+v", m.klippy)
	}
}

func TestShutdownViaWebhooksShowsBannerAndReservesRow(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	before := m.layout

	m = update(t, m, notificationMsg{
		Method: moonraker.MethodStatusUpdate,
		Params: rawParams(t, map[string]any{
			"webhooks": map[string]any{"state": "shutdown", "state_message": "Shutdown due to M112 command"},
		}, 2.0),
	})
	if m.klippy.State != "shutdown" {
		t.Fatalf("klippy = %+v", m.klippy)
	}
	if !strings.Contains(m.View(), "Klipper shutdown: Shutdown due to M112 command") {
		t.Error("banner missing from dashboard view")
	}
	if m.layout.consoleViewportH >= before.consoleViewportH && m.layout.topRowH >= before.topRowH {
		t.Errorf("layout did not give up a row for the banner: before %+v after %+v", before, m.layout)
	}
	if !logContains(m, "!! Klipper shutdown") {
		t.Error("state transition not logged")
	}
}

func TestShutdownNotificationFetchesReason(t *testing.T) {
	srv := fakePrinter(t)
	srv.Handle("printer.info", moonrakertest.Result(map[string]any{
		"state": "shutdown", "state_message": "MCU 'mcu' shutdown: Timer too close",
	}))
	m := readyModel(t, srv)

	cmd := m.onKlippyShutdown(nil)
	if m.klippy.State != "shutdown" {
		t.Fatalf("state not set immediately: %+v", m.klippy)
	}
	m = update(t, m, cmd())
	if m.klippy.Message != "MCU 'mcu' shutdown: Timer too close" {
		t.Fatalf("message = %q", m.klippy.Message)
	}
}

func TestEveryNotificationRearmsListener(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	for _, method := range []string{"notify_proc_stat_update", moonraker.MethodGcodeResponse} {
		params := rawParams(t, "ok")
		_, cmd := updateCmd(t, m, notificationMsg{Method: method, Params: params})
		if cmd == nil {
			t.Errorf("%s: no Cmd returned — listener not re-armed", method)
		}
	}
}

// --- keys ------------------------------------------------------------------

func TestEStopArmsFromConsoleAndOverridesPendingPrompt(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m.focus = focusConsole
	m.input.Focus()
	m.confirmation = &pendingConfirmation{prompt: "Start print: a.gcode?"}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.confirmation == nil || !m.confirmation.danger || !strings.Contains(m.confirmation.prompt, "EMERGENCY STOP") {
		t.Fatalf("confirmation = %+v", m.confirmation)
	}

	m, cmd := updateCmd(t, m, keyRunes("y"))
	if cmd == nil {
		t.Fatal("y produced no Cmd")
	}
	if res, ok := cmd().(klippyActionMsg); !ok || res.err != nil {
		t.Fatalf("e-stop result = %+v", res)
	}
	srv.WaitForCall("printer.emergency_stop", 1, wait)
	if m.confirmation != nil {
		t.Fatal("confirmation not cleared")
	}
}

func TestEStopNoSendsOnNo(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	m = update(t, m, keyRunes("n"))
	if m.confirmation != nil {
		t.Fatal("prompt still armed")
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(srv.CallsTo("printer.emergency_stop")); n != 0 {
		t.Fatalf("emergency_stop sent %d times after n", n)
	}
}

func TestRestartKeysAreConfirmGatedAndNotHijackedInConsole(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)

	m.focus = focusConsole
	m.input.Focus()
	m = update(t, m, keyRunes("K"))
	if m.confirmation != nil {
		t.Fatal("K in console armed a restart")
	}
	if m.input.Value() != "K" {
		t.Fatalf("console input = %q", m.input.Value())
	}

	m.focus = focusFiles
	m.input.Blur()
	for key, method := range map[string]string{"K": "printer.restart", "F": "printer.firmware_restart"} {
		m = update(t, m, keyRunes(key))
		if m.confirmation == nil {
			t.Fatalf("%s did not arm a confirmation", key)
		}
		if m.confirmation.danger {
			t.Errorf("%s: danger styling while idle", key)
		}
		var cmd tea.Cmd
		m, cmd = updateCmd(t, m, keyRunes("y"))
		cmd()
		srv.WaitForCall(method, 1, wait)
	}
}

func TestRestartIsDangerStyledDuringPrint(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m.job.State = "printing"
	m = update(t, m, keyRunes("F"))
	if m.confirmation == nil || !m.confirmation.danger {
		t.Fatalf("confirmation = %+v", m.confirmation)
	}
}

func TestRestartKeysWorkBeforeReady(t *testing.T) {
	srv := fakePrinter(t)
	m := newTestModel(t, srv)
	m = update(t, m, klippyStateMsg{state: "error", message: "bad config"})
	m = update(t, m, keyRunes("F"))
	if m.confirmation == nil {
		t.Fatal("F did not arm on the not-ready screen")
	}
	if !strings.Contains(m.View(), "Firmware restart") {
		t.Error("prompt not rendered on the not-ready screen")
	}
}

// --- reconnect -------------------------------------------------------------

func TestReconnectResetsKlippyAndReinitializes(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m.connState = stateReconnecting

	m, cmd := updateCmd(t, m, reconnectResultMsg{attempt: 1})
	if m.connState != stateConnected || m.klippy != (klippyStatus{}) {
		t.Fatalf("connState=%v klippy=%+v", m.connState, m.klippy)
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("reconnect Cmd = %T (len %d), want disconnect watcher + initialize", batch, len(batch))
	}
}

// --- console ---------------------------------------------------------------

func TestGcodeStoreBackfillPrependsBeforeLiveLines(t *testing.T) {
	srv := fakePrinter(t)
	m := newTestModel(t, srv)
	m.appendLog("live line")
	m = update(t, m, gcodeStoreMsg{entries: []moonraker.GcodeStoreEntry{
		{Message: "G28", Type: "command"},
		{Message: "ok", Type: "response"},
	}})
	want := []string{">>> G28", "ok", "** ── live ──", "live line"}
	if strings.Join(m.logLines, "|") != strings.Join(want, "|") {
		t.Fatalf("logLines = %q", m.logLines)
	}
}

func TestComputeLayoutReservedRows(t *testing.T) {
	a := computeLayout(160, 50, 4, 0, nil)
	b := computeLayout(160, 50, 4, 1, nil)
	if a.consoleViewportH+a.topRowH-(b.consoleViewportH+b.topRowH) != 1 {
		t.Fatalf("reserving 1 row changed pane rows by %d", a.consoleViewportH+a.topRowH-(b.consoleViewportH+b.topRowH))
	}
}

func methods(calls []moonrakertest.Call) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Method
	}
	return out
}
