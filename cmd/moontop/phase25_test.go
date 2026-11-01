package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/macros"
	"github.com/justinh-rahb/moontop/internal/moonraker/moonrakertest"
)

const klippyLog = `Starting Klippy...
Args: ['/home/pi/klipper/klippy/klippy.py']
Stats 100.0: gcodein=0 mcu: mcu_awake=0.002
WARNING: something odd
Traceback (most recent call last):
  File "/home/pi/klipper/klippy/klippy.py", line 130, in _connect
Error: MCU 'mcu' shutdown: Timer too close
Stats 101.0: gcodein=0 mcu: mcu_awake=0.002
`

func filesPrinter(t *testing.T) (*moonrakertest.Server, *uploads) {
	t.Helper()
	srv := fakePrinter(t)
	srv.Handle("server.files.list", func(raw json.RawMessage) (any, error) {
		var p struct{ Root string }
		_ = json.Unmarshal(raw, &p)
		if p.Root == "logs" {
			return []any{map[string]any{"path": "klippy.log.2026-10-30"}, map[string]any{"path": "moonraker.log"}, map[string]any{"path": "klippy.log"}}, nil
		}
		return []any{map[string]any{"path": "macros.cfg"}, map[string]any{"path": "printer.cfg"}, map[string]any{"path": "moonraker.conf"}}, nil
	})
	srv.HandleHTTP("/server/files/logs/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, klippyLog) // a 200 for the whole file: the client trims
	})
	srv.HandleHTTP("/server/files/config/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "# my printer\n[printer]\nkinematics: corexy\nmax_velocity: 300\n")
	})
	up := &uploads{}
	srv.HandleHTTP("/server/files/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		data, _ := io.ReadAll(f)
		up.add(r.FormValue("root"), h.Filename, string(data))
		w.WriteHeader(http.StatusCreated)
	})
	srv.Handle("printer.query_endstops.status", moonrakertest.Result(map[string]string{"y": "TRIGGERED", "x": "open", "z": "open"}))
	return srv, up
}

type uploads struct {
	mu  sync.Mutex
	got []string
}

func (u *uploads) add(root, name, data string) {
	u.mu.Lock()
	u.got = append(u.got, root+"/"+name+":"+data)
	u.mu.Unlock()
}

// run feeds a Cmd's message (and any it chains) back through Update.
func runChain(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	for i := 0; cmd != nil && i < 5; i++ {
		msg := cmd()
		if _, isBatch := msg.(tea.BatchMsg); isBatch {
			for _, c := range msg.(tea.BatchMsg) {
				if c != nil {
					if mm, ok := c().(logTextMsg); ok {
						m, _ = updateCmd(t, m, mm)
					}
				}
			}
			return m
		}
		m, cmd = updateCmd(t, m, msg)
	}
	return m
}

func TestLogs(t *testing.T) {
	srv, _ := filesPrinter(t)
	m := readyModel(t, srv)
	m, cmd := updateCmd(t, m, keyRunes("L"))
	m = runChain(t, m, cmd)
	if strings.Join(m.logs.names, ",") != "klippy.log,moonraker.log,klippy.log.2026-10-30" {
		t.Fatalf("order %v", m.logs.names)
	}
	v := stripANSI(m.View())
	for _, s := range []string{"Logs", "klippy.log", "(1 of 3", "Starting Klippy", "Timer too close", "8 lines"} {
		if !strings.Contains(v, s) {
			t.Errorf("logs view lacks %q:\n%s", s, v)
		}
	}
	for line, want := range map[string]string{
		"Error: MCU 'mcu' shutdown": "error", `  File "x.py", line 1`: "error", "Traceback (most recent call last):": "error",
		"WARNING: odd": "warning", "Stats 101.0: gcodein=0": "stats", "Starting Klippy...": "",
	} {
		if got := logLineKind(line); got != want {
			t.Errorf("logLineKind(%q) = %q, want %q", line, got, want)
		}
	}
	// / filters (on the prompt line); esc clears.
	m = update(t, m, keyRunes("/"))
	m.lineEdit.input.SetValue("stats")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if n := len(m.shownLogLines()); n != 2 || !strings.Contains(stripANSI(m.View()), `containing "stats"`) {
		t.Fatalf("filter: %d lines", n)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.logs.filter != "" || !m.logs.open {
		t.Fatal("esc should clear the filter first")
	}
	// f follows: a tick re-reads while on.
	m, cmd = updateCmd(t, m, keyRunes("f"))
	if !m.logs.follow || cmd == nil || m.onLogTick() == nil {
		t.Fatal("follow didn't start")
	}
	m = update(t, m, keyRunes("f"))
	if m.onLogTick() != nil {
		t.Fatal("tick after follow off")
	}
	// R rolls over klippy.log → application klipper.
	m = update(t, m, keyRunes("R"))
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "klipper") {
		t.Fatal("no rollover confirmation")
	}
	m, cmd = updateCmd(t, m, keyRunes("y"))
	cmd()
	var p struct{ Application string }
	_ = json.Unmarshal(srv.CallsTo("server.logs.rollover")[0].Params, &p)
	if p.Application != "klipper" {
		t.Fatalf("rolled over %q", p.Application)
	}
	// ←→ switches log.
	m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.currentLog() != "moonraker.log" || cmd == nil {
		t.Fatal("→ didn't switch")
	}
}

func TestConfigViewAndEdit(t *testing.T) {
	srv, up := filesPrinter(t)
	m := readyModel(t, srv)
	m, cmd := updateCmd(t, m, keyRunes("E"))
	m = runChain(t, m, cmd)
	if strings.Join(m.cfgFiles.files, ",") != "printer.cfg,macros.cfg,moonraker.conf" {
		t.Fatalf("files %v", m.cfgFiles.files)
	}
	m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = runChain(t, m, cmd)
	v := stripANSI(m.View())
	if !strings.Contains(v, "printer.cfg") || !strings.Contains(v, "   3 kinematics: corexy") || !strings.Contains(v, "e edit in $EDITOR") {
		t.Fatalf("view:\n%s", v)
	}
	// e: download to a temp file, hand it to the editor...
	msg := m.editCfgCmd("printer.cfg")()
	start, ok := msg.(startEditorMsg)
	if !ok || filepath.Base(start.tmp) != "printer.cfg" {
		t.Fatalf("edit returned %T %+v", msg, msg)
	}
	// ...unchanged: nothing uploaded.
	if cmd = m.onEditorDone(editorDoneMsg{path: "printer.cfg", tmp: start.tmp, before: start.before}); cmd != nil || !logContains(m, "no changes") {
		t.Fatal("unchanged file uploaded")
	}
	// Changed: uploaded, then Klipper's restart offered.
	start = m.editCfgCmd("printer.cfg")().(startEditorMsg)
	if err := os.WriteFile(start.tmp, append(start.before, "square_corner_velocity: 8\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = m.onEditorDone(editorDoneMsg{path: "printer.cfg", tmp: start.tmp, before: start.before})
	m = update(t, m, cmd())
	if len(up.got) != 1 || !strings.HasPrefix(up.got[0], "config/printer.cfg:# my printer") || !strings.Contains(up.got[0], "square_corner_velocity: 8") {
		t.Fatalf("uploaded %v", up.got)
	}
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "Restart Klipper (RESTART)") {
		t.Fatalf("no restart offer: %+v", m.confirmation)
	}
	if _, err := os.Stat(start.tmp); !os.IsNotExist(err) {
		t.Error("temp file left behind")
	}
	// The editor is $VISUAL, then $EDITOR, then vi.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "code -w")
	if c := editorCommand("/tmp/x.cfg"); strings.Join(c.Args, " ") != "code -w /tmp/x.cfg" {
		t.Fatalf("editor %v", c.Args)
	}
}

func TestConsoleCompletionAndHints(t *testing.T) {
	m := readyModel(t, fakePrinter(t))
	m = update(t, m, gcodeHelpMsg{commands: []string{"PRINT_START", "PRINT_END", "G28", "BED_MESH_CALIBRATE"}})
	m.macros = []macros.Macro{{Name: "PRINT_START", Params: []macros.Param{{Name: "BED", Default: "60"}, {Name: "EXTRUDER", Default: "210"}}}}
	m.setFocus(focusConsole)
	for _, r := range "print_s" {
		m = update(t, m, keyRunes(string(r)))
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != focusConsole || m.input.Value() != "PRINT_START" {
		t.Fatalf("tab: focus %v value %q", m.focus, m.input.Value())
	}
	if h := m.consoleHint(); h != "BED=60 EXTRUDER=210" {
		t.Fatalf("hint %q", h)
	}
	m.input.SetValue("PRINT_START BED=65 ")
	if h := m.consoleHint(); h != "EXTRUDER=210" {
		t.Fatalf("hint after BED %q", h)
	}
	if !strings.Contains(stripANSI(m.View()), "5 Console · EXTRUDER=210") {
		t.Fatal("hint not shown in the console title")
	}
	// Nothing to complete: tab moves on.
	m.input.SetValue("")
	if m = update(t, m, tea.KeyMsg{Type: tea.KeyTab}); m.focus == focusConsole {
		t.Fatal("tab with nothing to complete stayed in the console")
	}
}

func TestEndstops(t *testing.T) {
	srv, _ := filesPrinter(t)
	m := readyModel(t, srv)
	m = update(t, m, keyRunes("2"))
	m, cmd := updateCmd(t, m, keyRunes("e"))
	m = update(t, m, cmd())
	if !logContains(m, "// endstops: x open · y TRIGGERED · z open") {
		t.Fatalf("log %v", m.logLines)
	}
}
