package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moontop/internal/moonraker"
)

// ---------------------------------------------------------------------------
// Phase 20: the file manager — the Files tab of the Jobs screen
//
// One directory of the gcodes root at a time, with the metadata
// Moonraker parsed from each file (estimate, filament, layer height,
// slicer) as columns and, on wide terminals, a details column for the
// selected file. Folders open with enter or →, ← or backspace goes up.
// Files can be printed, queued, renamed, copied, moved, deleted and
// uploaded from this computer; folders created, renamed, moved and
// deleted. notify_filelist_changed keeps both this tab and the Files
// pane current, whoever changed the files.
// ---------------------------------------------------------------------------

type fmMode int

const (
	fmNone fmMode = iota
	fmRename
	fmMkdir
	fmMove
	fmCopy
	fmUpload
)

var fmPrompts = map[fmMode]string{
	fmRename: "Rename to",
	fmMkdir:  "New folder name",
	fmMove:   "Move to folder (path in gcodes; empty = the top)",
	fmCopy:   "Copy as",
	fmUpload: "Upload a file from this computer (path)",
}

const (
	fmSortModified = iota
	fmSortName
	fmSortSize
)

var fmSortNames = []string{"newest first", "name", "largest first"}

type fileManager struct {
	dir     string // relative to the gcodes root; "" is the top
	listing moonraker.Directory
	loaded  bool
	err     string
	cursor  int
	sortBy  int
	status  string

	mode   fmMode
	input  textinput.Model
	target fmRow // the row an edit applies to

	selectName string // select this entry after the next load
}

// fmRow is one line of the listing: the parent link, a folder or a file.
type fmRow struct {
	name string
	up   bool
	dir  *moonraker.DirEntry
	file *moonraker.DirFile
}

type dirLoadedMsg struct {
	dir     string
	listing moonraker.Directory
	err     error
}

type fileOpMsg struct {
	what        string
	err         error
	selectAfter string
}

func gcodesPath(dir, name string) string {
	return path.Join("gcodes", dir, name)
}

// relPath is a path relative to the gcodes root, as printing and the
// job queue want it.
func relPath(dir, name string) string {
	return strings.TrimPrefix(path.Join(dir, name), "/")
}

func loadDirCmd(c *moonraker.Client, dir string) tea.Cmd {
	return func() tea.Msg {
		l, err := c.GetDirectory(path.Join("gcodes", dir))
		return dirLoadedMsg{dir: dir, listing: l, err: err}
	}
}

func fileOp(what, selectAfter string, f func() error) tea.Cmd {
	return func() tea.Msg { return fileOpMsg{what: what, err: f(), selectAfter: selectAfter} }
}

func (f *fileManager) rows() []fmRow {
	var rows []fmRow
	if f.dir != "" {
		rows = append(rows, fmRow{name: "..", up: true})
	}
	dirs := append([]moonraker.DirEntry(nil), f.listing.Dirs...)
	sort.SliceStable(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	for i := range dirs {
		if strings.HasPrefix(dirs[i].Name, ".") {
			continue
		}
		rows = append(rows, fmRow{name: dirs[i].Name, dir: &dirs[i]})
	}
	files := append([]moonraker.DirFile(nil), f.listing.Files...)
	sort.SliceStable(files, func(i, j int) bool {
		a, b := files[i], files[j]
		switch f.sortBy {
		case fmSortName:
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case fmSortSize:
			return a.Size > b.Size
		}
		return a.Modified > b.Modified
	})
	for i := range files {
		if strings.HasPrefix(files[i].Name, ".") {
			continue
		}
		rows = append(rows, fmRow{name: files[i].Name, file: &files[i]})
	}
	return rows
}

func (f *fileManager) selected() (fmRow, bool) {
	rows := f.rows()
	if len(rows) == 0 {
		return fmRow{}, false
	}
	return rows[min(f.cursor, len(rows)-1)], true
}

// openFileManager shows the Files tab at dir, selecting name.
func (m *model) openFileManager(dir, name string) tea.Cmd {
	m.jobs.open = true
	m.jobs.tab = jobsFiles
	f := &m.jobs.files
	if dir != f.dir || !f.loaded {
		f.dir, f.cursor, f.loaded = dir, 0, false
	}
	f.selectName = name
	return tea.Batch(loadJobsCmd(m.client), loadDirCmd(m.client, dir))
}

func (m *model) onDirLoaded(msg dirLoadedMsg) {
	f := &m.jobs.files
	if msg.dir != f.dir {
		return // a stale load for a folder we've left
	}
	f.loaded = true
	f.err = ""
	if msg.err != nil {
		f.err = msg.err.Error()
		f.listing = moonraker.Directory{}
		return
	}
	f.listing = msg.listing
	rows := f.rows()
	if f.selectName != "" {
		for i, r := range rows {
			if r.name == f.selectName {
				f.cursor = i
			}
		}
		f.selectName = ""
	}
	f.cursor = min(f.cursor, max(0, len(rows)-1))
}

func (m *model) onFileOp(msg fileOpMsg) tea.Cmd {
	f := &m.jobs.files
	if msg.err != nil {
		f.status = "!! " + msg.what + ": " + msg.err.Error()
		m.appendLog("!! " + msg.what + ": " + msg.err.Error())
		return nil
	}
	f.status = msg.what
	if msg.selectAfter != "" {
		f.selectName = msg.selectAfter
	}
	return tea.Batch(loadDirCmd(m.client, f.dir), m.loadFiles())
}

// onFileListChanged keeps the Files pane and, when open, the file
// manager current.
func (m *model) onFileListChanged(params []json.RawMessage) tea.Cmd {
	ch, ok := moonraker.ParseFileListChange(params)
	if ok && ch.Item.Root != "" && ch.Item.Root != "gcodes" {
		return nil
	}
	cmds := []tea.Cmd{m.loadFiles()}
	if m.jobs.open {
		cmds = append(cmds, loadDirCmd(m.client, m.jobs.files.dir))
	}
	return tea.Batch(cmds...)
}

func (m *model) startFmEdit(mode fmMode, target fmRow, initial string) {
	f := &m.jobs.files
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 512
	ti.Width = max(20, m.width-12)
	ti.PlaceholderStyle = consoleInfoStyle
	ti.SetValue(initial)
	ti.CursorEnd()
	ti.Focus()
	f.mode, f.input, f.target, f.status = mode, ti, target, ""
}

// handleFileMgrKey handles the Files tab. done is false for keys the
// Jobs screen handles (tab switching, closing).
func (m *model) handleFileMgrKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	f := &m.jobs.files
	c := m.client
	if f.mode != fmNone {
		switch msg.String() {
		case "esc":
			f.mode = fmNone
			return nil, true
		case "enter":
			v := strings.TrimSpace(f.input.Value())
			mode := f.mode
			f.mode = fmNone
			return m.applyFmEdit(mode, v), true
		}
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		return cmd, true
	}

	rows := f.rows()
	n := len(rows)
	cur, ok := f.selected()
	up := func() tea.Cmd {
		if f.dir == "" {
			return nil
		}
		from := path.Base(f.dir)
		f.dir = strings.TrimPrefix(path.Dir(f.dir), ".")
		f.cursor, f.loaded, f.selectName, f.status = 0, false, from, ""
		return loadDirCmd(c, f.dir)
	}
	into := func(name string) tea.Cmd {
		f.dir = relPath(f.dir, name)
		f.cursor, f.loaded, f.status = 0, false, ""
		return loadDirCmd(c, f.dir)
	}

	switch msg.String() {
	case "up", "k":
		if n > 0 {
			f.cursor = (min(f.cursor, n-1) + n - 1) % n
		}
		return nil, true
	case "down", "j":
		if n > 0 {
			f.cursor = (min(f.cursor, n-1) + 1) % n
		}
		return nil, true
	case "pgup":
		f.cursor = max(0, f.cursor-10)
		return nil, true
	case "pgdown":
		f.cursor = min(max(0, n-1), f.cursor+10)
		return nil, true
	case "home", "g":
		f.cursor = 0
		return nil, true
	case "end", "G":
		f.cursor = max(0, n-1)
		return nil, true
	case "left", "h", "backspace":
		return up(), true
	case "right", "l":
		if ok && cur.dir != nil {
			return into(cur.name), true
		}
		return nil, true
	case "s":
		f.sortBy = (f.sortBy + 1) % len(fmSortNames)
		f.status = "sorted by " + fmSortNames[f.sortBy]
		return nil, true
	case "r":
		return loadDirCmd(c, f.dir), true
	case "n":
		m.startFmEdit(fmMkdir, fmRow{}, "")
		return nil, true
	case "u":
		m.startFmEdit(fmUpload, fmRow{}, "")
		return nil, true
	}
	if !ok {
		return nil, false
	}

	switch msg.String() {
	case "enter":
		switch {
		case cur.up:
			return up(), true
		case cur.dir != nil:
			return into(cur.name), true
		case jobActive(m.job.State):
			return m.refuse("a print is already running; a queues it instead"), true
		}
		name := relPath(f.dir, cur.name)
		m.confirmation = &pendingConfirmation{prompt: "Start print: " + name + "?", onYes: m.startPrint(name)}
		return nil, true
	case "a":
		if cur.file == nil {
			return nil, true
		}
		return m.queueFile(relPath(f.dir, cur.name)), true
	case "x", "delete":
		if cur.up {
			return nil, true
		}
		p := gcodesPath(f.dir, cur.name)
		if cur.dir != nil {
			m.confirmation = &pendingConfirmation{
				prompt: "Delete the folder " + relPath(f.dir, cur.name) + " and everything in it?",
				onYes:  fileOp("deleted "+cur.name+"/", "", func() error { return c.DeleteDirectory(p, true) }),
			}
		} else {
			m.confirmation = &pendingConfirmation{
				prompt: "Delete " + relPath(f.dir, cur.name) + "?",
				onYes:  fileOp("deleted "+cur.name, "", func() error { return c.DeleteFile(p) }),
			}
		}
		return nil, true
	case "R":
		if !cur.up {
			m.startFmEdit(fmRename, cur, cur.name)
		}
		return nil, true
	case "m":
		if !cur.up {
			m.startFmEdit(fmMove, cur, f.dir)
		}
		return nil, true
	case "C":
		if cur.file != nil {
			ext := path.Ext(cur.name)
			m.startFmEdit(fmCopy, cur, strings.TrimSuffix(cur.name, ext)+" copy"+ext)
		}
		return nil, true
	}
	return nil, false
}

// applyFmEdit runs the finished rename / new folder / move / copy /
// upload.
func (m *model) applyFmEdit(mode fmMode, v string) tea.Cmd {
	f := &m.jobs.files
	c := m.client
	t := f.target
	if v == "" && mode != fmMove {
		return nil
	}
	if mode != fmMove && mode != fmUpload && strings.ContainsAny(v, `/\`) {
		f.status = "!! a name can't contain / — use m to move it to another folder"
		return nil
	}
	switch mode {
	case fmMkdir:
		p := gcodesPath(f.dir, v)
		return fileOp("created "+v+"/", v, func() error { return c.CreateDirectory(p) })
	case fmRename:
		if v == t.name {
			return nil
		}
		src, dst := gcodesPath(f.dir, t.name), gcodesPath(f.dir, v)
		return fileOp("renamed "+t.name+" → "+v, v, func() error { return c.MoveFile(src, dst) })
	case fmCopy:
		src, dst := gcodesPath(f.dir, t.name), gcodesPath(f.dir, v)
		return fileOp("copied "+t.name+" → "+v, v, func() error { return c.CopyFile(src, dst) })
	case fmMove:
		folder := strings.Trim(path.Clean("/"+strings.ReplaceAll(v, `\`, "/")), "/")
		if folder == strings.Trim(f.dir, "/") {
			return nil
		}
		src, dst := gcodesPath(f.dir, t.name), gcodesPath(folder, t.name)
		where := "/" + folder
		if folder == "" {
			where = "the top"
		}
		return fileOp("moved "+t.name+" to "+where, "", func() error { return c.MoveFile(src, dst) })
	case fmUpload:
		local := expandHome(v)
		name := filepath.Base(local)
		dest := relPath(f.dir, name)
		m.appendLog(">>> upload " + local + " → " + dest)
		return fileOp("uploaded "+name, name, func() error {
			data, err := os.ReadFile(local)
			if err != nil {
				return err
			}
			return c.UploadFile("gcodes", dest, data)
		})
	}
	return nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// --- rendering ---------------------------------------------------------------

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func fmTime(t float64) string {
	if t <= 0 {
		return ""
	}
	return time.Unix(int64(t), 0).Format("2006-01-02 15:04")
}

// renderFileMgr is the Files tab body (lines) and its key hints.
func (m model) renderFileMgr(w, rowsFit int) ([]string, string) {
	f := m.jobs.files
	where := "/" + f.dir
	nDirs, nFiles := 0, 0
	for _, r := range f.rows() {
		switch {
		case r.dir != nil:
			nDirs++
		case r.file != nil:
			nFiles++
		}
	}
	head := footerFocusStyle.Render("gcodes"+strings.TrimSuffix(where, "/")) + consoleInfoStyle.Render(fmt.Sprintf(
		"  ·  %d folders, %d files  ·  %s", nDirs, nFiles, fmSortNames[f.sortBy]))
	if d := f.listing.Disk; d.Total > 0 {
		head += consoleInfoStyle.Render(fmt.Sprintf("  ·  %s free of %s", humanBytes(int64(d.Free)), humanBytes(int64(d.Total))))
	}
	lines := []string{clipWidth(head, w), ""}
	if !f.loaded {
		return append(lines, consoleInfoStyle.Render("Loading…")), ""
	}
	if f.err != "" {
		return append(lines, consoleErrStyle.Render(f.err)), ""
	}

	rows := f.rows()
	cur, _ := f.selected()
	detailW := 0
	if w >= 130 && cur.file != nil {
		detailW = 38
	}
	listW := w - detailW
	if detailW > 0 {
		listW -= 3
	}
	// name | size | modified | estimate | filament
	cols := []int{9, 17, 8, 9}
	if listW < 90 {
		cols = []int{9, 0, 8, 0}
	}
	fixed := 0
	for _, c := range cols {
		if c > 0 {
			fixed += c + 2
		}
	}
	nameW := max(12, listW-4-fixed)
	hdr := "  " + fmt.Sprintf("%-*s", nameW, "Name")
	for k, c := range cols {
		if c > 0 {
			hdr += "  " + fmt.Sprintf("%*s", c, []string{"Size", "Modified", "Estimate", "Filament"}[k])
		}
	}
	body := []string{consoleInfoStyle.Underline(true).Render(clipWidth(hdr, listW))}
	for i, r := range rows {
		mark, ns := "  ", footerText
		if i == min(f.cursor, len(rows)-1) {
			mark, ns = setupCursorStyle.Render("▸ "), jogStepStyle
		}
		name := r.name
		var size, mod, est, fil string
		switch {
		case r.up:
			name = "../"
		case r.dir != nil:
			name += "/"
			mod = fmTime(r.dir.Modified)
			ns = ns.Bold(true)
		default:
			size, mod = humanBytes(r.file.Size), fmTime(r.file.Modified)
			if float64(r.file.EstimatedTime) > 0 {
				est = shortDuration(float64(r.file.EstimatedTime))
			}
			fil = formatFilament(float64(r.file.FilamentTotal))
		}
		row := mark + ns.Render(fmt.Sprintf("%-*s", nameW, ellipsizeMiddle(name, nameW)))
		vals := []string{size, mod, est, fil}
		for k, c := range cols {
			if c > 0 {
				row += "  " + consoleInfoStyle.Render(fmt.Sprintf("%*s", c, vals[k]))
			}
		}
		body = append(body, clipWidth(row, listW))
	}
	if len(rows) == 0 {
		body = append(body, consoleInfoStyle.Render("Empty. u uploads a file from this computer, n makes a folder."))
	}
	if len(body)-1 > rowsFit {
		list := body[1:]
		start := max(0, min(f.cursor-rowsFit+1, len(list)-rowsFit))
		body = append(body[:1:1], list[start:start+rowsFit]...)
	}

	if detailW > 0 {
		det := fileDetails(*cur.file, f.dir)
		left := lipgloss.NewStyle().Width(listW).Render(strings.Join(body, "\n"))
		right := lipgloss.NewStyle().Width(detailW).BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).
			BorderForeground(colorBorderBlurred).PaddingLeft(1).Render(strings.Join(det, "\n"))
		lines = append(lines, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right), "\n")...)
	} else {
		lines = append(lines, body...)
		if cur.file != nil {
			if s := fileSummary(*cur.file); s != "" {
				lines = append(lines, "", consoleInfoStyle.Render(clipWidth(s, w)))
			}
		}
	}

	if f.mode != fmNone {
		lines = append(lines, "", footerFocusStyle.Render(fmPrompts[f.mode]+": ")+f.input.View())
	} else if f.status != "" {
		st := consoleNoticeStyle
		if strings.HasPrefix(f.status, "!!") {
			st = consoleErrStyle
		}
		lines = append(lines, "", st.Render(clipWidth(f.status, w)))
	}

	foot := "enter open / print  •  ← up  •  a queue  •  x delete  •  R rename  •  C copy  •  m move\n" +
		"n new folder  •  u upload  •  s sort  •  r refresh  •  tab history / queue  •  esc close"
	if f.mode != fmNone {
		foot = "enter apply  •  esc cancel"
	}
	return lines, foot
}

// fileDetails is the details column for a gcode file.
func fileDetails(fl moonraker.DirFile, dir string) []string {
	lab := func(k, v string) string {
		if v == "" {
			return ""
		}
		return consoleInfoStyle.Render(fmt.Sprintf("%-11s", k)) + v
	}
	mm := func(v float64) string {
		if v <= 0 {
			return ""
		}
		return fmt.Sprintf("%.2f mm", v)
	}
	var out []string
	add := func(s string) {
		if s != "" {
			out = append(out, s)
		}
	}
	out = append(out, footerFocusStyle.Render(ellipsizeMiddle(fl.Name, 36)), "")
	add(lab("Size", humanBytes(fl.Size)))
	add(lab("Modified", fmTime(fl.Modified)))
	if float64(fl.EstimatedTime) > 0 {
		add(lab("Estimate", shortDuration(float64(fl.EstimatedTime))))
	}
	fil := formatFilament(float64(fl.FilamentTotal))
	if float64(fl.FilamentWeightTotal) > 0 {
		fil += fmt.Sprintf(" · %.1f g", float64(fl.FilamentWeightTotal))
	}
	if string(fl.FilamentType) != "" {
		fil += " · " + string(fl.FilamentType)
	}
	add(lab("Filament", strings.TrimPrefix(fil, " · ")))
	layer := mm(float64(fl.LayerHeight))
	if float64(fl.FirstLayerHeight) > 0 && layer != "" {
		layer += fmt.Sprintf(" (first %.2f)", float64(fl.FirstLayerHeight))
	}
	add(lab("Layers", layer))
	add(lab("Height", mm(float64(fl.ObjectHeight))))
	add(lab("Nozzle", mm(float64(fl.NozzleDiameter))))
	if float64(fl.FirstLayerExtrTemp) > 0 || float64(fl.FirstLayerBedTemp) > 0 {
		add(lab("Temps", fmt.Sprintf("%.0f / %.0f °C", float64(fl.FirstLayerExtrTemp), float64(fl.FirstLayerBedTemp))))
	}
	add(lab("Slicer", strings.TrimSpace(string(fl.Slicer)+" "+string(fl.SlicerVersion))))
	if fl.PrintStartTime > 0 {
		add(lab("Last print", fmTime(float64(fl.PrintStartTime))))
	} else if string(fl.Slicer) != "" {
		add(lab("Last print", "never"))
	}
	if dir != "" {
		add(lab("Folder", "/"+dir))
	}
	return out
}

// fileSummary is one line about a file, for narrow terminals.
func fileSummary(fl moonraker.DirFile) string {
	var parts []string
	if float64(fl.EstimatedTime) > 0 {
		parts = append(parts, "estimate "+shortDuration(float64(fl.EstimatedTime)))
	}
	if s := formatFilament(float64(fl.FilamentTotal)); s != "" {
		parts = append(parts, s+" filament")
	}
	if float64(fl.LayerHeight) > 0 {
		parts = append(parts, fmt.Sprintf("%.2f mm layers", float64(fl.LayerHeight)))
	}
	if string(fl.Slicer) != "" {
		parts = append(parts, string(fl.Slicer))
	}
	return strings.Join(parts, " · ")
}
