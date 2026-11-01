package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker/moonrakertest"
)

func jobsPrinter(t *testing.T) *moonrakertest.Server {
	t.Helper()
	srv := fakePrinter(t)
	srv.Handle("server.history.list", moonrakertest.Result(map[string]any{"count": 3, "jobs": []any{
		map[string]any{"job_id": "0003", "filename": "benchy.gcode", "status": "completed", "start_time": 1.7e9, "print_duration": 4320.0, "filament_used": 3400.0, "exists": true},
		map[string]any{"job_id": "0002", "filename": "cube.gcode", "status": "cancelled", "start_time": 1.69e9, "print_duration": 300.0, "exists": true},
		map[string]any{"job_id": "0001", "filename": "gone.gcode", "status": "error", "start_time": 1.68e9, "exists": false},
	}}))
	srv.Handle("server.history.totals", moonrakertest.Result(map[string]any{"job_totals": map[string]any{
		"total_jobs": 3, "total_print_time": 4620.0, "total_filament_used": 3400.0, "longest_print": 4320.0,
	}}))
	srv.Handle("server.job_queue.status", moonrakertest.Result(map[string]any{"queue_state": "paused", "queued_jobs": []any{
		map[string]any{"job_id": "q1", "filename": "plate1.gcode", "time_in_queue": 120.0},
	}}))
	return srv
}

func openJobsScreen(t *testing.T, m model) model {
	t.Helper()
	m, cmd := updateCmd(t, m, keyRunes("J"))
	if !m.jobs.open || cmd == nil {
		t.Fatal("J didn't open Jobs")
	}
	return update(t, m, cmd())
}

func TestJobsHistory(t *testing.T) {
	srv := jobsPrinter(t)
	m := openJobsScreen(t, readyModel(t, srv))
	v := m.View()
	for _, s := range []string{"Jobs", "History", "Queue (1)", "3 jobs", "1h17m printing", "3.4 m filament", "1h12m", "benchy.gcode", "✓", "completed", "cube.gcode", "cancelled", "gone.gcode"} {
		if !strings.Contains(v, s) {
			t.Errorf("history lacks %q:\n%s", s, v)
		}
	}
	// enter: print again, after a confirmation.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirmation == nil || !strings.Contains(m.View(), "Print benchy.gcode again?") {
		t.Fatal("no reprint confirmation")
	}
	m, cmd := updateCmd(t, m, keyRunes("y"))
	cmd()
	srv.WaitForCall("printer.print.start", 1, wait)
	// a queues; a missing file can't be reprinted or queued.
	m, cmd = updateCmd(t, m, keyRunes("a"))
	if cmd == nil {
		t.Fatal("a didn't queue")
	}
	update(t, m, cmd())
	srv.WaitForCall("server.job_queue.post_job", 1, wait)
	var p struct {
		Filenames []string `json:"filenames"`
	}
	_ = json.Unmarshal(srv.CallsTo("server.job_queue.post_job")[0].Params, &p)
	if strings.Join(p.Filenames, ",") != "benchy.gcode" {
		t.Fatalf("queued %v", p.Filenames)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp}) // wraps to gone.gcode
	if m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}); m.confirmation != nil || !logContains(m, "no longer on the printer") {
		t.Fatal("reprinted a missing file")
	}
	// x deletes the record, after a confirmation.
	m = update(t, m, keyRunes("x"))
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "history record for gone.gcode") {
		t.Fatal("no delete confirmation")
	}
	m, cmd = updateCmd(t, m, keyRunes("y"))
	cmd()
	srv.WaitForCall("server.history.delete_job", 1, wait)
	// A history change while open reloads.
	before := len(srv.CallsTo("server.history.list"))
	if cmd = m.onJobsChanged(nil); cmd == nil {
		t.Fatal("notify_history_changed didn't reload")
	}
	cmd()
	if len(srv.CallsTo("server.history.list")) != before+1 {
		t.Fatal("no reload")
	}
	if m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc}); m.jobs.open {
		t.Fatal("esc didn't close")
	}
}

func TestJobsQueue(t *testing.T) {
	srv := jobsPrinter(t)
	m := openJobsScreen(t, readyModel(t, srv))
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	v := m.View()
	for _, s := range []string{"Queue is paused", "s starts it", "plate1.gcode", "queued 2m"} {
		if !strings.Contains(v, s) {
			t.Errorf("queue lacks %q:\n%s", s, v)
		}
	}
	m, cmd := updateCmd(t, m, keyRunes("s"))
	cmd()
	srv.WaitForCall("server.job_queue.start", 1, wait)
	_, cmd = updateCmd(t, m, keyRunes("x"))
	cmd()
	srv.WaitForCall("server.job_queue.delete_job", 1, wait)

	// From Files, a queues the selected file.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m.fileInfos = []moonraker.FileInfo{{Path: "plate2.gcode"}}
	m.rebuildFiles()
	m.setFocus(focusFiles)
	if _, cmd = updateCmd(t, m, keyRunes("a")); cmd == nil {
		t.Fatal("a in Files didn't queue")
	}
	cmd()
	srv.WaitForCall("server.job_queue.post_job", 1, wait)
}

func TestJobsComponentsDisabled(t *testing.T) {
	srv := fakePrinter(t)
	notFound := func(json.RawMessage) (any, error) { return nil, errors.New("Method not found") }
	srv.Handle("server.history.list", notFound)
	srv.Handle("server.job_queue.status", notFound)
	m := openJobsScreen(t, readyModel(t, srv))
	if v := m.View(); !strings.Contains(v, "add [history] to moonraker.conf") {
		t.Fatalf("history tab:\n%s", v)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if v := m.View(); !strings.Contains(v, "add [job_queue] to moonraker.conf") {
		t.Fatalf("queue tab:\n%s", v)
	}
}
