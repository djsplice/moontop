package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moontop/internal/config"
)

// ---------------------------------------------------------------------------
// Setup screen (UI-2), modelled on htop's F2: categories on the left,
// the selected category's items on the right, changes apply and save
// immediately. Where a change is saved depends on what it is: display
// preferences go to the config file, per-printer settings (heater
// names, hidden heaters, presets) to the printer's Moonraker database.
// ---------------------------------------------------------------------------

type setupState struct {
	open     bool
	cat      int  // selected category
	item     int  // selected item in it
	inItems  bool // focus on the right-hand column
	editing  *setupEdit
	input    textinput.Model
	status   string // last save result or error
	pendingD bool   // "d" pressed once on a deletable item
}

// setupEdit is an in-progress text edit (rename, pattern, number…).
type setupEdit struct {
	prompt string
	apply  func(m *model, value string) (tea.Cmd, error)
}

// setupItem is one row on the right. Rows with neither toggle nor edit
// are informational.
type setupItem struct {
	label   string
	value   string // right-aligned text, e.g. "1500 mm/min"
	check   *bool  // non-nil: drawn as [x] / [ ]
	note    string // dimmed hint
	toggle  func(m *model) tea.Cmd
	edit    *setupEdit
	initial string // edit's starting text
	del     func(m *model) tea.Cmd
	move    func(m *model, delta int) tea.Cmd // [ / ] reorder
}

type setupCategory struct {
	name  string
	about string // one line under the items
	items func(m *model) []setupItem
	add   *setupEdit // "a" adds an item, when set
	addAs string     // label for the add action
}

func checked(v bool) *bool { return &v }

// setupCategories is filled in init: item functions read it back
// (via clampSetup), which a package-level initializer can't allow.
var setupCategories []setupCategory

func init() {
	setupCategories = []setupCategory{
		{name: "Panes", about: "space shows/hides · [ ] reorder · enter sets width (0 = natural) or console height. Saved to the config file.", items: panesItems},
		{name: "Columns", about: "Columns in Temperatures and Files; Temperatures drops State, then Power, when narrow. Saved to the config file.", items: columnsItems},
		{name: "Temperatures", about: "Hide sensors you don't need; enter renames (empty = default). Saved to the printer.", items: tempsItems},
		{name: "Console", about: "Lines matching a filter are hidden; ctrl+o shows them. Saved to the config file.", items: consoleItems,
			add: &setupEdit{prompt: "Filter pattern (Go regexp)", apply: addFilter}, addAs: "add a filter"},
		{name: "Toolhead", about: "Jog step sizes and feedrates. Saved to the config file.", items: toolheadItems},
		{name: "Extruder", about: "Commands for l (load) and u (unload) in Extruder; empty = detect from the printer's macros. Saved to the printer.", items: extruderItems},
		{name: "Safety", about: "Prompts before dangerous actions. Saved to the config file.", items: safetyItems},
		{name: "Presets", about: "Preheat presets for t in Temperatures. Moontop's are saved to the printer; Mainsail's are edited in Mainsail.", items: presetItems,
			add: &setupEdit{prompt: "Name for a preset of the current heater targets", apply: addPreset}, addAs: "save current targets as a preset"},
		{name: "Keys", about: "Every key, from the keymap registry. Remapping comes later.", items: keyItems},
	}
}

// --- persistence -----------------------------------------------------------

func (m *model) saveFile() tea.Cmd {
	m.applySettings()
	return saveFileCfgCmd(m.cfgPath, m.cfg)
}

func (m *model) savePrinter() tea.Cmd {
	m.applySettings()
	m.rebuildRows()
	if !m.connected() || !m.ready {
		return func() tea.Msg {
			return settingsSavedMsg{err: fmt.Errorf("not saved: the printer isn't connected")}
		}
	}
	return savePrinterCfgCmd(m.client, m.printerCfg)
}

// editTemps edits the effective heater settings and stores them on the
// printer. The first edit copies any file settings over, so they move
// to the printer rather than being silently replaced.
func (m *model) editTemps(f func(t *config.Temperatures)) tea.Cmd {
	t := m.temps()
	t.Hidden = append([]string(nil), t.Hidden...)
	if t.Rename != nil {
		r := make(map[string]string, len(t.Rename))
		for k, v := range t.Rename {
			r[k] = v
		}
		t.Rename = r
	}
	f(&t)
	m.printerCfg.Temperatures = t
	return m.savePrinter()
}

// --- categories --------------------------------------------------------------

func panesItems(m *model) []setupItem {
	var items []setupItem
	toggle := func(name string) func(m *model) tea.Cmd {
		return func(m *model) tea.Cmd {
			hide := !m.cfg.Panes.PaneHidden(name)
			if hide && m.visiblePaneCount() <= 1 {
				m.setup.status = "!! at least one pane has to stay visible"
				return nil
			}
			m.cfg.Panes.SetPaneHidden(name, hide)
			return m.saveFile()
		}
	}
	for _, name := range m.cfg.Panes.Ordered() {
		name := name
		f, _ := paneByName(name)
		w := m.cfg.Panes.WidthOf(name)
		value := "natural width"
		if w > 0 {
			value = fmt.Sprintf("width ×%d", w)
		}
		items = append(items, setupItem{
			label:   fmt.Sprintf("%d %s", paneNumber(f), name),
			check:   checked(!m.cfg.Panes.PaneHidden(name)),
			value:   value,
			toggle:  toggle(name),
			initial: strconv.Itoa(w),
			edit: &setupEdit{
				prompt: name + " width: its share of spare width (0 = natural width, just what it needs)",
				apply: func(m *model, v string) (tea.Cmd, error) {
					n, err := strconv.Atoi(strings.TrimSpace(v))
					if err != nil || n < 0 || n > 9 {
						return nil, fmt.Errorf("width is a whole number from 0 to 9")
					}
					m.cfg.Panes.SetWidth(name, n)
					return m.saveFile(), nil
				},
			},
			move: func(m *model, delta int) tea.Cmd {
				m.cfg.Panes.Move(name, delta)
				m.setup.item += delta
				m.clampSetup()
				return m.saveFile()
			},
		})
	}
	if m.afcModel() != nil {
		items = append(items, setupItem{
			label:  fmt.Sprintf("%d AFC", paneNumber(focusAFC)),
			note:   "beside the console",
			check:  checked(!m.cfg.Panes.PaneHidden("AFC")),
			toggle: toggle("AFC"),
		})
	}
	if len(m.macros) > 0 {
		items = append(items, setupItem{
			label:  fmt.Sprintf("%d Macros", paneNumber(focusMacros)),
			note:   "pinned macros; F3 lists all",
			check:  checked(!m.cfg.Panes.PaneHidden("Macros")),
			toggle: toggle("Macros"),
		})
	}
	items = append(items, setupItem{
		label:  fmt.Sprintf("%d Extruder", paneNumber(focusExtruder)),
		note:   "beside the console",
		check:  checked(!m.cfg.Panes.PaneHidden("Extruder")),
		toggle: toggle("Extruder"),
	})
	if len(m.outputs) > 0 {
		items = append(items, setupItem{
			label:  fmt.Sprintf("%d Outputs", paneNumber(focusOutputs)),
			note:   "under Extruder",
			check:  checked(!m.cfg.Panes.PaneHidden("Outputs")),
			toggle: toggle("Outputs"),
		})
	}
	pct := m.cfg.Panes.ConsolePercent()
	value := "height auto"
	if pct > 0 {
		value = fmt.Sprintf("height %d%%", pct)
	}
	items = append(items, setupItem{
		label:   fmt.Sprintf("%d Console", paneNumber(focusConsole)),
		note:    "always along the bottom",
		check:   checked(!m.cfg.Panes.PaneHidden("Console")),
		value:   value,
		toggle:  toggle("Console"),
		initial: strconv.Itoa(pct),
		edit: &setupEdit{
			prompt: "Console height in percent of the screen (0 = auto: everything the top row doesn't need)",
			apply: func(m *model, v string) (tea.Cmd, error) {
				n, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil || n < 0 || n > 85 || (n > 0 && n < 15) {
					return nil, fmt.Errorf("console height is 0 (auto) or 15 to 85")
				}
				if n == 0 {
					m.cfg.Panes.ConsoleHeight = nil
				} else {
					m.cfg.Panes.ConsoleHeight = config.Int(n)
				}
				return m.saveFile(), nil
			},
		},
	})
	return items
}

func columnsItems(m *model) []setupItem {
	c := &m.cfg.Columns
	var items []setupItem
	names := map[string]string{"state": "State", "current": "Current", "target": "Target", "power": "Power (heater duty)", "history": "History (sparkline, last 5 min)",
		"size": "Size", "modified": "Modified"}
	temps := c.Temps()
	for _, col := range config.TempColumns {
		col := col
		items = append(items, setupItem{
			label: "Temperatures: " + names[col],
			check: checked(contains(temps, col)),
			toggle: func(m *model) tea.Cmd {
				if !m.cfg.Columns.ToggleTemp(col) {
					m.setup.status = "!! Temperatures needs at least one column besides Name"
					return nil
				}
				return m.saveFile()
			},
		})
	}
	files := c.FileCols()
	for _, col := range config.FileColumns {
		col := col
		items = append(items, setupItem{
			label: "Files: " + names[col],
			check: checked(contains(files, col)),
			toggle: func(m *model) tea.Cmd {
				m.cfg.Columns.ToggleFile(col)
				return m.saveFile()
			},
		})
	}
	sortValue := "newest first"
	if c.SortByName() {
		sortValue = "by name"
	}
	items = append(items, setupItem{
		label: "Files: sort", value: sortValue, note: "space or enter switches",
		toggle: func(m *model) tea.Cmd {
			if m.cfg.Columns.SortByName() {
				m.cfg.Columns.FilesSort = ""
			} else {
				m.cfg.Columns.FilesSort = "name"
			}
			return m.saveFile()
		},
	})
	return items
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func tempsItems(m *model) []setupItem {
	t := m.temps()
	var items []setupItem
	for _, n := range m.sensorNames {
		n := n
		note := n
		if !canSetTarget(n) {
			note += " · sensor"
		}
		items = append(items, setupItem{
			label: m.label(n),
			check: checked(!t.IsHidden(n)),
			note:  note,
			toggle: func(m *model) tea.Cmd {
				return m.editTemps(func(t *config.Temperatures) { t.SetHidden(n, !t.IsHidden(n)) })
			},
			initial: t.Label(n),
			edit: &setupEdit{
				prompt: "Name for " + n + " (empty restores the default)",
				apply: func(m *model, v string) (tea.Cmd, error) {
					return m.editTemps(func(t *config.Temperatures) { t.SetLabel(n, strings.TrimSpace(v)) }), nil
				},
			},
		})
	}
	if len(items) == 0 {
		items = append(items, setupItem{label: "Waiting for the printer's heater list…"})
	}
	return items
}

func consoleItems(m *model) []setupItem {
	c := &m.cfg.Console
	items := []setupItem{
		{
			label: "Hide heat-wait temperature reports",
			note:  "B:26.3 /0.0 T0:168.1 /140.0 during M109 / M190",
			check: checked(c.WaitTempsHidden()),
			toggle: func(m *model) tea.Cmd {
				m.cfg.Console.HideWaitTemps = config.Bool(!m.cfg.Console.WaitTempsHidden())
				return m.saveFile()
			},
		},
		{
			label: "Hide TMC run current reports",
			note:  "// Run Current: 0.80A Hold Current: 0.80A",
			check: checked(c.RunCurrentHidden()),
			toggle: func(m *model) tea.Cmd {
				m.cfg.Console.HideRunCurrent = config.Bool(!m.cfg.Console.RunCurrentHidden())
				return m.saveFile()
			},
		},
	}
	for i, f := range c.Filters {
		i, f := i, f
		items = append(items, setupItem{
			label: f.Name,
			note:  "/" + f.Pattern + "/",
			check: checked(!f.Disabled),
			toggle: func(m *model) tea.Cmd {
				m.cfg.Console.Filters[i].Disabled = !m.cfg.Console.Filters[i].Disabled
				return m.saveFile()
			},
			del: func(m *model) tea.Cmd {
				fs := m.cfg.Console.Filters
				m.cfg.Console.Filters = append(fs[:i:i], fs[i+1:]...)
				if len(m.cfg.Console.Filters) == 0 {
					m.cfg.Console.Filters = nil
				}
				return m.saveFile()
			},
		})
	}
	return items
}

func addFilter(m *model, v string) (tea.Cmd, error) {
	v = strings.TrimSpace(v)
	if err := config.ValidateFilter(v); err != nil {
		return nil, err
	}
	m.cfg.Console.Filters = append(m.cfg.Console.Filters, config.Filter{Name: v, Pattern: v})
	return m.saveFile(), nil
}

func toolheadItems(m *model) []setupItem {
	t := &m.cfg.Toolhead
	steps := make([]string, len(t.Steps()))
	for i, s := range t.Steps() {
		steps[i] = strconv.FormatFloat(s, 'f', -1, 64)
	}
	stepText := strings.Join(steps, ", ")
	return []setupItem{
		{
			label: "Step sizes", value: stepText + " mm", initial: stepText,
			edit: &setupEdit{prompt: "Step sizes in mm, comma-separated", apply: func(m *model, v string) (tea.Cmd, error) {
				var out []float64
				for _, f := range strings.Split(v, ",") {
					f = strings.TrimSpace(f)
					if f == "" {
						continue
					}
					x, err := strconv.ParseFloat(f, 64)
					if err != nil || x <= 0 {
						return nil, fmt.Errorf("%q is not a positive number", f)
					}
					out = append(out, x)
				}
				if len(out) == 0 {
					m.cfg.Toolhead.StepSizes = nil // back to defaults
				} else {
					m.cfg.Toolhead.StepSizes = out
				}
				return m.saveFile(), nil
			}},
		},
		feedItem("XY feedrate", t.XY(), func(m *model) **int { return &m.cfg.Toolhead.XYFeedrate }),
		feedItem("Z feedrate", t.Z(), func(m *model) **int { return &m.cfg.Toolhead.ZFeedrate }),
	}
}

func feedItem(label string, cur int, field func(m *model) **int) setupItem {
	return setupItem{
		label: label, value: fmt.Sprintf("%d mm/min", cur), initial: strconv.Itoa(cur),
		edit: &setupEdit{prompt: label + " in mm/min (empty = default)", apply: func(m *model, v string) (tea.Cmd, error) {
			v = strings.TrimSpace(v)
			if v == "" {
				*field(m) = nil
				return m.saveFile(), nil
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("%q is not a positive whole number", v)
			}
			*field(m) = config.Int(n)
			return m.saveFile(), nil
		}},
	}
}

func safetyItems(m *model) []setupItem {
	s := &m.cfg.Safety
	return []setupItem{
		{
			label: "Confirm emergency stop", note: "off: ctrl+x stops the printer at once",
			check: checked(s.EStopConfirms()),
			toggle: func(m *model) tea.Cmd {
				m.cfg.Safety.ConfirmEStop = config.Bool(!m.cfg.Safety.EStopConfirms())
				return m.saveFile()
			},
		},
		{
			label: "Confirm restarts when idle", note: "during a print they always confirm",
			check: checked(s.RestartConfirmsIdle()),
			toggle: func(m *model) tea.Cmd {
				m.cfg.Safety.ConfirmRestartIdle = config.Bool(!m.cfg.Safety.RestartConfirmsIdle())
				return m.saveFile()
			},
		},
	}
}

func presetItems(m *model) []setupItem {
	var items []setupItem
	for i, p := range m.printerCfg.Presets {
		i := i
		pr := mergePresets([]config.Preset{p}, nil)[0]
		items = append(items, setupItem{
			label: p.Name,
			note:  presetSummary(pr),
			value: "Moontop",
			del: func(m *model) tea.Cmd {
				ps := m.printerCfg.Presets
				m.printerCfg.Presets = append(ps[:i:i], ps[i+1:]...)
				if len(m.printerCfg.Presets) == 0 {
					m.printerCfg.Presets = nil
				}
				return m.savePrinter()
			},
		})
	}
	for _, p := range m.mainsailPresets {
		items = append(items, setupItem{label: p.Name, note: presetSummary(p), value: "Mainsail"})
	}
	if len(items) == 0 {
		items = append(items, setupItem{label: "No presets yet. Set heater targets, then press a here."})
	}
	return items
}

func addPreset(m *model, v string) (tea.Cmd, error) {
	name := strings.TrimSpace(v)
	if name == "" {
		return nil, fmt.Errorf("a preset needs a name")
	}
	p, err := m.currentTargetsPreset(name)
	if err != nil {
		return nil, err
	}
	// Same name replaces.
	var out []config.Preset
	for _, q := range m.printerCfg.Presets {
		if q.Name != name {
			out = append(out, q)
		}
	}
	out = append(out, p)
	config.SortPresets(out)
	m.printerCfg.Presets = out
	return m.savePrinter(), nil
}

func keyItems(m *model) []setupItem {
	var items []setupItem
	for _, e := range registry {
		h := e.b.Help()
		items = append(items, setupItem{label: h.Key, note: e.scope.title() + " · " + h.Desc})
	}
	return items
}

// --- keys --------------------------------------------------------------------

func (m *model) openSetup() {
	m.setup.open = true
	m.setup.editing = nil
	m.setup.status = ""
	m.setup.pendingD = false
	if m.cfgPath == "" && m.setup.status == "" {
		m.setup.status = "!! the config file couldn't be read, so file settings won't be saved this session"
	}
}

func (m *model) currentItems() []setupItem {
	return setupCategories[m.setup.cat].items(m)
}

func (m *model) clampSetup() {
	n := len(m.currentItems())
	if m.setup.item >= n {
		m.setup.item = n - 1
	}
	if m.setup.item < 0 {
		m.setup.item = 0
	}
}

func (m *model) handleSetupKey(msg tea.KeyMsg) tea.Cmd {
	st := &m.setup
	if st.editing != nil {
		switch msg.String() {
		case "esc":
			st.editing = nil
			return nil
		case "enter":
			ed := st.editing
			cmd, err := ed.apply(m, st.input.Value())
			if err != nil {
				st.status = "!! " + err.Error()
				return nil // stay in the edit so it can be fixed
			}
			st.editing = nil
			m.clampSetup()
			return cmd
		}
		var cmd tea.Cmd
		st.input, cmd = st.input.Update(msg)
		return cmd
	}

	k := msg.String()
	if k != "d" {
		st.pendingD = false
	}
	cat := setupCategories[st.cat]
	switch {
	case key.Matches(msg, *keys.Setup), k == "q" && !st.inItems:
		st.open = false
		return nil
	case k == "esc":
		if st.inItems {
			st.inItems = false
		} else {
			st.open = false
		}
		return nil
	case k == "tab", k == "right", k == "l":
		if !st.inItems {
			st.inItems, st.item = true, 0
		}
		return nil
	case k == "shift+tab", k == "left", k == "h":
		st.inItems = false
		return nil
	case k == "up", k == "k":
		if st.inItems {
			st.item--
			m.clampSetup()
		} else if st.cat > 0 {
			st.cat--
			st.item = 0
		}
		return nil
	case k == "down", k == "j":
		if st.inItems {
			st.item++
			m.clampSetup()
		} else if st.cat < len(setupCategories)-1 {
			st.cat++
			st.item = 0
		}
		return nil
	case k == "a" && cat.add != nil:
		m.beginEdit(cat.add, "")
		return nil
	}
	if !st.inItems {
		if k == "enter" {
			st.inItems, st.item = true, 0
		}
		return nil
	}

	items := m.currentItems()
	if st.item >= len(items) {
		return nil
	}
	it := items[st.item]
	switch k {
	case " ":
		if it.toggle != nil {
			return it.toggle(m)
		}
	case "enter":
		switch {
		case it.edit != nil:
			m.beginEdit(it.edit, it.initial)
		case it.toggle != nil:
			return it.toggle(m)
		}
	case "[", "]":
		if it.move != nil {
			d := 1
			if k == "[" {
				d = -1
			}
			return it.move(m, d)
		}
	case "d":
		if it.del == nil {
			return nil
		}
		if !st.pendingD {
			st.pendingD = true
			st.status = "press d again to delete " + it.label
			return nil
		}
		st.pendingD = false
		cmd := it.del(m)
		m.clampSetup()
		return cmd
	}
	return nil
}

func (m *model) beginEdit(e *setupEdit, initial string) {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 200
	ti.Width = 40
	ti.SetValue(initial)
	ti.CursorEnd()
	ti.Focus()
	m.setup.input = ti
	m.setup.editing = e
	m.setup.status = ""
}

// --- rendering -----------------------------------------------------------------

func (m model) renderSetup() string {
	st := m.setup
	w := m.width
	if w < 60 {
		w = 60
	}
	leftW := 16
	for _, c := range setupCategories {
		if len(c.name)+4 > leftW {
			leftW = len(c.name) + 4
		}
	}
	rightW := w - leftW - 4 - 4 - 2
	// Inside a panel, Padding(0,1) leaves width-2 columns for content.
	// Every line is clipped to that: a line that wrapped would make the
	// screen taller than the terminal and scroll the title away.
	leftCW, rightCW := leftW-2, rightW-2

	// Left: categories.
	var left []string
	for i, c := range setupCategories {
		line := "  " + c.name
		switch {
		case i == st.cat && !st.inItems:
			line = setupCursorStyle.Render("▸ " + c.name)
		case i == st.cat:
			line = helpGroupStyle.Render("▸ " + c.name)
		}
		left = append(left, line)
	}

	// Right: items, scrolled to keep the cursor in view.
	cat := setupCategories[st.cat]
	items := cat.items(&m)
	bodyH := m.height - 8
	if bodyH < 5 {
		bodyH = 5
	}
	listH := bodyH - 2 // about + add lines
	start := 0
	if st.item >= listH {
		start = st.item - listH + 1
	}
	var right []string
	for i := start; i < len(items) && i < start+listH; i++ {
		right = append(right, renderSetupItem(items[i], st.inItems && i == st.item, rightCW))
	}
	for len(right) < listH {
		right = append(right, "")
	}
	about := consoleInfoStyle.Render(cat.about)
	if cat.add != nil {
		about = footerFocusStyle.Render("a") + footerText.Render(" "+cat.addAs) + consoleInfoStyle.Render("  ·  "+cat.about)
	}
	right = append(right, "", about)

	box := func(content []string, width, contentW int, focused bool) string {
		for i, l := range content {
			content[i] = clipWidth(l, contentW)
		}
		if len(content) > bodyH {
			content = content[:bodyH]
		}
		s := panelStyle.Width(width).Height(bodyH)
		if focused {
			s = s.BorderForeground(colorBorderFocused)
		}
		return s.Render(strings.Join(content, "\n"))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		box(left, leftW, leftCW, !st.inItems),
		box(right, rightW, rightCW, st.inItems))

	title := appTitleStyle.Render(appTitle + "  ·  Setup")
	var bottom string
	switch {
	case st.editing != nil:
		bottom = footerStyle.Render(clipWidth(st.editing.prompt+"   enter save  •  esc cancel", m.width-3)) + "\n  " + st.input.View()
	default:
		keysLine := "↑↓ j k select  •  ←→ / tab switch column  •  space toggle  •  enter edit  •  [ ] reorder  •  d d delete  •  esc / F2 close"
		bottom = footerStyle.Render(clipWidth(keysLine, m.width-3))
	}
	status := ""
	if st.status != "" {
		s := footerText
		if strings.HasPrefix(st.status, "!!") {
			s = consoleErrStyle
		}
		status = "\n" + lipgloss.NewStyle().MarginLeft(2).Render(s.Render(clipWidth(st.status, m.width-3)))
	}
	return title + "\n" + bodyStyle.Render(body) + "\n" + bottom + status + "\n"
}

func renderSetupItem(it setupItem, cursor bool, width int) string {
	box := "    "
	if it.check != nil {
		box = "[ ] "
		if *it.check {
			box = "[x] "
		}
	}
	text := box + it.label
	if it.note != "" {
		text += "  " + consoleInfoStyle.Render(it.note)
	}
	// width is the panel's content width; the cursor column takes 2.
	room := width - 2
	if it.value != "" {
		pad := room - lipgloss.Width(text) - lipgloss.Width(it.value)
		if pad < 2 {
			pad = 2
		}
		text += strings.Repeat(" ", pad) + it.value
	}
	if lipgloss.Width(text) > room {
		text = truncateRunes(stripANSI(text), room-1) + "…"
	}
	if cursor {
		return setupCursorStyle.Render("▸ ") + text
	}
	return "  " + text
}

// clipWidth cuts a (possibly styled) line to w columns so it can never
// wrap.
func clipWidth(s string, w int) string {
	if w < 1 || lipgloss.Width(s) <= w {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

// stripANSI removes styling so a line can be truncated by width.
func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func extruderItems(m *model) []setupItem {
	item := func(label string, unload bool, field func(m *model) *string) setupItem {
		cur := *field(m)
		value := cur
		if cur == "" {
			value = "detect"
			if d := m.filamentCommand(unload); d != "" {
				value = "detect: " + d
			} else {
				value = "detect: none found"
			}
		}
		return setupItem{
			label: label, value: value, initial: cur,
			edit: &setupEdit{prompt: label + ": a command or macro name (empty = detect)", apply: func(m *model, v string) (tea.Cmd, error) {
				v = strings.ToUpper(strings.TrimSpace(v))
				if v != "" && (strings.ContainsAny(v, " \t;#=") || (m.gcodeCommands != nil && !m.gcodeCommands[v])) {
					return nil, fmt.Errorf("%q isn't a command this printer has", v)
				}
				*field(m) = v
				return m.savePrinter(), nil
			}},
		}
	}
	return []setupItem{
		item("Load command", false, func(m *model) *string { return &m.printerCfg.LoadMacro }),
		item("Unload command", true, func(m *model) *string { return &m.printerCfg.UnloadMacro }),
	}
}
