package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/config"
	"github.com/justinh-rahb/moontop/internal/moonraker"
	"github.com/justinh-rahb/moontop/internal/moonraker/moonrakertest"
)

// connModel is a connection manager over a temp host list.
func connModel(t *testing.T, entries ...config.HostEntry) (connectModel, string) {
	t.Helper()
	t.Setenv("MOONRAKER_API_KEY", "")
	path := filepath.Join(t.TempDir(), "state", "hosts.json")
	h := &config.Hosts{Entries: entries}
	m := newConnectModel(h, path, false)
	m.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	return m, path
}

func connKey(t *testing.T, m connectModel, keys ...string) connectModel {
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
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "ctrl+d":
			msg = tea.KeyMsg{Type: tea.KeyCtrlD}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		default:
			msg = keyRunes(k)
		}
		out, cmd := m.Update(msg)
		m = out.(connectModel)
		if k == "enter" {
			m = connDrain(t, m, cmd)
		}
	}
	return m
}

// connDrain runs the connect / login Cmds an enter starts, feeding their
// results back until there's nothing more to do.
func connDrain(t *testing.T, m connectModel, cmd tea.Cmd) connectModel {
	t.Helper()
	for i := 0; cmd != nil && i < 5; i++ {
		msg := cmd()
		switch msg.(type) {
		case connectedMsg, loginMsg:
		default:
			return m
		}
		var out tea.Model
		out, cmd = m.Update(msg)
		m = out.(connectModel)
	}
	if m.client != nil {
		t.Cleanup(func() { m.client.Close() })
	}
	return m
}

func connType(t *testing.T, m connectModel, s string) connectModel {
	t.Helper()
	out, _ := m.Update(keyRunes(s))
	return out.(connectModel)
}

func TestConnectTypedHostIsRemembered(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.Handle("printer.info", moonrakertest.Result(map[string]any{"state": "ready", "hostname": "voron"}))
	m, path := connModel(t)
	if v := m.View(); !strings.Contains(v, "Connect") || !strings.Contains(v, "Printer") || strings.Contains(v, "Recent") {
		t.Fatalf("empty connect screen:\n%s", v)
	}
	m = connKey(t, m, "enter")
	if !strings.Contains(m.errText, "type the printer's address") {
		t.Fatalf("empty host: %q", m.errText)
	}
	m = connType(t, m, srv.Host())
	m = connKey(t, m, "enter")
	if m.client == nil {
		t.Fatalf("not connected: %q", m.errText)
	}
	h, err := config.LoadHosts(path)
	if err != nil || len(h.Entries) != 1 || h.Entries[0].Host != srv.Host() || h.Entries[0].Name != "voron" {
		t.Fatalf("saved = %+v, %v", h.Entries, err)
	}
}

func TestConnectRecentHosts(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	m, path := connModel(t,
		config.HostEntry{Host: "newest:7125", Name: "u1", LastUsed: now.Add(-2 * time.Hour), APIKey: "k"},
		config.HostEntry{Host: "older:7125", LastUsed: now.Add(-72 * time.Hour), Username: "me", RefreshToken: "r"},
	)
	if m.host.Value() != "newest:7125" || m.sel != 0 {
		t.Fatalf("last used not preselected: %q sel %d", m.host.Value(), m.sel)
	}
	v := m.View()
	for _, want := range []string{"Recent", "newest:7125", "u1", "2 h ago", "api key", "older:7125", "3 days ago", "signed in as me"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	m = connKey(t, m, "down")
	if m.host.Value() != "older:7125" {
		t.Fatalf("down → %q", m.host.Value())
	}
	m = connKey(t, m, "down")
	if m.host.Value() != "newest:7125" {
		t.Fatalf("down wraps → %q", m.host.Value())
	}
	m = connKey(t, m, "up", "ctrl+d")
	if len(m.hosts.Entries) != 1 || m.hosts.Entries[0].Host != "newest:7125" || m.host.Value() != "newest:7125" {
		t.Fatalf("forget: %+v, input %q", m.hosts.Entries, m.host.Value())
	}
	if h, _ := config.LoadHosts(path); len(h.Entries) != 1 {
		t.Fatalf("forget not saved: %+v", h.Entries)
	}
	// Typing replaces the selection.
	m = connKey(t, m, "x")
	if m.sel != -1 {
		t.Fatal("typing should drop the selection")
	}
}

func TestConnectUnreachable(t *testing.T) {
	srv := moonrakertest.New(t)
	host := srv.Host()
	srv.Close()
	m, _ := connModel(t)
	m = connType(t, m, host)
	m = connKey(t, m, "enter")
	if m.client != nil || !strings.Contains(m.View(), "connection refused") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestConnectLogin(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.RequireAuth("", "admin", "hunter2")
	m, path := connModel(t)
	m = connType(t, m, srv.Host())
	m = connKey(t, m, "enter")
	if m.stage != stageAuth || !strings.Contains(m.View(), "needs you to sign in") || m.row != authRowUser {
		t.Fatalf("no sign-in form (row %d):\n%s", m.row, m.View())
	}
	m = connType(t, m, "admin")
	m = connKey(t, m, "tab")
	m = connType(t, m, "wrong")
	m = connKey(t, m, "enter")
	if m.client != nil || !strings.Contains(m.View(), "wrong username or password") {
		t.Fatalf("bad password:\n%s", m.View())
	}
	if strings.Contains(m.View(), "wrong\n") || strings.Contains(m.View(), "hunter2") {
		t.Fatal("password shown in clear")
	}
	m = connKey(t, m, "ctrl+u")
	m = connType(t, m, "hunter2")
	m = connKey(t, m, "enter")
	if m.client == nil {
		t.Fatalf("not connected: %q", m.errText)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "hunter2") {
		t.Fatal("password written to the host list")
	}
	e, ok := mustHosts(t, path).Lookup(srv.Host())
	if !ok || e.Username != "admin" || e.RefreshToken == "" {
		t.Fatalf("saved entry = %+v", e)
	}
	// Next time, the saved refresh token connects without a form.
	m2 := newConnectModel(mustHosts(t, path), path, false)
	m2 = connKey(t, m2, "enter")
	if m2.client == nil || m2.stage != stageHost {
		t.Fatalf("saved login not reused: stage %d, %q", m2.stage, m2.errText)
	}
}

func TestConnectAPIKey(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.RequireAuth("sekrit", "", "")
	m, path := connModel(t)
	m = connType(t, m, srv.Host())
	m = connKey(t, m, "enter", "up") // to the method row
	m = connKey(t, m, "right")
	if m.method != authAPIKey || !strings.Contains(m.View(), "API key") {
		t.Fatalf("method = %d\n%s", m.method, m.View())
	}
	m = connKey(t, m, "tab")
	m = connType(t, m, "sekrit")
	m = connKey(t, m, "enter")
	if m.client == nil {
		t.Fatalf("not connected: %q", m.errText)
	}
	if e, _ := mustHosts(t, path).Lookup(srv.Host()); e.APIKey != "sekrit" {
		t.Fatalf("key not saved: %+v", e)
	}
	// A saved key that stops working lands on the form, on the key.
	h := mustHosts(t, path)
	h.Touch(config.HostEntry{Host: srv.Host(), APIKey: "old"}, time.Now())
	m2 := newConnectModel(h, path, false)
	m2 = connKey(t, m2, "enter")
	if m2.stage != stageAuth || m2.method != authAPIKey || !strings.Contains(m2.View(), "the API key was refused") {
		t.Fatalf("stale key:\n%s", m2.View())
	}
	// esc goes back to the host list.
	m2 = connKey(t, m2, "esc")
	if m2.stage != stageHost {
		t.Fatal("esc should go back")
	}
}

func TestConnectEnvAPIKeyNotSaved(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.RequireAuth("envkey", "", "")
	m, path := connModel(t)
	m.envAPIKey = "envkey"
	m = connType(t, m, srv.Host())
	m = connKey(t, m, "enter")
	if m.client == nil {
		t.Fatalf("not connected: %q", m.errText)
	}
	if e, _ := mustHosts(t, path).Lookup(srv.Host()); e.APIKey != "" {
		t.Fatal("an API key from the environment was written to disk")
	}
}

// -host with saved credentials connects without the manager.
func TestConnectClientUsesSavedCredentials(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.RequireAuth("k1", "", "")
	t.Setenv("MOONRAKER_API_KEY", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path, _ := config.DefaultHostsPath()
	h := &config.Hosts{}
	h.Touch(config.HostEntry{Host: srv.Host(), APIKey: "k1"}, time.Now())
	if err := h.Save(path); err != nil {
		t.Fatal(err)
	}
	c, host, err := connectClient("http://"+srv.Host()+"/", false, true)
	if err != nil || c == nil || host != srv.Host() {
		t.Fatalf("connectClient = %v %q %v", c, host, err)
	}
	c.Close()
	if _, err := dialHost(srv.Host()); err != nil {
		t.Fatalf("dialHost: %v", err)
	}
	other := moonrakertest.New(t)
	other.RequireAuth("k2", "", "")
	if _, err := dialHost(other.Host()); err == nil || !strings.Contains(err.Error(), "MOONRAKER_API_KEY") {
		t.Fatalf("dialHost without creds: %v", err)
	}
	t.Setenv("MOONRAKER_API_KEY", "k2")
	if c, err := dialHost(other.Host()); err != nil {
		t.Fatalf("env key: %v", err)
	} else {
		c.Close()
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now", 5 * time.Minute: "5 min ago", 3 * time.Hour: "3 h ago", 50 * time.Hour: "2 days ago",
	} {
		if got := ago(now.Add(-d), now); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
	if ago(time.Time{}, now) != "" {
		t.Error("zero time")
	}
}

func mustHosts(t *testing.T, path string) *config.Hosts {
	t.Helper()
	h, err := config.LoadHosts(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

var _ = moonraker.ErrUnauthorized

func TestConnectDefaultHost(t *testing.T) {
	now := time.Now()
	m, path := connModel(t,
		config.HostEntry{Host: "a:7125", LastUsed: now},
		config.HostEntry{Host: "b:7125", LastUsed: now.Add(-time.Hour)},
	)
	if !strings.Contains(m.View(), "ctrl+s connect at start") {
		t.Fatalf("footer lacks the default key:\n%s", m.View())
	}
	m = connKey(t, m, "down")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = out.(connectModel)
	if !m.hosts.IsDefault("b:7125") || !strings.Contains(m.View(), "[default]") || !strings.Contains(m.View(), "connects to it at start") {
		t.Fatalf("default not set:\n%s", m.View())
	}
	if mustHosts(t, path).Default != "b:7125" {
		t.Fatal("default not saved")
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = out.(connectModel)
	if m.hosts.Default != "" || mustHosts(t, path).Default != "" {
		t.Fatal("ctrl+s again should clear the default")
	}
}

// With a default, start-up connects to it without the screen; the
// connection screen reached from the dashboard (useDefault false) and
// -host don't use it.
func TestConnectClientUsesDefault(t *testing.T) {
	srv := moonrakertest.New(t)
	t.Setenv("MOONRAKER_API_KEY", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path, _ := config.DefaultHostsPath()
	h := &config.Hosts{}
	h.Touch(config.HostEntry{Host: srv.Host()}, time.Now())
	h.ToggleDefault(srv.Host())
	if err := h.Save(path); err != nil {
		t.Fatal(err)
	}
	c, host, err := connectClient("", false, true)
	if err != nil || c == nil || host != srv.Host() {
		t.Fatalf("default: %v %q %v", c, host, err)
	}
	c.Close()
	// Unreachable default without a terminal: an error, not a hang.
	interactive = func() bool { return false }
	t.Cleanup(func() { interactive = func() bool { return isTerminal(os.Stdin) } })
	srv.Close()
	if _, _, err := connectClient("", false, true); err == nil {
		t.Fatal("unreachable default should fail without a terminal")
	}
}

func TestPrintersKeyLeavesForTheConnectScreen(t *testing.T) {
	m := readyModel(t, fakePrinter(t))
	m, cmd := updateCmd(t, m, keyRunes("P"))
	if !m.switchPrinter || cmd == nil {
		t.Fatal("P didn't ask to switch printers")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("P should end this dashboard")
	}
	// Typing in the console, P is just a letter.
	m = readyModel(t, fakePrinter(t))
	m = press(t, m, "5")
	m, _ = updateCmd(t, m, keyRunes("P"))
	if m.switchPrinter {
		t.Fatal("P in the console switched printers")
	}
}
