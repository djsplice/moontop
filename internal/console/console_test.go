package console_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justinh-rahb/moontop/internal/console"
	"github.com/justinh-rahb/moontop/internal/moonraker"
	"github.com/justinh-rahb/moontop/internal/moonraker/moonrakertest"
)

func TestClassifyLine(t *testing.T) {
	cases := map[string]console.Kind{
		"!! Move out of range: 0 0 -5": console.KindError,
		"// Klipper state: Ready":      console.KindInfo,
		">>> G28":                      console.KindCommand,
		"** reconnected (attempt 1)":   console.KindNotice,
		"ok":                           console.KindNormal,
		"B:60.0 /60.0 T0:210.0 /210.0": console.KindNormal,
	}
	for in, want := range cases {
		if got := console.ClassifyLine(in); got != want {
			t.Errorf("ClassifyLine(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBackfillLines(t *testing.T) {
	got := console.BackfillLines([]moonraker.GcodeStoreEntry{
		{Message: "G28", Type: "command"},
		{Message: "// line one\n// line two", Type: "response"},
		// A Moontop jog, as Moonraker stores it (seen on a Qidi Q2).
		{Message: "SAVE_GCODE_STATE NAME=_ui_movement\nG91\nG1 Z-10 F600\nRESTORE_GCODE_STATE NAME=_ui_movement", Type: "command"},
		// Someone else's multi-line script keeps every line, each marked.
		{Message: "G28\nG1 X10\nG1 Y10\nM400", Type: "command"},
	})
	want := []string{">>> G28", "// line one", "// line two", ">>> G91 · G1 Z-10 F600",
		">>> G28", ">>> G1 X10", ">>> G1 Y10", ">>> M400"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestErrorLine(t *testing.T) {
	cases := map[string]string{
		`JSON-RPC error 400: Unknown command:"FOO"`:   `!! Unknown command:"FOO"`,
		"JSON-RPC error 400: !! Must home axis first": "!! Must home axis first",
		"write: broken pipe":                          "!! write: broken pipe",
	}
	for in, want := range cases {
		if got := console.ErrorLine(errors.New(in)); got != want {
			t.Errorf("ErrorLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHistoryRecallAndDraft(t *testing.T) {
	h := console.NewHistory(0)
	if _, ok := h.Up("x"); ok {
		t.Fatal("Up on empty history should be a no-op")
	}
	h.Add("G28")
	h.Add("M115")
	h.Add("M115") // immediate repeat skipped
	h.Add("   ")  // blank skipped
	if got := h.Entries(); strings.Join(got, ",") != "G28,M115" {
		t.Fatalf("entries = %q", got)
	}

	steps := []struct {
		up   bool
		want string
		ok   bool
	}{
		{true, "M115", true},
		{true, "G28", true},
		{true, "", false}, // oldest: stay put
		{false, "M115", true},
		{false, "half-typ", true}, // past newest: draft restored
		{false, "", false},        // not recalling
	}
	for i, s := range steps {
		var got string
		var ok bool
		if s.up {
			got, ok = h.Up("half-typ")
		} else {
			got, ok = h.Down()
		}
		if got != s.want || ok != s.ok {
			t.Fatalf("step %d: got (%q,%v), want (%q,%v)", i, got, ok, s.want, s.ok)
		}
	}
}

func TestHistoryCapAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "console_history")
	h := console.NewHistory(3)
	for _, c := range []string{"A", "B", "C", "D"} {
		h.Add(c)
	}
	if got := strings.Join(h.Entries(), ","); got != "B,C,D" {
		t.Fatalf("capped entries = %s", got)
	}
	if err := h.Save(path); err != nil {
		t.Fatal(err)
	}
	h2 := console.NewHistory(2)
	if err := h2.Load(path); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h2.Entries(), ","); got != "C,D" {
		t.Fatalf("loaded = %s", got)
	}
	if v, _ := h2.Up(""); v != "D" {
		t.Fatalf("Up after Load = %q", v)
	}
	if err := console.NewHistory(0).Load(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
}

func TestDefaultHistoryPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x/state")
	p, err := console.DefaultHistoryPath()
	if err != nil || p != "/x/state/moontop/console_history" {
		t.Fatalf("got %q, %v", p, err)
	}
}

// fakeKlipper answers printer.gcode.script like Moonraker does: output
// as notify_gcode_response first, then the RPC reply.
func fakeKlipper(t *testing.T) *moonrakertest.Server {
	srv := moonrakertest.New(t)
	srv.Handle("printer.gcode.script", func(raw json.RawMessage) (any, error) {
		var p struct {
			Script string `json:"script"`
		}
		_ = json.Unmarshal(raw, &p)
		switch p.Script {
		case "M115":
			srv.Notify(moonraker.MethodGcodeResponse, "// FIRMWARE_NAME:Klipper\n// FIRMWARE_VERSION:v0.13")
			return "ok", nil
		case "SLOW":
			srv.Notify(moonraker.MethodGcodeResponse, "// step 1")
			time.Sleep(30 * time.Millisecond)
			srv.Notify(moonraker.MethodGcodeResponse, "// step 2")
			return "ok", nil
		case "ECHOERR":
			srv.Notify(moonraker.MethodGcodeResponse, "!! Must home axis first")
			return nil, errors.New("!! Must home axis first")
		case "BAD":
			return nil, errors.New(`Unknown command:"BAD"`)
		}
		return "ok", nil
	})
	return srv
}

func newRunner(t *testing.T, srv *moonrakertest.Server) (*console.Runner, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	c, err := moonraker.New(srv.Host())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	var out, errb bytes.Buffer
	return &console.Runner{Client: c, Out: &out, Err: &errb}, &out, &errb
}

func TestRunnerCollectsOutputPerCommand(t *testing.T) {
	srv := fakeKlipper(t)
	r, out, errb := newRunner(t, srv)
	r.Echo = true
	if failed := r.RunAll([]string{"M115", "SLOW"}); failed != 0 {
		t.Fatalf("failed = %d, stderr %q", failed, errb.String())
	}
	want := ">>> M115\n// FIRMWARE_NAME:Klipper\n// FIRMWARE_VERSION:v0.13\n>>> SLOW\n// step 1\n// step 2\n"
	if out.String() != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", out.String(), want)
	}
}

func TestRunnerStopsAtFirstErrorUnlessKeepGoing(t *testing.T) {
	srv := fakeKlipper(t)
	r, _, errb := newRunner(t, srv)
	if failed := r.RunAll([]string{"BAD", "M115"}); failed != 1 {
		t.Fatalf("failed = %d", failed)
	}
	if errb.String() != "!! Unknown command:\"BAD\"\n" {
		t.Fatalf("stderr = %q", errb.String())
	}
	if n := len(srv.CallsTo("printer.gcode.script")); n != 1 {
		t.Fatalf("sent %d commands after a failure without KeepGoing", n)
	}

	r.KeepGoing = true
	if failed := r.RunAll([]string{"BAD", "M115", "BAD"}); failed != 2 {
		t.Fatalf("keep-going failed = %d", failed)
	}
}

func TestRunnerDoesNotDuplicateEchoedError(t *testing.T) {
	srv := fakeKlipper(t)
	r, out, errb := newRunner(t, srv)
	if r.Run("ECHOERR") {
		t.Fatal("expected failure")
	}
	if out.String() != "!! Must home axis first\n" || errb.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", out.String(), errb.String())
	}
}

func TestRunnerReaderSkipsBlanksAndComments(t *testing.T) {
	srv := fakeKlipper(t)
	r, _, _ := newRunner(t, srv)
	in := strings.NewReader("; preamble\n\nG28\n  M115  \n;G1 X10\nG1 X5 ; inline stays\n")
	failed, err := r.RunReader(in)
	if failed != 0 || err != nil {
		t.Fatalf("failed=%d err=%v", failed, err)
	}
	var sent []string
	for _, c := range srv.CallsTo("printer.gcode.script") {
		var p struct {
			Script string `json:"script"`
		}
		_ = json.Unmarshal(c.Params, &p)
		sent = append(sent, p.Script)
	}
	if strings.Join(sent, "|") != "G28|M115|G1 X5 ; inline stays" {
		t.Fatalf("sent %q", sent)
	}
}

func TestRunnerStyle(t *testing.T) {
	srv := fakeKlipper(t)
	r, out, _ := newRunner(t, srv)
	r.Style = func(k console.Kind, l string) string {
		if k == console.KindInfo {
			return "[info]" + l
		}
		return l
	}
	r.Run("M115")
	if !strings.HasPrefix(out.String(), "[info]// FIRMWARE_NAME") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestDefaultFilters(t *testing.T) {
	hidden := []string{
		"B:26.3 /0.0 T0:168.1 /140.0",
		"ok B:26.3 /0.0 T0:150.0 /140.0",
		"T0:210.0 /210.0 T1:25.0 /0.0 B:60.0 /60.0",
		"C:30.1 /0.0",
		"// Run Current: 0.80A Hold Current: 0.80A",
		"Run current set to 1.20A",
	}
	shown := []string{
		"echo: Nozzle cleared",
		"!! B:bad",
		">>> M105",
		"** reconnected",
		"T0 selected",
		"// Klipper state: Ready",
		"Bed mesh loaded",
	}
	for _, l := range hidden {
		if !console.DefaultFilters.Hides(l) {
			t.Errorf("should hide %q", l)
		}
	}
	for _, l := range shown {
		if console.DefaultFilters.Hides(l) {
			t.Errorf("should show %q", l)
		}
	}
}

func TestRunnerFilter(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.Handle("printer.gcode.script", func(json.RawMessage) (any, error) {
		srv.Notify(moonraker.MethodGcodeResponse, "B:26.3 /0.0 T0:168.1 /140.0")
		srv.Notify(moonraker.MethodGcodeResponse, "echo: done")
		return "ok", nil
	})
	r, out, _ := newRunner(t, srv)
	r.Filter = console.DefaultFilters
	r.Run("M109 S140")
	if out.String() != "echo: done\n" || r.Hidden != 1 {
		t.Fatalf("stdout %q hidden %d", out.String(), r.Hidden)
	}
}
