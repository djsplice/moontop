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

func updatesPrinter(t *testing.T, oldMoonraker bool) *moonrakertest.Server {
	t.Helper()
	srv := fakePrinter(t)
	srv.Handle("machine.update.status", moonrakertest.Result(map[string]any{"busy": false, "version_info": map[string]any{
		"system":    map[string]any{"package_count": 3.0},
		"moonraker": map[string]any{"version": "v0.9.3-10", "remote_version": "v0.9.3-10", "is_valid": true, "commits_behind": []any{}},
		"klipper": map[string]any{"version": "v0.13.0-100", "remote_version": "v0.13.0-102", "is_valid": true, "commits_behind": []any{
			map[string]any{"sha": "abcdef1234", "subject": "toolhead: faster lookahead", "author": "Kevin"},
			map[string]any{"sha": "1234567890", "subject": "docs: typo", "author": "Someone"},
		}},
		"mainsail": map[string]any{"version": "v2.12.0", "remote_version": "v2.13.0", "configured_type": "web"},
		"crowsnest": map[string]any{"version": "v4.1.0", "remote_version": "v4.1.0", "is_dirty": true},
	}}))
	srv.Handle("server.announcements.list", moonrakertest.Result(map[string]any{"entries": []any{
		map[string]any{"entry_id": "arksine/moonlight/issue/3", "title": "Klipper 0.13 is out", "description": "Lots of changes. Read the release notes before updating.", "url": "https://example.org/notes", "priority": "high", "date": 1.73e9, "feed": "klipper"},
	}}))
	if oldMoonraker {
		srv.Handle("machine.update.upgrade", func(json.RawMessage) (any, error) { return nil, errors.New("Method not found") })
	}
	return srv
}

func openUpdates(t *testing.T, srv *moonrakertest.Server) model {
	t.Helper()
	m := readyModel(t, srv)
	m = update(t, m, loadUpdatesCmd(m.client)()) // what init does
	m, cmd := updateCmd(t, m, keyRunes("U"))
	if !m.updates.open || cmd == nil {
		t.Fatal("U didn't open")
	}
	return update(t, m, cmd())
}

func TestUpdates(t *testing.T) {
	srv := updatesPrinter(t, false)
	m := readyModel(t, srv)
	m = update(t, m, loadUpdatesCmd(m.client)())
	// The dashboard title says something's waiting.
	if v := m.View(); !strings.Contains(v, "⬆ 3 updates · ✉ 1") {
		t.Fatalf("no title badge:\n%s", strings.SplitN(v, "\n", 2)[0])
	}
	m = openUpdates(t, srv)
	v := m.View()
	for _, s := range []string{"Updates (3)", "Announcements (1)", "system", "3 packages to update", "moonraker", "up to date",
		"klipper", "v0.13.0-100 → v0.13.0-102", "2 commits behind", "mainsail", "v2.12.0 → v2.13.0", "crowsnest", "dirty"} {
		if !strings.Contains(v, s) {
			t.Errorf("updates lack %q", s)
		}
	}
	// Order: system, moonraker, klipper, then by name.
	if strings.Join(m.updateNames(), ",") != "system,moonraker,klipper,crowsnest,mainsail" {
		t.Fatalf("order %v", m.updateNames())
	}
	// Up to date: refused. Klipper: its commits show; enter confirms.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirmation != nil || !logContains(m, "moonraker has no update") {
		t.Fatal("updated an up-to-date component")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if v := m.View(); !strings.Contains(v, "Incoming for klipper") || !strings.Contains(v, "abcdef1  toolhead: faster lookahead") {
		t.Fatalf("commits missing:\n%s", v)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "Update klipper?") {
		t.Fatalf("confirmation %+v", m.confirmation)
	}
	m, cmd := updateCmd(t, m, keyRunes("y"))
	m = update(t, m, cmd())
	var p struct{ Name string }
	_ = json.Unmarshal(srv.CallsTo("machine.update.upgrade")[0].Params, &p)
	if p.Name != "klipper" {
		t.Fatalf("upgraded %q", p.Name)
	}
	// Streamed output, then complete.
	m.onUpdateResponse(rawParams(t, map[string]any{"application": "klipper", "message": "Updating Repo...\nPulling 2 commits", "complete": false}))
	if v := m.View(); !strings.Contains(v, "Pulling 2 commits") {
		t.Fatalf("no streamed output:\n%s", v)
	}
	if cmd := m.onUpdateResponse(rawParams(t, map[string]any{"message": "Update Finished", "complete": true})); cmd == nil {
		t.Fatal("completion didn't reload")
	}
	// a: everything with an update, one confirmation.
	m = update(t, m, keyRunes("a"))
	if m.confirmation == nil || !strings.Contains(m.confirmation.prompt, "Update system, klipper, mainsail?") {
		t.Fatalf("update all: %+v", m.confirmation)
	}
	m = update(t, m, keyRunes("n"))
	// Not while printing.
	m.job.State = "printing"
	if m = update(t, m, keyRunes("a")); m.confirmation != nil || !logContains(m, "wait until the print is done") {
		t.Fatal("update during a print")
	}
	m.job.State = "standby"

	// Announcements: description and link; d dismisses.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	v = m.View()
	for _, s := range []string{"Klipper 0.13 is out", "! ", "Read the release notes", "https://example.org/notes"} {
		if !strings.Contains(v, s) {
			t.Errorf("announcements lack %q", s)
		}
	}
	_, cmd = updateCmd(t, m, keyRunes("d"))
	cmd()
	srv.WaitForCall("server.announcements.dismiss", 1, wait)
}

func TestUpgradeFallsBackOnOldMoonraker(t *testing.T) {
	srv := updatesPrinter(t, true)
	c, err := moonraker.New(srv.Host())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Upgrade("klipper"); err != nil {
		t.Fatal(err)
	}
	if err := c.Upgrade("mainsail"); err != nil {
		t.Fatal(err)
	}
	if len(srv.CallsTo("machine.update.klipper")) != 1 || len(srv.CallsTo("machine.update.client")) != 1 {
		t.Fatal("no fallback to the per-kind endpoints")
	}
}
