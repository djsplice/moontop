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
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/moonraker"
	"github.com/justinh-rahb/moontop/internal/moonraker/moonrakertest"
)

// fmPrinter serves a gcodes root with a folder:
//
//	top.gcode       (sliced: estimate, filament, slicer)
//	sub/part.gcode
type fmPrinter struct {
	*moonrakertest.Server
	mu      sync.Mutex
	uploads []string // root|path|filename|data
}

func newFmPrinter(t *testing.T) *fmPrinter {
	t.Helper()
	p := &fmPrinter{Server: jobsPrinter(t)}
	p.Handle("server.files.list", moonrakertest.Result([]any{
		map[string]any{"path": "top.gcode", "size": 1000, "modified": 2.0},
		map[string]any{"path": "sub/part.gcode", "size": 2000, "modified": 1.0},
	}))
	p.Handle("server.files.get_directory", func(raw json.RawMessage) (any, error) {
		var q struct{ Path string }
		_ = json.Unmarshal(raw, &q)
		disk := map[string]any{"total": 32 << 30, "used": 4 << 30, "free": 28 << 30}
		switch q.Path {
		case "gcodes":
			return map[string]any{
				"dirs": []any{map[string]any{"dirname": "sub", "modified": 3.0, "size": 4096}, map[string]any{"dirname": ".thumbs"}},
				"files": []any{
					map[string]any{"filename": "top.gcode", "modified": 1.7e9, "size": 12 << 20, "slicer": "OrcaSlicer", "slicer_version": "2.2.0",
						"estimated_time": 5580.0, "filament_total": 11600.0, "filament_weight_total": 34.5, "filament_type": "PLA",
						"layer_height": 0.2, "first_layer_height": 0.25, "object_height": 42.0, "nozzle_diameter": 0.4,
						"first_layer_extr_temp": 215.0, "first_layer_bed_temp": 60.0},
					map[string]any{"filename": "plain.gcode", "modified": 1.6e9, "size": 300},
				},
				"disk_usage": disk,
			}, nil
		case "gcodes/sub":
			return map[string]any{"dirs": []any{}, "files": []any{map[string]any{"filename": "part.gcode", "modified": 1.0, "size": 2000}}, "disk_usage": disk}, nil
		}
		return nil, &rpcErr{"Directory does not exist"}
	})
	p.HandleHTTP("/server/files/upload", func(w http.ResponseWriter, r *http.Request) {
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
		p.mu.Lock()
		rec := strings.Join([]string{r.FormValue("root"), r.FormValue("path"), h.Filename, string(data)}, "|")
		printing := r.FormValue("print") == "true"
		if printing {
			rec += "|print"
		}
		p.uploads = append(p.uploads, rec)
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{
			"item": map[string]any{"path": h.Filename, "root": "gcodes"}, "action": "create_file",
			"print_started": printing, "print_queued": false}})
	})
	return p
}

type rpcErr struct{ s string }

func (e *rpcErr) Error() string { return e.s }

// fmRun runs Cmds (and batches), feeding file manager, file list and
// jobs results back through Update, and running what they return.
func fmRun(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 30; steps++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		switch msg.(type) {
		case dirLoadedMsg, fileOpMsg, filesLoadedMsg, jobsLoadedMsg, jobsActionMsg:
			var next tea.Cmd
			m, next = updateCmd(t, m, msg)
			queue = append(queue, next)
		}
	}
	return m
}

func fmKeys(t *testing.T, m model, keys ...string) model {
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
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		default:
			msg = keyRunes(k)
		}
		var cmd tea.Cmd
		m, cmd = updateCmd(t, m, msg)
		m = fmRun(t, m, cmd)
	}
	return m
}

func fmOpen(t *testing.T, p *fmPrinter, width int) model {
	t.Helper()
	m := readyModel(t, p.Server)
	m = update(t, m, tea.WindowSizeMsg{Width: width, Height: 40})
	m = fmRun(t, m, m.loadFiles())
	return m
}

func lastParams(t *testing.T, srv *moonrakertest.Server, method string) map[string]any {
	t.Helper()
	calls := srv.WaitForCall(method, 1, wait)
	var p map[string]any
	_ = json.Unmarshal(calls[len(calls)-1].Params, &p)
	return p
}

func TestFilesPaneOpensFileManagerAtTheFile(t *testing.T) {
	p := newFmPrinter(t)
	m := fmOpen(t, p, 160)
	if !strings.Contains(m.View(), "f manage") {
		t.Fatal("Files footer doesn't offer the file manager")
	}
	// Select sub/part.gcode in the pane (sorted newest first: top, then part).
	m = fmKeys(t, m, "down", "f")
	if !m.jobs.open || m.jobs.tab != jobsFiles || m.jobs.files.dir != "sub" {
		t.Fatalf("open=%v tab=%d dir=%q", m.jobs.open, m.jobs.tab, m.jobs.files.dir)
	}
	if cur, _ := m.jobs.files.selected(); cur.name != "part.gcode" {
		t.Fatalf("selected %q", cur.name)
	}
	v := m.View()
	for _, s := range []string{"Files", "gcodes/sub", "../", "part.gcode", "28.0 GB free of 32.0 GB"} {
		if !strings.Contains(v, s) {
			t.Errorf("view lacks %q:\n%s", s, v)
		}
	}
	// ← goes up and selects the folder we came from.
	m = fmKeys(t, m, "left")
	if m.jobs.files.dir != "" {
		t.Fatalf("dir = %q", m.jobs.files.dir)
	}
	if cur, _ := m.jobs.files.selected(); cur.name != "sub" {
		t.Fatalf("after up, selected %q", cur.name)
	}
}

func TestFileManagerListingAndDetails(t *testing.T) {
	p := newFmPrinter(t)
	m := fmOpen(t, p, 160)
	m = fmRun(t, m, m.openFileManager("", "top.gcode"))
	v := m.View()
	for _, s := range []string{"1 folders, 2 files", "sub/", "top.gcode", "12.0 MB", "1h33m", "11.6 m", "plain.gcode",
		"Estimate", "34.5 g", "PLA", "0.20 mm (first 0.25)", "42.00 mm", "215 / 60 °C", "OrcaSlicer 2.2.0", "never"} {
		if !strings.Contains(v, s) {
			t.Errorf("view lacks %q:\n%s", s, v)
		}
	}
	if strings.Contains(v, ".thumbs") {
		t.Error("hidden folder shown")
	}
	// Narrow: no details column, a summary line instead.
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	v = m.View()
	if strings.Contains(v, "Last print") || !strings.Contains(v, "estimate 1h33m · 11.6 m filament · 0.20 mm layers · OrcaSlicer") {
		t.Errorf("narrow view:\n%s", v)
	}
	// s cycles the sort.
	m = fmKeys(t, m, "s")
	if !strings.Contains(m.View(), "sorted by name") {
		t.Error("sort status missing")
	}
	if rows := m.jobs.files.rows(); rows[1].name != "plain.gcode" {
		t.Fatalf("by name: %v", rows[1].name)
	}
}

func TestFileManagerNavigateAndPrint(t *testing.T) {
	p := newFmPrinter(t)
	m := fmOpen(t, p, 160)
	m = fmRun(t, m, m.openFileManager("", ""))
	m = fmKeys(t, m, "enter") // into sub/
	if m.jobs.files.dir != "sub" {
		t.Fatalf("dir = %q", m.jobs.files.dir)
	}
	if got := lastParams(t, p.Server, "server.files.get_directory")["path"]; got != "gcodes/sub" {
		t.Fatalf("listed %v", got)
	}
	m = fmKeys(t, m, "down", "enter") // part.gcode
	if m.confirmation == nil || !strings.Contains(m.View(), "Start print: sub/part.gcode?") {
		t.Fatalf("no print confirmation:\n%s", m.View())
	}
	m, cmd := updateCmd(t, m, keyRunes("y"))
	cmd()
	if got := lastParams(t, p.Server, "printer.print.start")["filename"]; got != "sub/part.gcode" {
		t.Fatalf("printed %v", got)
	}
	m = fmKeys(t, m, "a")
	if got := lastParams(t, p.Server, "server.job_queue.post_job")["filenames"]; got == nil {
		t.Fatal("not queued")
	}
	m = fmKeys(t, m, "up", "enter") // ..
	if m.jobs.files.dir != "" {
		t.Fatalf("enter on .. → %q", m.jobs.files.dir)
	}
}

func TestFileManagerEdits(t *testing.T) {
	p := newFmPrinter(t)
	m := fmOpen(t, p, 160)
	m = fmRun(t, m, m.openFileManager("", "top.gcode"))

	// Rename; a / in a name is refused.
	m = fmKeys(t, m, "R", "ctrl+u", "a/b.gcode", "enter")
	if !strings.Contains(m.View(), "can't contain /") {
		t.Fatalf("slash accepted:\n%s", m.View())
	}
	m = fmKeys(t, m, "R", "ctrl+u", "renamed.gcode", "enter")
	if q := lastParams(t, p.Server, "server.files.move"); q["source"] != "gcodes/top.gcode" || q["dest"] != "gcodes/renamed.gcode" {
		t.Fatalf("rename: %v", q)
	}
	if !strings.Contains(m.View(), "renamed top.gcode → renamed.gcode") {
		t.Errorf("no status:\n%s", m.View())
	}

	// Copy offers "name copy.gcode".
	m = fmRun(t, m, m.openFileManager("", "top.gcode"))
	m = fmKeys(t, m, "C")
	if m.jobs.files.input.Value() != "top copy.gcode" {
		t.Fatalf("copy default %q", m.jobs.files.input.Value())
	}
	m = fmKeys(t, m, "enter")
	if q := lastParams(t, p.Server, "server.files.copy"); q["dest"] != "gcodes/top copy.gcode" {
		t.Fatalf("copy: %v", q)
	}

	// Move into sub/.
	m = fmRun(t, m, m.openFileManager("", "top.gcode"))
	m = fmKeys(t, m, "m", "ctrl+u", "sub", "enter")
	if q := lastParams(t, p.Server, "server.files.move"); q["dest"] != "gcodes/sub/top.gcode" {
		t.Fatalf("move: %v", q)
	}

	// New folder.
	m = fmKeys(t, m, "n", "prints", "enter")
	if q := lastParams(t, p.Server, "server.files.post_directory"); q["path"] != "gcodes/prints" {
		t.Fatalf("mkdir: %v", q)
	}

	// esc cancels an edit without closing the screen.
	m = fmKeys(t, m, "n", "esc")
	if m.jobs.files.mode != fmNone || !m.jobs.open {
		t.Fatal("esc should cancel the edit only")
	}

	// Delete a file, then a folder (with its contents), after confirmations.
	m = fmRun(t, m, m.openFileManager("", "top.gcode"))
	m = fmKeys(t, m, "x")
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "Delete top.gcode?") {
		t.Fatal("no delete confirmation")
	}
	m, cmd := updateCmd(t, m, keyRunes("y"))
	m = fmRun(t, m, cmd)
	if q := lastParams(t, p.Server, "server.files.delete_file"); q["path"] != "gcodes/top.gcode" {
		t.Fatalf("delete: %v", q)
	}
	m = fmRun(t, m, m.openFileManager("", "sub"))
	m = fmKeys(t, m, "x")
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "folder sub and everything in it") {
		t.Fatal("no folder delete confirmation")
	}
	_, cmd = updateCmd(t, m, keyRunes("y"))
	fmRun(t, m, cmd)
	if q := lastParams(t, p.Server, "server.files.delete_directory"); q["path"] != "gcodes/sub" || q["force"] != true {
		t.Fatalf("rmdir: %v", q)
	}
}

func (p *fmPrinter) uploaded() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.uploads, ";")
}

// localDir makes a folder on "this computer" for the picker:
// a.gcode (older), b.gcode (newer), notes.txt and parts/.
func localDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	write := func(name, data string, age time.Duration) {
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-age)
		_ = os.Chtimes(p, ts, ts)
	}
	write("a.gcode", "G28 ; a\n", 2*time.Hour)
	write("b.gcode", "G28 ; b\n", time.Minute)
	write("notes.txt", "hi", time.Second)
	if err := os.Mkdir(filepath.Join(d, "parts"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join("parts", "c.gcode"), "G28 ; c\n", time.Hour)
	return d
}

func TestFileManagerUploadPicker(t *testing.T) {
	p := newFmPrinter(t)
	m := fmOpen(t, p, 160)
	m = fmRun(t, m, m.openFileManager("sub", ""))
	d := localDir(t)
	lastUploadDir = d
	t.Cleanup(func() { lastUploadDir = "" })

	m = fmKeys(t, m, "u")
	pk := m.jobs.files.pick
	if !pk.open || pk.print {
		t.Fatal("u didn't open the picker")
	}
	var names []string
	for _, e := range pk.entries {
		names = append(names, e.name)
	}
	if strings.Join(names, ",") != "parts,b.gcode,a.gcode" {
		t.Fatalf("entries = %v (folders, then gcode newest first; no .txt)", names)
	}
	v := m.View()
	for _, s := range []string{"Upload from this computer", tildePath(d), "parts", "b.gcode", "gcode files", ": type a path"} {
		if !strings.Contains(v, s) {
			t.Errorf("picker lacks %q:\n%s", s, v)
		}
	}
	m = fmKeys(t, m, ".")
	if len(m.jobs.files.pick.entries) != 4 {
		t.Fatalf(". should show all files: %d", len(m.jobs.files.pick.entries))
	}
	m = fmKeys(t, m, ".", "enter") // into parts/
	if !strings.HasSuffix(m.jobs.files.pick.dir, "parts") {
		t.Fatalf("dir = %q", m.jobs.files.pick.dir)
	}
	m = fmKeys(t, m, "left") // back up, parts selected
	if m.jobs.files.pick.entries[m.jobs.files.pick.cursor].name != "parts" {
		t.Fatal("up should select the folder we came from")
	}
	m = fmKeys(t, m, "down", "enter") // b.gcode
	if m.jobs.files.pick.open {
		t.Fatal("picker still open")
	}
	if got := p.uploaded(); got != "gcodes|sub|b.gcode|G28 ; b\n" {
		t.Fatalf("uploads = %q", got)
	}
	if !strings.Contains(m.View(), "uploaded b.gcode") {
		t.Errorf("no status:\n%s", m.View())
	}
	// The picker remembers the folder; esc cancels it.
	m = fmKeys(t, m, "u")
	if m.jobs.files.pick.dir != d {
		t.Fatalf("picker started in %q", m.jobs.files.pick.dir)
	}
	m = fmKeys(t, m, "esc")
	if m.jobs.files.pick.open || !m.jobs.open {
		t.Fatal("esc should close only the picker")
	}
	// ":" types a path instead.
	m = fmKeys(t, m, "u", ":")
	if m.jobs.files.mode != fmUpload || !strings.HasPrefix(m.jobs.files.input.Value(), d) {
		t.Fatalf("typed path: mode %d, %q", m.jobs.files.mode, m.jobs.files.input.Value())
	}
	m = fmKeys(t, m, "ctrl+u", filepath.Join(d, "missing.gcode"), "enter")
	if !strings.Contains(m.View(), "!! ") {
		t.Errorf("missing file not reported:\n%s", m.View())
	}
}

func TestFileManagerUploadAndPrint(t *testing.T) {
	p := newFmPrinter(t)
	m := fmOpen(t, p, 160)
	m = fmRun(t, m, m.openFileManager("", ""))
	lastUploadDir = localDir(t)
	t.Cleanup(func() { lastUploadDir = "" })

	m = fmKeys(t, m, "P")
	if !m.jobs.files.pick.print || !strings.Contains(m.View(), "Upload and print") {
		t.Fatal("P didn't open the picker for upload & print")
	}
	m = fmKeys(t, m, "down", "enter") // b.gcode
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "Upload b.gcode") || !strings.Contains(m.confirmation.prompt, "print it?") {
		t.Fatalf("no confirmation: %+v", m.confirmation)
	}
	if p.uploaded() != "" {
		t.Fatal("uploaded before the confirmation")
	}
	m, cmd := updateCmd(t, m, keyRunes("y"))
	m = fmRun(t, m, cmd)
	if got := p.uploaded(); got != "gcodes||b.gcode|G28 ; b\n|print" {
		t.Fatalf("uploads = %q", got)
	}
	if !strings.Contains(m.View(), "uploaded b.gcode") || !strings.Contains(m.View(), "printing") {
		t.Errorf("status:\n%s", m.View())
	}
	// Not while a print is running.
	m.job.State = "printing"
	m = fmKeys(t, m, "P")
	if m.jobs.files.pick.open {
		t.Fatal("upload & print offered during a print")
	}
}

func TestFileListChangedRefreshes(t *testing.T) {
	p := newFmPrinter(t)
	m := fmOpen(t, p, 160)
	before := len(p.CallsTo("server.files.list"))
	params := rawParams(t, map[string]any{"action": "create_file", "item": map[string]any{"root": "gcodes", "path": "new.gcode"}})
	m = fmRun(t, m, m.onFileListChanged(params))
	if len(p.CallsTo("server.files.list")) != before+1 {
		t.Fatal("Files pane not reloaded")
	}
	if n := len(p.CallsTo("server.files.get_directory")); n != 0 {
		t.Fatalf("closed file manager reloaded (%d)", n)
	}
	m = fmRun(t, m, m.openFileManager("", ""))
	dirs := len(p.CallsTo("server.files.get_directory"))
	m = fmRun(t, m, m.onFileListChanged(params))
	if len(p.CallsTo("server.files.get_directory")) != dirs+1 {
		t.Fatal("open file manager not reloaded")
	}
	cfg := rawParams(t, map[string]any{"action": "modify_file", "item": map[string]any{"root": "config", "path": "printer.cfg"}})
	if cmd := m.onFileListChanged(cfg); cmd != nil {
		t.Fatal("a config change reloaded gcodes")
	}
	// Routed from the notification itself.
	_, cmd := updateCmd(t, m, notificationMsg{Method: moonraker.MethodFileListChanged, Params: params})
	if cmd == nil {
		t.Fatal("notify_filelist_changed not routed")
	}
}

func TestJobsTabsCycleThroughFiles(t *testing.T) {
	p := newFmPrinter(t)
	m := openJobsScreen(t, fmOpen(t, p, 160))
	if m.jobs.tab != jobsHistory {
		t.Fatal("J should open on History")
	}
	m = fmKeys(t, m, "shift+tab")
	if m.jobs.tab != jobsFiles || !strings.Contains(m.View(), "top.gcode") {
		t.Fatalf("shift+tab from History: tab %d\n%s", m.jobs.tab, m.View())
	}
	m = fmKeys(t, m, "tab")
	if m.jobs.tab != jobsHistory {
		t.Fatalf("tab from Files: %d", m.jobs.tab)
	}
	m = fmKeys(t, m, "shift+tab", "esc")
	if m.jobs.open {
		t.Fatal("esc on Files should close")
	}
}
