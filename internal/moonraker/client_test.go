package moonraker_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker/moonrakertest"
)

const wait = 2 * time.Second

func dial(t *testing.T, srv *moonrakertest.Server) *moonraker.Client {
	t.Helper()
	c, err := moonraker.New(srv.Host())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func nextNotification(t *testing.T, c *moonraker.Client) moonraker.Notification {
	t.Helper()
	select {
	case n := <-c.Notifications():
		return n
	case <-time.After(wait):
		t.Fatal("timed out waiting for notification")
	}
	return moonraker.Notification{}
}

func TestNotificationsForwardEveryMethodOnOneChannel(t *testing.T) {
	srv := moonrakertest.New(t)
	c := dial(t, srv)
	srv.WaitForConnections(1, wait)

	srv.Notify(moonraker.MethodGcodeResponse, "!! Move out of range")
	srv.Notify(moonraker.MethodKlippyShutdown)
	srv.Notify("notify_proc_stat_update", map[string]any{"cpu_temp": 50})

	n := nextNotification(t, c)
	if n.Method != moonraker.MethodGcodeResponse {
		t.Fatalf("first method = %q", n.Method)
	}
	line, err := moonraker.ParseGcodeResponse(n.Params)
	if err != nil || line != "!! Move out of range" {
		t.Fatalf("ParseGcodeResponse = %q, %v", line, err)
	}
	if n := nextNotification(t, c); n.Method != moonraker.MethodKlippyShutdown || len(n.Params) != 0 {
		t.Fatalf("second = %+v", n)
	}
	// Unknown methods are forwarded too — routing is the consumer's job.
	if n := nextNotification(t, c); n.Method != "notify_proc_stat_update" {
		t.Fatalf("third method = %q", n.Method)
	}
}

func TestParseStatusUpdate(t *testing.T) {
	params := []json.RawMessage{
		json.RawMessage(`{"webhooks":{"state":"shutdown","state_message":"Emergency stop"}}`),
		json.RawMessage(`1234.5`),
	}
	u, err := moonraker.ParseStatusUpdate(params)
	if err != nil {
		t.Fatal(err)
	}
	if u.Timestamp != 1234.5 || u.Objects["webhooks"]["state"] != "shutdown" {
		t.Fatalf("got %+v", u)
	}
	if _, err := moonraker.ParseStatusUpdate(nil); err == nil {
		t.Fatal("expected error for empty params")
	}
}

func TestKlippyLifecycleRPCs(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.Handle("server.info", moonrakertest.Result(map[string]any{
		"klippy_connected": true, "klippy_state": "error",
	}))
	srv.Handle("printer.info", moonrakertest.Result(map[string]any{
		"state": "error", "state_message": "Option 'foo' is not valid\nOnce the underlying issue is corrected...",
	}))
	srv.Handle("server.gcode_store", moonrakertest.Result(map[string]any{
		"gcode_store": []map[string]any{
			{"message": "G28", "time": 1.0, "type": "command"},
			{"message": "ok", "time": 1.1, "type": "response"},
		},
	}))
	c := dial(t, srv)

	si, err := c.GetServerInfo()
	if err != nil || si.KlippyState != "error" || !si.KlippyConnected {
		t.Fatalf("GetServerInfo = %+v, %v", si, err)
	}
	pi, err := c.GetPrinterInfo()
	if err != nil || pi.State != "error" || pi.StateMessage == "" {
		t.Fatalf("GetPrinterInfo = %+v, %v", pi, err)
	}
	store, err := c.GcodeStore(100)
	if err != nil || len(store) != 2 || store[0].Type != "command" {
		t.Fatalf("GcodeStore = %+v, %v", store, err)
	}
	if err := c.Identify("Moontop", "0.14.0", "other", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []func() error{c.EmergencyStop, c.RestartKlipper, c.FirmwareRestart} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []string{"printer.emergency_stop", "printer.restart", "printer.firmware_restart"} {
		if len(srv.CallsTo(m)) != 1 {
			t.Errorf("%s called %d times", m, len(srv.CallsTo(m)))
		}
	}
	var ident map[string]string
	_ = json.Unmarshal(srv.CallsTo("server.connection.identify")[0].Params, &ident)
	if ident["client_name"] != "Moontop" || ident["type"] != "other" {
		t.Fatalf("identify params = %v", ident)
	}
}

func TestRPCErrorSurfaces(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.Handle("printer.info", func(json.RawMessage) (any, error) {
		return nil, errString("Klippy Disconnected")
	})
	c := dial(t, srv)
	if _, err := c.GetPrinterInfo(); err == nil {
		t.Fatal("expected error")
	}
}

// Reconnect brings the socket back; the notification channel is the
// same one (consumers keep reading it), and no subscribe is replayed —
// the consumer's initialize path owns that.
func TestReconnectKeepsChannelAndDoesNotResubscribe(t *testing.T) {
	srv := moonrakertest.New(t)
	c := dial(t, srv)
	srv.WaitForConnections(1, wait)
	if _, err := c.Subscribe(map[string][]string{"toolhead": {"position"}}); err != nil {
		t.Fatal(err)
	}
	ch := c.Notifications()

	srv.DropConnections()
	select {
	case <-c.Disconnected():
	case <-time.After(wait):
		t.Fatal("no disconnect signal")
	}

	if err := c.Reconnect(context.Background()); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	srv.WaitForConnections(1, wait)

	srv.Notify(moonraker.MethodKlippyReady)
	select {
	case n := <-ch:
		if n.Method != moonraker.MethodKlippyReady {
			t.Fatalf("method = %q", n.Method)
		}
	case <-time.After(wait):
		t.Fatal("no notification after reconnect on the original channel")
	}
	if got := len(srv.CallsTo("printer.objects.subscribe")); got != 1 {
		t.Fatalf("subscribe calls = %d, want 1 (no replay on reconnect)", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
