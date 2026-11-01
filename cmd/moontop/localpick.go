package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Picking a file on this computer to upload (u) or upload and print (P)
// from the file manager. A small browser of the local disk: folders
// first, then gcode files newest first — a fresh slice from the slicer
// is at the top. It starts in the folder of the last upload, else
// ~/Downloads. ":" switches to typing a path instead.
// ---------------------------------------------------------------------------

var gcodeExts = map[string]bool{".gcode": true, ".bgcode": true, ".g": true, ".gco": true, ".gc": true}

type localEntry struct {
	name string
	dir  bool
	size int64
	mod  time.Time
}

type localPicker struct {
	open    bool
	print   bool // upload and print
	dir     string
	entries []localEntry
	cursor  int
	showAll bool
	err     string
}

// lastUploadDir is where the picker starts next time (per session).
var lastUploadDir string

func defaultUploadDir() string {
	if lastUploadDir != "" {
		return lastUploadDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	if fi, err := os.Stat(filepath.Join(home, "Downloads")); err == nil && fi.IsDir() {
		return filepath.Join(home, "Downloads")
	}
	return home
}

func (p *localPicker) load(dir string, selectName string) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		dir = abs
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		p.err = err.Error()
		return
	}
	p.dir, p.err, p.cursor = dir, "", 0
	p.entries = p.entries[:0]
	var dirs, files []localEntry
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		if info.IsDir() {
			dirs = append(dirs, localEntry{name: de.Name(), dir: true, mod: info.ModTime()})
			continue
		}
		if !p.showAll && !gcodeExts[strings.ToLower(filepath.Ext(de.Name()))] {
			continue
		}
		files = append(files, localEntry{name: de.Name(), size: info.Size(), mod: info.ModTime()})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name) })
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	p.entries = append(append(p.entries, dirs...), files...)
	for i, e := range p.entries {
		if e.name == selectName {
			p.cursor = i
		}
	}
}

// openPicker starts choosing a local file; print chooses upload & print.
func (m *model) openPicker(print bool) tea.Cmd {
	if print && jobActive(m.job.State) {
		return m.refuse("a print is already running; u uploads without printing")
	}
	p := &m.jobs.files.pick
	p.open, p.print = true, print
	p.load(defaultUploadDir(), "")
	if p.err != "" { // a remembered folder that's gone
		lastUploadDir = ""
		p.load(defaultUploadDir(), "")
	}
	m.jobs.files.status = ""
	return nil
}

func (m *model) handlePickKey(msg tea.KeyMsg) tea.Cmd {
	p := &m.jobs.files.pick
	n := len(p.entries)
	switch msg.String() {
	case "esc", "q":
		p.open = false
	case "up", "k":
		if n > 0 {
			p.cursor = (p.cursor + n - 1) % n
		}
	case "down", "j":
		if n > 0 {
			p.cursor = (p.cursor + 1) % n
		}
	case "pgup":
		p.cursor = max(0, p.cursor-10)
	case "pgdown":
		p.cursor = min(max(0, n-1), p.cursor+10)
	case "left", "h", "backspace":
		parent := filepath.Dir(p.dir)
		if parent != p.dir {
			p.load(parent, filepath.Base(p.dir))
		}
	case "~":
		if home, err := os.UserHomeDir(); err == nil {
			p.load(home, "")
		}
	case ".":
		p.showAll = !p.showAll
		cur := ""
		if p.cursor < n {
			cur = p.entries[p.cursor].name
		}
		p.load(p.dir, cur)
	case ":":
		p.open = false
		m.jobs.files.uploadPrint = p.print
		m.startFmEdit(fmUpload, fmRow{}, p.dir+string(filepath.Separator))
	case "right", "l", "enter":
		if n == 0 {
			return nil
		}
		e := p.entries[min(p.cursor, n-1)]
		if e.dir {
			p.load(filepath.Join(p.dir, e.name), "")
			return nil
		}
		if msg.String() != "enter" {
			return nil
		}
		p.open = false
		return m.uploadLocal(filepath.Join(p.dir, e.name), p.print)
	}
	return nil
}

// uploadLocal uploads a local file into the current folder; with print,
// after a confirmation, Moonraker starts printing it once it's there.
func (m *model) uploadLocal(local string, print bool) tea.Cmd {
	f := &m.jobs.files
	c := m.client
	fi, err := os.Stat(local)
	if err != nil {
		f.status = "!! " + err.Error()
		return nil
	}
	if fi.IsDir() {
		f.status = "!! " + local + " is a folder"
		return nil
	}
	lastUploadDir = filepath.Dir(local)
	name := filepath.Base(local)
	dest := relPath(f.dir, name)
	size := humanBytes(fi.Size())
	run := func() tea.Cmd {
		m.appendLog(">>> upload " + local + " → " + dest)
		f.status = fmt.Sprintf("uploading %s (%s)…", name, size)
		return func() tea.Msg {
			res, err := c.UploadLocal("gcodes", dest, local, print)
			what := fmt.Sprintf("uploaded %s (%s)", name, size)
			switch {
			case res.PrintStarted:
				what += " — printing"
			case res.PrintQueued:
				what += " — added to the job queue"
			case print && err == nil:
				what += " — the print didn't start (is the printer ready?)"
			}
			return fileOpMsg{what: what, err: err, selectAfter: name}
		}
	}
	if !print {
		return run()
	}
	if jobActive(m.job.State) {
		return m.refuse("a print is already running; u uploads without printing")
	}
	m.confirmation = &pendingConfirmation{prompt: fmt.Sprintf("Upload %s (%s) and print it?", name, size), onYes: run()}
	return nil
}

func (m model) renderPicker(w, rowsFit int) []string {
	p := m.jobs.files.pick
	what := "Upload from this computer"
	if p.print {
		what = "Upload and print from this computer"
	}
	filter := "gcode files"
	if p.showAll {
		filter = "all files"
	}
	lines := []string{
		clipWidth(footerFocusStyle.Render(what+": ")+tildePath(p.dir)+consoleInfoStyle.Render("  ·  "+filter), w),
		"",
	}
	if p.err != "" {
		lines = append(lines, consoleErrStyle.Render(p.err))
	}
	var rows []string
	nameW := max(16, w-36)
	for i, e := range p.entries {
		mark, ns := "  ", footerText
		if i == p.cursor {
			mark, ns = setupCursorStyle.Render("▸ "), jogStepStyle
		}
		name, size := e.name, ""
		if e.dir {
			name += string(filepath.Separator)
			ns = ns.Bold(true)
		} else {
			size = humanBytes(e.size)
		}
		rows = append(rows, clipWidth(mark+ns.Render(fmt.Sprintf("%-*s", nameW, ellipsizeMiddle(name, nameW)))+
			consoleInfoStyle.Render(fmt.Sprintf("  %9s  %s", size, e.mod.Format("2006-01-02 15:04"))), w))
	}
	if len(rows) == 0 {
		rows = append(rows, consoleInfoStyle.Render("No gcode files here. . shows all files, ← goes up."))
	}
	if len(rows) > rowsFit {
		start := max(0, min(p.cursor-rowsFit+1, len(rows)-rowsFit))
		rows = rows[start : start+rowsFit]
	}
	return append(lines, rows...)
}

// tildePath shows a path under the home folder as ~/….
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return "~" + string(filepath.Separator) + rel
	}
	return p
}
