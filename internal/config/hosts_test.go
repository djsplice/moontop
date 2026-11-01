package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestHostsTouchOrderAndForget(t *testing.T) {
	h := &Hosts{}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	h.Touch(HostEntry{Host: "a:7125", Name: "voron"}, t0)
	h.Touch(HostEntry{Host: "b:7125"}, t0.Add(time.Hour))
	h.Touch(HostEntry{Host: "A:7125", APIKey: "k"}, t0.Add(2*time.Hour))
	if len(h.Entries) != 2 || h.Entries[0].Host != "A:7125" || h.Entries[0].Name != "voron" || h.Entries[0].APIKey != "k" {
		t.Fatalf("entries = %+v", h.Entries)
	}
	if e, ok := h.Lookup("a:7125"); !ok || !e.SignedIn() {
		t.Fatalf("lookup = %+v %v", e, ok)
	}
	h.Forget("b:7125")
	if len(h.Entries) != 1 {
		t.Fatalf("after forget: %+v", h.Entries)
	}
}

func TestHostsSaveLoadPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "hosts.json")
	h := &Hosts{}
	h.Touch(HostEntry{Host: "old:7125"}, time.Unix(100, 0))
	h.Touch(HostEntry{Host: "new:7125", RefreshToken: "r", Username: "u"}, time.Unix(200, 0))
	if err := h.Save(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	got, err := LoadHosts(path)
	if err != nil || len(got.Entries) != 2 || got.Entries[0].Host != "new:7125" || got.Entries[0].RefreshToken != "r" {
		t.Fatalf("load = %+v, %v", got, err)
	}
	if empty, err := LoadHosts(filepath.Join(t.TempDir(), "missing.json")); err != nil || len(empty.Entries) != 0 {
		t.Fatalf("missing file: %+v %v", empty, err)
	}
}

func TestDefaultHostsPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x/state")
	if p, _ := DefaultHostsPath(); p != filepath.Join("/x/state", "moontop", "hosts.json") {
		t.Fatal(p)
	}
}

func TestHostsDefault(t *testing.T) {
	h := &Hosts{}
	h.Touch(HostEntry{Host: "a:7125"}, time.Unix(1, 0))
	h.ToggleDefault("a:7125")
	if !h.IsDefault("A:7125") {
		t.Fatal("not default")
	}
	path := filepath.Join(t.TempDir(), "hosts.json")
	if err := h.Save(path); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadHosts(path)
	if got.Default != "a:7125" {
		t.Fatalf("default not saved: %+v", got)
	}
	got.ToggleDefault("a:7125")
	if got.Default != "" {
		t.Fatal("toggle should clear")
	}
	got.ToggleDefault("a:7125")
	got.Forget("a:7125")
	if got.Default != "" {
		t.Fatal("forgetting the default host should clear it")
	}
}
