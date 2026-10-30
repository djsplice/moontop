package console

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// DefaultHistoryMax caps how many commands History keeps (and saves).
const DefaultHistoryMax = 1000

// History is shell-style command history with up/down recall and draft
// restoration.
//
// Recall-state convention (same as the dashboard console pane):
//   - idx == len(entries): not recalling; the caller's input holds a
//     live draft.
//   - 0 <= idx < len(entries): recalling entries[idx]; draft holds what
//     was typed before the first Up.
type History struct {
	entries []string
	idx     int
	draft   string
	max     int
}

// NewHistory returns an empty history holding at most max entries
// (DefaultHistoryMax if max <= 0).
func NewHistory(max int) *History {
	if max <= 0 {
		max = DefaultHistoryMax
	}
	return &History{max: max}
}

// Entries returns a copy of the stored commands, oldest first.
func (h *History) Entries() []string { return append([]string(nil), h.entries...) }

// Add records a sent command. Blank commands and an exact repeat of the
// newest entry are skipped. Recall state resets either way.
func (h *History) Add(cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd != "" && (len(h.entries) == 0 || h.entries[len(h.entries)-1] != cmd) {
		h.entries = append(h.entries, cmd)
		if over := len(h.entries) - h.max; over > 0 {
			h.entries = append([]string(nil), h.entries[over:]...)
		}
	}
	h.Reset()
}

// Reset leaves recall mode (call when the user edits a recalled line).
func (h *History) Reset() {
	h.idx = len(h.entries)
	h.draft = ""
}

// Recalling reports whether Up has been pressed since the last Reset.
func (h *History) Recalling() bool { return h.idx < len(h.entries) }

// Up steps toward older entries. current is the input's present value,
// saved as the draft on the first Up. ok is false when there is nothing
// older (the input should stay as it is).
func (h *History) Up(current string) (value string, ok bool) {
	if len(h.entries) == 0 || h.idx == 0 {
		return "", false
	}
	if h.idx == len(h.entries) {
		h.draft = current
	}
	h.idx--
	return h.entries[h.idx], true
}

// Down steps toward newer entries, restoring the draft after the newest.
// ok is false when not recalling.
func (h *History) Down() (value string, ok bool) {
	if h.idx >= len(h.entries) {
		return "", false
	}
	h.idx++
	if h.idx == len(h.entries) {
		d := h.draft
		h.draft = ""
		return d, true
	}
	return h.entries[h.idx], true
}

// DefaultHistoryPath is $XDG_STATE_HOME/moontop/console_history, falling
// back to ~/.local/state/moontop/console_history.
func DefaultHistoryPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "moontop", "console_history"), nil
}

// Load replaces entries with the file's lines (one command per line). A
// missing file is not an error.
func (h *History) Load(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	h.entries = nil
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			h.entries = append(h.entries, l)
		}
	}
	if over := len(h.entries) - h.max; over > 0 {
		h.entries = h.entries[over:]
	}
	h.Reset()
	return sc.Err()
}

// Save writes entries to path, creating its directory.
func (h *History) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data := strings.Join(h.entries, "\n")
	if data != "" {
		data += "\n"
	}
	if err := os.WriteFile(tmp, []byte(data), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
