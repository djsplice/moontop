package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Hosts is the connection history: every printer Moontop has connected
// to, newest first, with what it needs to sign in again. It holds
// secrets (API keys, refresh tokens — never passwords), so it lives in
// its own file with owner-only permissions, apart from config.toml.
type Hosts struct {
	Entries []HostEntry `json:"hosts"`
	// Default is the host to connect to at start, skipping the
	// connection screen ("" = always show it).
	Default string `json:"default,omitempty"`
}

// HostEntry is one remembered printer.
type HostEntry struct {
	Host         string    `json:"host"`
	Name         string    `json:"name,omitempty"` // the printer's hostname, when known
	LastUsed     time.Time `json:"last_used"`
	APIKey       string    `json:"api_key,omitempty"`
	Username     string    `json:"username,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
}

// SignedIn reports whether the entry carries credentials.
func (e HostEntry) SignedIn() bool { return e.APIKey != "" || e.RefreshToken != "" }

// DefaultHostsPath is $XDG_STATE_HOME/moontop/hosts.json
// (~/.local/state/moontop/hosts.json), beside the console history.
func DefaultHostsPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "moontop", "hosts.json"), nil
}

// LoadHosts reads the history. A missing file is an empty history.
func LoadHosts(path string) (*Hosts, error) {
	h := &Hosts{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	if err := json.Unmarshal(data, h); err != nil {
		return &Hosts{}, err
	}
	h.sort()
	return h, nil
}

// Save writes the history with owner-only permissions.
func (h *Hosts) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (h *Hosts) sort() {
	sort.SliceStable(h.Entries, func(i, j int) bool { return h.Entries[i].LastUsed.After(h.Entries[j].LastUsed) })
}

func (h *Hosts) index(host string) int {
	for i, e := range h.Entries {
		if strings.EqualFold(e.Host, host) {
			return i
		}
	}
	return -1
}

// Lookup finds a remembered host.
func (h *Hosts) Lookup(host string) (HostEntry, bool) {
	if i := h.index(host); i >= 0 {
		return h.Entries[i], true
	}
	return HostEntry{}, false
}

// Touch records a successful connection: e replaces any entry for the
// same host (keeping its name if e has none) and moves to the top.
func (h *Hosts) Touch(e HostEntry, now time.Time) {
	e.LastUsed = now
	if i := h.index(e.Host); i >= 0 {
		if e.Name == "" {
			e.Name = h.Entries[i].Name
		}
		h.Entries = append(h.Entries[:i], h.Entries[i+1:]...)
	}
	h.Entries = append([]HostEntry{e}, h.Entries...)
	const keep = 20
	if len(h.Entries) > keep {
		h.Entries = h.Entries[:keep]
	}
}

// Forget removes a host (and its default status).
func (h *Hosts) Forget(host string) {
	if i := h.index(host); i >= 0 {
		h.Entries = append(h.Entries[:i], h.Entries[i+1:]...)
	}
	if strings.EqualFold(h.Default, host) {
		h.Default = ""
	}
}

// IsDefault reports whether host is the start-up default.
func (h *Hosts) IsDefault(host string) bool {
	return h.Default != "" && strings.EqualFold(h.Default, host)
}

// ToggleDefault makes host the default, or clears it if it already is.
func (h *Hosts) ToggleDefault(host string) {
	if h.IsDefault(host) {
		h.Default = ""
		return
	}
	h.Default = host
}
