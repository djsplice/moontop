package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
)

// ---------------------------------------------------------------------------
// Phase 25: logs (L or F12) and config files (E)
//
// Logs: the files in Moonraker's logs root; the last half megabyte of
// one, with errors, warnings and tracebacks coloured. Scroll, follow
// (re-read every two seconds), filter (/), roll over (R).
//
// Config: the files in the config root; enter reads one (sections,
// keys and comments picked out), e edits it in $VISUAL / $EDITOR (the
// TUI steps aside while the editor runs), and a changed file is
// uploaded back, after which Moontop offers the restart that applies it.
// ---------------------------------------------------------------------------

const (
	logTailBytes = 512 << 10
	logMaxLines  = 4000
)

type logScreen struct {
	open    bool
	loaded  bool
	names   []string
	idx     int
	lines   []string
	cut     bool
	scroll  int // lines up from the bottom
	follow  bool
	filter  string
	err     string
	fetched time.Time
}

type logListMsg struct {
	names []string
	err   error
}

type logTextMsg struct {
	name string
	data []byte
	cut  bool
	err  error
}

type logTickMsg struct{}

func listLogsCmd(c *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		files, err := c.ListRoot("logs")
		var names []string
		for _, f := range files {
			names = append(names, f.Path)
		}
		return logListMsg{names: sortLogs(names), err: err}
	}
}

var rotated = regexp.MustCompile(`\.(\d+|gz|\d{4}-\d{2}-\d{2}.*)$`)

// sortLogs: current logs first (klippy.log, moonraker.log), then
// rotated ones.
func sortLogs(names []string) []string {
	rank := func(n string) int {
		switch {
		case n == "klippy.log":
			return 0
		case n == "moonraker.log":
			return 1
		case rotated.MatchString(n):
			return 3
		}
		return 2
	}
	sort.SliceStable(names, func(i, j int) bool {
		ri, rj := rank(names[i]), rank(names[j])
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	return names
}

func fetchLogCmd(c *moonraker.Client, name string) tea.Cmd {
	return func() tea.Msg {
		data, cut, err := c.FetchFile("logs", name, logTailBytes)
		return logTextMsg{name: name, data: data, cut: cut, err: err}
	}
}

func logTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return logTickMsg{} })
}

func (m *model) openLogs() tea.Cmd {
	m.logs.open = true
	return listLogsCmd(m.client)
}

func (m *model) currentLog() string {
	if len(m.logs.names) == 0 {
		return ""
	}
	return m.logs.names[min(m.logs.idx, len(m.logs.names)-1)]
}

func (m *model) onLogList(msg logListMsg) tea.Cmd {
	l := &m.logs
	l.loaded = true
	if msg.err != nil {
		l.err = msg.err.Error()
		return nil
	}
	l.names, l.err = msg.names, ""
	if name := m.currentLog(); name != "" {
		return fetchLogCmd(m.client, name)
	}
	return nil
}

func (m *model) onLogText(msg logTextMsg) {
	l := &m.logs
	if msg.name != m.currentLog() {
		return // switched away while it loaded
	}
	if msg.err != nil {
		l.err = msg.err.Error()
		return
	}
	text := strings.ReplaceAll(string(msg.data), "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if msg.cut && len(lines) > 1 {
		lines = lines[1:] // the first line was cut mid-way
	}
	if len(lines) > logMaxLines {
		lines, msg.cut = lines[len(lines)-logMaxLines:], true
	}
	l.lines, l.cut, l.err, l.fetched = lines, msg.cut, "", time.Now()
}

// shownLogLines applies the filter.
func (m *model) shownLogLines() []string {
	f := strings.ToLower(m.logs.filter)
	if f == "" {
		return m.logs.lines
	}
	var out []string
	for _, l := range m.logs.lines {
		if strings.Contains(strings.ToLower(l), f) {
			out = append(out, l)
		}
	}
	return out
}

func (m *model) logBodyHeight() int { return max(3, m.height-7) }

func (m *model) handleLogsKey(msg tea.KeyMsg) tea.Cmd {
	l := &m.logs
	n := len(m.shownLogLines())
	page := m.logBodyHeight()
	switch msg.String() {
	case "esc", "q":
		if l.filter != "" {
			l.filter = ""
			return nil
		}
		l.open = false
	case "up", "k":
		l.scroll = min(l.scroll+1, max(0, n-page))
	case "down", "j":
		l.scroll = max(0, l.scroll-1)
	case "pgup", "b":
		l.scroll = min(l.scroll+page, max(0, n-page))
	case "pgdown", " ":
		l.scroll = max(0, l.scroll-page)
	case "home", "g":
		l.scroll = max(0, n-page)
	case "end", "G":
		l.scroll = 0
	case "tab", "right", "l":
		if len(l.names) > 0 {
			l.idx, l.scroll, l.lines = (l.idx+1)%len(l.names), 0, nil
			return fetchLogCmd(m.client, m.currentLog())
		}
	case "shift+tab", "left", "h":
		if len(l.names) > 0 {
			l.idx, l.scroll, l.lines = (l.idx+len(l.names)-1)%len(l.names), 0, nil
			return fetchLogCmd(m.client, m.currentLog())
		}
	case "r":
		return fetchLogCmd(m.client, m.currentLog())
	case "f":
		l.follow = !l.follow
		if l.follow {
			l.scroll = 0
			return tea.Batch(fetchLogCmd(m.client, m.currentLog()), logTick())
		}
	case "/":
		m.openLineEdit("Show lines containing:", l.filter, func(m *model, v string) (tea.Cmd, error) {
			m.logs.filter, m.logs.scroll = strings.TrimSpace(v), 0
			return nil, nil
		})
	case "R":
		app := map[string]string{"klippy.log": "klipper", "moonraker.log": "moonraker"}[m.currentLog()]
		what := app
		if what == "" {
			what = "all applications"
		}
		c := m.client
		m.confirmation = &pendingConfirmation{prompt: "Roll over the logs for " + what + "? (New files start; the old ones are kept.)",
			onYes: func() tea.Msg {
				if err := c.LogRollover(app); err != nil {
					return machineActionMsg{what: "log rollover", err: err}
				}
				return listLogsCmd(c)()
			}}
	}
	return nil
}

func (m *model) onLogTick() tea.Cmd {
	if !m.logs.open || !m.logs.follow {
		return nil
	}
	return tea.Batch(fetchLogCmd(m.client, m.currentLog()), logTick())
}

var (
	logErrorRe = regexp.MustCompile(`(?i)\b(error|exception|traceback|shutdown|failed|!!)\b|^\s+File "`)
	logWarnRe  = regexp.MustCompile(`(?i)\bwarn(ing)?\b`)
	logStatsRe = regexp.MustCompile(`^Stats \d`)
)

// logLineKind classifies a log line: error, warning, stats or plain.
func logLineKind(l string) string {
	switch {
	case logErrorRe.MatchString(l):
		return "error"
	case logWarnRe.MatchString(l):
		return "warning"
	case logStatsRe.MatchString(l):
		return "stats"
	}
	return ""
}

func styleLogLine(l string) string {
	switch logLineKind(l) {
	case "error":
		return consoleErrStyle.Render(l)
	case "warning":
		return consoleCmdStyle.Render(l)
	case "stats":
		return consoleInfoStyle.Render(l)
	}
	return l
}

func (m model) renderLogs() string {
	l := m.logs
	name := m.currentLog()
	head := appTitleStyle.Render(appTitle + "  ·  Logs")
	if name != "" {
		head += "   " + jogStepStyle.Render(name) + consoleInfoStyle.Render(fmt.Sprintf("  (%d of %d, ←→ switch)", l.idx+1, len(l.names)))
	}
	if l.follow {
		head += "   " + consoleNoticeStyle.Render("● following")
	}
	w := m.width - 4
	var body []string
	switch {
	case !l.loaded:
		body = []string{consoleInfoStyle.Render("Loading…")}
	case l.err != "":
		body = []string{consoleErrStyle.Render("!! " + l.err)}
	case name == "":
		body = []string{consoleInfoStyle.Render("No log files.")}
	default:
		lines := m.shownLogLines()
		h := m.logBodyHeight()
		end := max(0, len(lines)-l.scroll)
		start := max(0, end-h)
		for _, ln := range lines[start:end] {
			body = append(body, styleLogLine(clipWidth(ln, w)))
		}
		status := fmt.Sprintf("%d lines", len(lines))
		if l.cut {
			status += " (the end of the file)"
		}
		if l.filter != "" {
			status += fmt.Sprintf("  ·  containing %q (esc clears)", l.filter)
		}
		if l.scroll > 0 {
			status += fmt.Sprintf("  ·  %d lines above the end", l.scroll)
		}
		body = append(body, consoleInfoStyle.Render(status))
	}
	out := head + "\n\n" + bodyStyle.Render(strings.Join(body, "\n")) + "\n"
	switch {
	case m.lineEdit != nil:
		out += renderLineEdit(m.lineEdit, m.width) + "\n"
	case m.confirmation != nil:
		out += renderPrompt(m.confirmation, m.width) + "\n"
	}
	return out + clipWidth(footerStyle.Render("↑↓ PgUp PgDn scroll  •  g G top / end  •  f follow  •  / filter  •  r reload  •  R roll over  •  esc close"), m.width) + "\n"
}

// --- config files -------------------------------------------------------------

type cfgScreen struct {
	open    bool
	loaded  bool
	files   []string
	cursor  int
	viewing string
	lines   []string
	scroll  int
	err     string
}

type cfgListMsg struct {
	files []string
	err   error
}

type cfgTextMsg struct {
	path string
	data []byte
	err  error
}

// editorDoneMsg: the editor exited; tmp holds the (maybe) edited file.
type editorDoneMsg struct {
	path, tmp string
	before    []byte
	err       error
}

type cfgUploadedMsg struct {
	path string
	err  error
}

func listCfgCmd(c *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		files, err := c.ListRoot("config")
		var names []string
		for _, f := range files {
			names = append(names, f.Path)
		}
		sort.SliceStable(names, func(i, j int) bool {
			if (names[i] == "printer.cfg") != (names[j] == "printer.cfg") {
				return names[i] == "printer.cfg"
			}
			return names[i] < names[j]
		})
		return cfgListMsg{files: names, err: err}
	}
}

func fetchCfgCmd(c *moonraker.Client, path string) tea.Cmd {
	return func() tea.Msg {
		data, _, err := c.FetchFile("config", path, 0)
		return cfgTextMsg{path: path, data: data, err: err}
	}
}

func (m *model) openConfig() tea.Cmd {
	m.cfgFiles.open = true
	return listCfgCmd(m.client)
}

// editorCommand builds the editor invocation: $VISUAL, $EDITOR, vi.
var editorCommand = func(path string) *exec.Cmd {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "vi"
	}
	parts := strings.Fields(ed) // "code -w"
	return exec.Command(parts[0], append(parts[1:], path)...)
}

// editCfgCmd downloads a config file to a temp file and hands the
// terminal to the editor; editorDoneMsg follows.
func (m *model) editCfgCmd(path string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		data, _, err := c.FetchFile("config", path, 0)
		if err != nil {
			return editorDoneMsg{path: path, err: err}
		}
		dir, err := os.MkdirTemp("", "moontop-edit-")
		if err != nil {
			return editorDoneMsg{path: path, err: err}
		}
		tmp := filepath.Join(dir, filepath.Base(path)) // keeps the extension for syntax highlighting
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return editorDoneMsg{path: path, err: err}
		}
		return startEditorMsg{path: path, tmp: tmp, before: data}
	}
}

type startEditorMsg struct {
	path, tmp string
	before    []byte
}

func (m *model) onStartEditor(msg startEditorMsg) tea.Cmd {
	m.appendLog("** editing " + msg.path + " in " + editorCommand(msg.tmp).Args[0])
	return tea.ExecProcess(editorCommand(msg.tmp), func(err error) tea.Msg {
		return editorDoneMsg{path: msg.path, tmp: msg.tmp, before: msg.before, err: err}
	})
}

// onEditorDone uploads the file if it changed.
func (m *model) onEditorDone(msg editorDoneMsg) tea.Cmd {
	if msg.tmp != "" {
		defer os.RemoveAll(filepath.Dir(msg.tmp))
	}
	if msg.err != nil {
		m.cfgFiles.err = "edit " + msg.path + ": " + msg.err.Error()
		return nil
	}
	after, err := os.ReadFile(msg.tmp)
	if err != nil {
		m.cfgFiles.err = err.Error()
		return nil
	}
	if string(after) == string(msg.before) {
		m.appendLog("** " + msg.path + ": no changes")
		return nil
	}
	c := m.client
	return m.requireConnected("upload", func() tea.Msg {
		return cfgUploadedMsg{path: msg.path, err: c.UploadFile("config", msg.path, after)}
	})
}

// onCfgUploaded refreshes the view and offers the restart that applies
// the change.
func (m *model) onCfgUploaded(msg cfgUploadedMsg) tea.Cmd {
	if msg.err != nil {
		m.cfgFiles.err = msg.err.Error()
		m.appendLog("!! " + msg.err.Error())
		return nil
	}
	m.appendLog("** saved " + msg.path)
	c := m.client
	switch {
	case strings.HasSuffix(msg.path, "moonraker.conf"):
		m.confirmation = &pendingConfirmation{prompt: "Saved " + msg.path + ". Restart Moonraker to apply it?",
			onYes: machineAction("restart moonraker", func() error { return c.ServiceAction("restart", "moonraker") })}
	case strings.HasSuffix(msg.path, ".cfg") && !jobActive(m.job.State):
		m.confirmation = &pendingConfirmation{prompt: "Saved " + msg.path + ". Restart Klipper (RESTART) to apply it?",
			onYes: m.restartKlipper()}
	}
	if m.cfgFiles.viewing == msg.path {
		return fetchCfgCmd(c, msg.path)
	}
	return nil
}

func (m *model) handleConfigKey(msg tea.KeyMsg) tea.Cmd {
	f := &m.cfgFiles
	if f.viewing != "" {
		page := max(3, m.height-6)
		switch msg.String() {
		case "esc", "q":
			f.viewing, f.lines = "", nil
		case "up", "k":
			f.scroll = max(0, f.scroll-1)
		case "down", "j":
			f.scroll = min(f.scroll+1, max(0, len(f.lines)-page))
		case "pgup", "b":
			f.scroll = max(0, f.scroll-page)
		case "pgdown", " ":
			f.scroll = min(f.scroll+page, max(0, len(f.lines)-page))
		case "g", "home":
			f.scroll = 0
		case "G", "end":
			f.scroll = max(0, len(f.lines)-page)
		case "e":
			return m.editCfgCmd(f.viewing)
		}
		return nil
	}
	switch msg.String() {
	case "esc", "q":
		f.open = false
	case "up", "k":
		if len(f.files) > 0 {
			f.cursor = (f.cursor + len(f.files) - 1) % len(f.files)
		}
	case "down", "j":
		if len(f.files) > 0 {
			f.cursor = (f.cursor + 1) % len(f.files)
		}
	case "enter":
		if len(f.files) > 0 {
			f.viewing, f.scroll, f.lines = f.files[f.cursor], 0, nil
			return fetchCfgCmd(m.client, f.viewing)
		}
	case "e":
		if len(f.files) > 0 {
			return m.editCfgCmd(f.files[f.cursor])
		}
	case "r":
		return listCfgCmd(m.client)
	}
	return nil
}

var (
	cfgSectionRe = regexp.MustCompile(`^\s*\[[^\]]+\]`)
	cfgKeyRe     = regexp.MustCompile(`^(\s*[A-Za-z0-9_.\-]+\s*)([:=])(.*)$`)
)

func styleCfgLine(l string) string {
	t := strings.TrimSpace(l)
	switch {
	case strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";"):
		return consoleInfoStyle.Render(l)
	case cfgSectionRe.MatchString(l):
		return helpGroupStyle.Render(l)
	}
	if g := cfgKeyRe.FindStringSubmatch(l); g != nil && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
		return jogStepStyle.Render(g[1]) + g[2] + g[3]
	}
	return l
}

func (m model) renderConfig() string {
	f := m.cfgFiles
	w := m.width - 4
	head := appTitleStyle.Render(appTitle + "  ·  Config")
	var body []string
	var foot string
	switch {
	case f.err != "":
		body = append(body, consoleErrStyle.Render("!! "+f.err), "")
		fallthrough
	default:
		if f.viewing != "" {
			head += "   " + jogStepStyle.Render(f.viewing)
			h := max(3, m.height-6)
			end := min(len(f.lines), f.scroll+h)
			for i := f.scroll; i < end; i++ {
				body = append(body, clipWidth(consoleInfoStyle.Render(fmt.Sprintf("%4d ", i+1))+styleCfgLine(f.lines[i]), w))
			}
			if f.lines == nil {
				body = append(body, consoleInfoStyle.Render("Loading…"))
			}
			foot = "↑↓ PgUp PgDn scroll  •  e edit in $EDITOR  •  esc back"
			break
		}
		if !f.loaded {
			body = append(body, consoleInfoStyle.Render("Loading…"))
		}
		for i, p := range f.files {
			mark, ns := "  ", footerText
			if i == f.cursor {
				mark, ns = setupCursorStyle.Render("▸ "), jogStepStyle
			}
			body = append(body, mark+ns.Render(p))
		}
		if fit := max(3, m.height-6); len(body) > fit {
			start := max(0, min(f.cursor-fit/2, len(body)-fit))
			body = body[start : start+fit]
		}
		foot = "↑↓ select  •  enter read  •  e edit in $EDITOR  •  r reload  •  esc close"
	}
	out := head + "\n\n" + bodyStyle.Render(strings.Join(body, "\n")) + "\n\n"
	if m.confirmation != nil {
		out += renderPrompt(m.confirmation, m.width) + "\n"
	}
	return out + clipWidth(footerStyle.Render(foot), m.width) + "\n"
}

func (m *model) onCfgList(msg cfgListMsg) {
	f := &m.cfgFiles
	f.loaded = true
	if msg.err != nil {
		f.err = msg.err.Error()
		return
	}
	f.files, f.err = msg.files, ""
	f.cursor = min(f.cursor, max(0, len(f.files)-1))
}

func (m *model) onCfgText(msg cfgTextMsg) {
	f := &m.cfgFiles
	if msg.path != f.viewing {
		return
	}
	if msg.err != nil {
		f.err = msg.err.Error()
		return
	}
	f.lines = strings.Split(strings.TrimRight(strings.ReplaceAll(string(msg.data), "\r\n", "\n"), "\n"), "\n")
	f.err = ""
}

// --- console completion and macro hints -----------------------------------------

// completeConsole accepts the console's completion (tab), when there's
// one to accept; otherwise tab moves on to the next pane as usual.
func (m *model) completeConsole() bool {
	s := m.input.CurrentSuggestion()
	v := m.input.Value()
	if s == "" || v == "" || strings.EqualFold(s, v) || strings.Contains(v, " ") {
		return false
	}
	m.input.SetValue(s)
	m.input.CursorEnd()
	return true
}

// consoleHint is the parameters of the macro being typed that aren't
// typed yet, with defaults: "BED=60 EXTRUDER=210 CHAMBER=".
func (m *model) consoleHint() string {
	fields := strings.Fields(m.input.Value())
	if len(fields) == 0 {
		return ""
	}
	name := strings.ToUpper(fields[0])
	typed := map[string]bool{}
	for _, f := range fields[1:] {
		if k, _, ok := strings.Cut(f, "="); ok {
			typed[strings.ToUpper(k)] = true
		}
	}
	for _, mc := range m.macros {
		if mc.Name != name {
			continue
		}
		var parts []string
		for _, p := range mc.Params {
			if !typed[p.Name] {
				parts = append(parts, p.Name+"="+p.Default)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func (m model) renderConsoleInput() string { return m.input.View() }

// --- endstops (Toolhead e) ---------------------------------------------------------

type endstopsMsg struct {
	states map[string]string
	err    error
}

func queryEndstopsCmd(c *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		s, err := c.QueryEndstops()
		return endstopsMsg{states: s, err: err}
	}
}

func (m *model) onEndstops(msg endstopsMsg) {
	if msg.err != nil {
		m.appendLog("!! endstops: " + msg.err.Error())
		return
	}
	var names []string
	for n := range msg.states {
		names = append(names, n)
	}
	sort.Strings(names)
	var parts []string
	for _, n := range names {
		parts = append(parts, n+" "+msg.states[n])
	}
	m.appendLog("// endstops: " + strings.Join(parts, " · "))
}
