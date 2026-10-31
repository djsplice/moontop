// Package config holds Moontop's settings (UI-2).
//
// Two stores, decided in docs/UI-PLAN.md:
//
//   - File: ~/.config/moontop/config.toml ($XDG_CONFIG_HOME respected)
//     holds display and behaviour preferences — panes, console
//     filters, toolhead steps, safety prompts. It's hand-editable and
//     stores only what differs from the defaults, so new defaults reach
//     users on upgrade.
//   - Printer: Moonraker's database, namespace "moontop", key "printer",
//     holds what belongs to one machine — heater renames, hidden
//     objects, Moontop's own presets. It follows the printer to any
//     computer, the way Mainsail's settings do, and wins over the same
//     settings in the file.
//
// Defaults live in code, behind accessor methods, never in the stored
// values: an unset field means "default".
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/BurntSushi/toml"
)

// Config is the file. Pointer and empty fields mean "use the default".
type Config struct {
	Panes        Panes        `toml:"panes,omitempty"`
	Temperatures Temperatures `toml:"temperatures,omitempty"`
	Console      Console      `toml:"console,omitempty"`
	Toolhead     Toolhead     `toml:"toolhead,omitempty"`
	Safety       Safety       `toml:"safety,omitempty"`
	Columns      Columns      `toml:"columns,omitempty"`
}

// Panes: names as shown in pane titles ("Files", "Toolhead", "Tuning",
// "Temperatures", "Console").
//
// Layout (UI-3): the four top panes sit in a row in Order, the Console
// across the bottom. Width maps a pane to its share of the row's spare
// width; 0 (or absent) means "natural width" — just what its content
// needs. ConsoleHeight is the Console's share of the screen in percent.
type Panes struct {
	Hidden        []string       `toml:"hidden,omitempty"`
	Order         []string       `toml:"order,omitempty"`
	Width         map[string]int `toml:"width,omitempty"`
	ConsoleHeight *int           `toml:"console_height,omitempty"` // percent
}

// TopPanes are the panes that can be ordered, in their default order.
var TopPanes = []string{"Files", "Toolhead", "Tuning", "Temperatures"}

// DefaultWidths: Files and Temperatures share spare width 1:2;
// Toolhead and Tuning keep their natural width.
var DefaultWidths = map[string]int{"Files": 1, "Toolhead": 0, "Tuning": 0, "Temperatures": 2}

// Ordered returns TopPanes in the configured order. Unknown names are
// dropped and missing ones appended, so a stale or hand-edited list
// can't lose a pane.
func (p *Panes) Ordered() []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range p.Order {
		if contains(TopPanes, n) && !seen[n] {
			out = append(out, n)
			seen[n] = true
		}
	}
	for _, n := range TopPanes {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}

// Move shifts pane name by delta places in the order.
func (p *Panes) Move(name string, delta int) {
	o := p.Ordered()
	i := -1
	for k, n := range o {
		if n == name {
			i = k
		}
	}
	j := i + delta
	if i < 0 || j < 0 || j >= len(o) {
		return
	}
	o[i], o[j] = o[j], o[i]
	p.Order = o
	if equalStrings(o, TopPanes) {
		p.Order = nil
	}
}

// WidthOf returns pane name's width weight.
func (p *Panes) WidthOf(name string) int {
	if w, ok := p.Width[name]; ok && w >= 0 {
		return w
	}
	return DefaultWidths[name]
}

// SetWidth sets a weight; the default value removes the override.
func (p *Panes) SetWidth(name string, w int) {
	if w == DefaultWidths[name] {
		delete(p.Width, name)
		if len(p.Width) == 0 {
			p.Width = nil
		}
		return
	}
	if p.Width == nil {
		p.Width = map[string]int{}
	}
	p.Width[name] = w
}

// ConsolePercent is the Console's share of the screen, 0 = automatic.
func (p *Panes) ConsolePercent() int {
	if p.ConsoleHeight == nil {
		return 0
	}
	return *p.ConsoleHeight
}

// Columns chooses table columns (UI-3). Empty means the default.
type Columns struct {
	Temperatures []string `toml:"temperatures,omitempty"`
	Files        []string `toml:"files,omitempty"`
	FilesSort    string   `toml:"files_sort,omitempty"` // "modified" (default) or "name"
}

// Column choices, in display order.
var (
	TempColumns        = []string{"state", "current", "target", "power"}
	DefaultTempColumns = []string{"state", "current", "target"}
	FileColumns        = []string{"size", "modified"}
	DefaultFileColumns = []string{"size"}
)

func pick(choices, chosen, def []string) []string {
	if len(chosen) == 0 {
		return def
	}
	var out []string
	for _, c := range choices {
		if contains(chosen, c) {
			out = append(out, c)
		}
	}
	return out
}

func (c *Columns) Temps() []string { return pick(TempColumns, c.Temperatures, DefaultTempColumns) }
func (c *Columns) FileCols() []string {
	return pick(FileColumns, c.Files, DefaultFileColumns)
}
func (c *Columns) SortByName() bool { return c.FilesSort == "name" }

// ToggleTemp turns a Temperatures column on or off. Turning off the
// last one is refused (Name alone isn't useful).
func (c *Columns) ToggleTemp(col string) bool {
	cur := c.Temps()
	next := setMember(append([]string(nil), cur...), col, !contains(cur, col))
	if len(next) == 0 {
		return false
	}
	c.Temperatures = pick(TempColumns, next, nil) // stored in display order
	if equalStrings(c.Temperatures, DefaultTempColumns) {
		c.Temperatures = nil
	}
	return true
}

// ToggleFile turns a Files column on or off (none is allowed: name only).
func (c *Columns) ToggleFile(col string) {
	cur := c.FileCols()
	next := setMember(append([]string(nil), cur...), col, !contains(cur, col))
	c.Files = pick(FileColumns, next, nil)
	if len(next) == 0 {
		c.Files = []string{"none"} // remembered as "no columns", not "default"
	}
	if equalStrings(pick(FileColumns, next, nil), DefaultFileColumns) {
		c.Files = nil
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Temperatures in the file are a fallback for printers with no
// Moontop settings in their database yet.
type Temperatures struct {
	Hidden []string          `toml:"hidden,omitempty"` // Klipper object names
	Rename map[string]string `toml:"rename,omitempty"` // object → label
}

type Console struct {
	HideWaitTemps  *bool    `toml:"hide_wait_temperatures,omitempty"`
	HideRunCurrent *bool    `toml:"hide_run_current,omitempty"`
	Filters        []Filter `toml:"filters,omitempty"`
}

// Filter is a user console filter: lines matching Pattern (a Go
// regular expression) are hidden unless Disabled.
type Filter struct {
	Name     string `toml:"name"`
	Pattern  string `toml:"pattern"`
	Disabled bool   `toml:"disabled,omitempty"`
}

type Toolhead struct {
	StepSizes  []float64 `toml:"step_sizes,omitempty"`  // mm
	XYFeedrate *int      `toml:"xy_feedrate,omitempty"` // mm/min
	ZFeedrate  *int      `toml:"z_feedrate,omitempty"`  // mm/min
}

type Safety struct {
	ConfirmEStop       *bool `toml:"confirm_estop,omitempty"`
	ConfirmRestartIdle *bool `toml:"confirm_restart_when_idle,omitempty"`
}

// Defaults.
var (
	DefaultStepSizes  = []float64{0.1, 1, 10, 25, 50, 100}
	DefaultXYFeedrate = 1500
	DefaultZFeedrate  = 600
)

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// Bool returns a pointer for setting an optional flag.
func Bool(v bool) *bool { return &v }

func (c *Console) WaitTempsHidden() bool  { return boolOr(c.HideWaitTemps, true) }
func (c *Console) RunCurrentHidden() bool { return boolOr(c.HideRunCurrent, true) }

func (s *Safety) EStopConfirms() bool       { return boolOr(s.ConfirmEStop, true) }
func (s *Safety) RestartConfirmsIdle() bool { return boolOr(s.ConfirmRestartIdle, true) }
func (t *Toolhead) Steps() []float64 {
	if len(t.StepSizes) == 0 {
		return DefaultStepSizes
	}
	s := append([]float64(nil), t.StepSizes...)
	sort.Float64s(s)
	return s
}
func (t *Toolhead) XY() int { return intOr(t.XYFeedrate, DefaultXYFeedrate) }
func (t *Toolhead) Z() int  { return intOr(t.ZFeedrate, DefaultZFeedrate) }

func intOr(p *int, def int) int {
	if p == nil || *p <= 0 {
		return def
	}
	return *p
}

// Int returns a pointer for setting an optional number.
func Int(v int) *int { return &v }

// PaneHidden reports whether the named pane is hidden.
func (p *Panes) PaneHidden(name string) bool { return contains(p.Hidden, name) }

// SetPaneHidden adds or removes name from the hidden list.
func (p *Panes) SetPaneHidden(name string, hidden bool) { p.Hidden = setMember(p.Hidden, name, hidden) }

// ValidateFilter compiles a filter pattern, for the setup screen to
// reject bad input before saving it.
func ValidateFilter(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty pattern")
	}
	_, err := regexp.Compile(pattern)
	return err
}

// DefaultPath is $XDG_CONFIG_HOME/moontop/config.toml, else
// ~/.config/moontop/config.toml (also on macOS, like most CLI tools).
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "moontop", "config.toml"), nil
}

// Load reads path; a missing file is an empty (all-default) config.
func Load(path string) (*Config, error) {
	c := &Config{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if _, err := toml.Decode(string(data), c); err != nil {
		return &Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

const fileHeader = `# Moontop settings. Written by the setup screen (F2); safe to edit by hand.
# Only settings that differ from the defaults are stored here.
# Per-printer settings (heater names, hidden heaters, presets) live in
# the printer's Moonraker database instead.

`

// Save writes c to path atomically, creating its directory.
func Save(path string, c *Config) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeader)
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func setMember(list []string, s string, member bool) []string {
	out := list[:0:0]
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	if member {
		out = append(out, s)
		sort.Strings(out)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
