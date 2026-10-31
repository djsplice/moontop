package main

import (
	"fmt"
	"regexp"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/config"
	"github.com/justinh-rahb/moonraker-tui/internal/console"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
)

// ---------------------------------------------------------------------------
// UI-2: settings as model state
//
// m.cfg (the file) and m.printerCfg (the printer's database entry) are
// the only stored settings; everything derived from them — filters,
// hidden panes, the visible heater list, step sizes — is recomputed by
// applySettings whenever either changes. The setup screen edits them
// through the same path, so a change shows up immediately.
// ---------------------------------------------------------------------------

// printerCfgMsg carries the printer's Moontop settings from Moonraker.
type printerCfgMsg struct {
	cfg config.Printer
	err error
}

// settingsSavedMsg reports where a save went (or why it failed).
type settingsSavedMsg struct {
	where string
	err   error
}

func loadPrinterCfgCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		raw, err := client.DatabaseGetItem(config.DBNamespace, config.DBKey)
		if err != nil {
			// No "moontop" namespace yet is the normal first-run case.
			return printerCfgMsg{}
		}
		p, err := config.ParsePrinter(raw)
		return printerCfgMsg{cfg: p, err: err}
	}
}

func savePrinterCfgCmd(client *moonraker.Client, p config.Printer) tea.Cmd {
	return func() tea.Msg {
		err := client.DatabasePostItem(config.DBNamespace, config.DBKey, p)
		return settingsSavedMsg{where: "printer (Moonraker database)", err: err}
	}
}

func saveFileCfgCmd(path string, c config.Config) tea.Cmd {
	return func() tea.Msg {
		if path == "" {
			return settingsSavedMsg{err: fmt.Errorf("not saved: the config file had errors at startup")}
		}
		return settingsSavedMsg{where: path, err: config.Save(path, &c)}
	}
}

// temps is the effective heater settings: the printer's, else the file's.
func (m *model) temps() config.Temperatures {
	return m.printerCfg.EffectiveTemperatures(m.cfg.Temperatures)
}

// label is an object's display name: its rename, else the friendly name.
func (m *model) label(object string) string {
	t := m.temps()
	if l := t.Label(object); l != "" {
		return l
	}
	return friendlyName(object)
}

// hiddenPanes maps the file's pane names to focus areas.
func (m *model) hiddenPanes() map[focusArea]bool {
	h := map[focusArea]bool{}
	for _, f := range paneOrder {
		if m.cfg.Panes.PaneHidden(paneName(f)) {
			h[f] = true
		}
	}
	return h
}

// layoutInput gathers the solver's inputs from settings and state.
func (m *model) layoutInput(reservedRows int) layoutInput {
	in := defaultLayoutInput(m.width, m.height, len(m.shownSensors))
	in.reservedRows = reservedRows
	in.hidden = m.hiddenPanes()
	in.order = nil
	for _, name := range m.cfg.Panes.Ordered() {
		if f, ok := paneByName(name); ok {
			in.order = append(in.order, f)
			in.weights[f] = m.cfg.Panes.WidthOf(name)
		}
	}
	in.consolePct = m.cfg.Panes.ConsolePercent()
	return in
}

func paneByName(name string) (focusArea, bool) {
	for _, f := range paneOrder {
		if paneName(f) == name {
			return f, true
		}
	}
	return 0, false
}

// buildFilters turns the console settings into a FilterSet.
func buildFilters(c config.Console) console.FilterSet {
	var fs console.FilterSet
	if c.WaitTempsHidden() {
		fs = append(fs, console.WaitTempFilter)
	}
	if c.RunCurrentHidden() {
		fs = append(fs, console.RunCurrentFilter)
	}
	for _, f := range c.Filters {
		if f.Disabled {
			continue
		}
		re, err := regexp.Compile(f.Pattern)
		if err != nil {
			continue // rejected at entry; a hand-edited bad one is skipped
		}
		fs = append(fs, console.Filter{Name: f.Name, Pattern: re})
	}
	return fs
}

// applySettings recomputes everything derived from the settings.
func (m *model) applySettings() {
	m.filters = buildFilters(m.cfg.Console)

	// Step sizes: keep the selected size if it still exists, else the
	// nearest one to it.
	cur := 10.0
	if m.stepIndex >= 0 && m.stepIndex < len(m.steps) {
		cur = m.steps[m.stepIndex]
	}
	m.steps = m.cfg.Toolhead.Steps()
	m.stepIndex = nearestIndex(m.steps, cur)

	// Visible heaters, in the table's sort order.
	t := m.temps()
	m.shownSensors = m.shownSensors[:0]
	for _, n := range m.sensorNames {
		if !t.IsHidden(n) {
			m.shownSensors = append(m.shownSensors, n)
		}
	}

	// A hidden pane can't keep focus.
	if m.hiddenPanes()[m.focus] {
		m.cycleFocus(1)
	}

	m.presets = mergePresets(m.printerCfg.Presets, m.mainsailPresets)

	if m.ready {
		m.resizeLayout() // also re-picks the table columns
		m.refreshConsole()
	}
	if len(m.fileInfos) > 0 {
		m.rebuildFiles()
	}
}

func nearestIndex(steps []float64, v float64) int {
	best := 0
	for i, s := range steps {
		if abs(s-v) < abs(steps[best]-v) {
			best = i
		}
	}
	return best
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// mergePresets lists Moontop's presets first, then Mainsail's.
func mergePresets(own []config.Preset, mainsail []preset) []preset {
	var out []preset
	for _, p := range own {
		pr := preset{Name: p.Name, Gcode: p.Gcode, Source: "Moontop"}
		for obj, t := range p.Targets {
			pr.Values = append(pr.Values, presetValue{Object: obj, Target: t})
		}
		sort.Slice(pr.Values, func(i, j int) bool { return pr.Values[i].Object < pr.Values[j].Object })
		out = append(out, pr)
	}
	for _, p := range mainsail {
		p.Source = "Mainsail"
		out = append(out, p)
	}
	return out
}

// currentTargetsPreset captures every heater's current target above 0.
func (m *model) currentTargetsPreset(name string) (config.Preset, error) {
	p := config.Preset{Name: name, Targets: map[string]float64{}}
	for _, n := range m.sensorNames {
		if s := m.sensors[n]; s != nil && s.Target > 0 && canSetTarget(n) {
			p.Targets[n] = s.Target
		}
	}
	if len(p.Targets) == 0 {
		return p, fmt.Errorf("no heater has a target set; set the temperatures you want first")
	}
	return p, nil
}
