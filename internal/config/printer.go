package config

import (
	"encoding/json"
	"sort"
)

// Database location of per-printer settings.
const (
	DBNamespace = "moontop"
	DBKey       = "printer"
)

// Printer is what Moontop keeps in a printer's Moonraker database.
type Printer struct {
	Temperatures Temperatures `json:"temperatures"`
	Presets      []Preset     `json:"presets,omitempty"`
	// Macros pinned to the dashboard's Macros pane, sorted.
	Macros []string `json:"macros,omitempty"`
	// Filament load / unload commands for the Extruder pane; empty means
	// detect from the printer's macros.
	LoadMacro   string `json:"load_macro,omitempty"`
	UnloadMacro string `json:"unload_macro,omitempty"`
}

// MacroPinned reports whether a macro is on the Macros pane.
func (p *Printer) MacroPinned(name string) bool { return contains(p.Macros, name) }

// SetMacroPinned pins or unpins a macro (the list stays sorted).
func (p *Printer) SetMacroPinned(name string, pinned bool) {
	p.Macros = setMember(p.Macros, name, pinned)
}

// Preset is a Moontop preheat preset: heater object → target °C, plus
// optional gcode run after the targets are set.
type Preset struct {
	Name    string             `json:"name"`
	Targets map[string]float64 `json:"targets"`
	Gcode   string             `json:"gcode,omitempty"`
}

// ParsePrinter decodes the database value. Unknown fields are ignored so
// older Moontops can read newer settings.
func ParsePrinter(raw json.RawMessage) (Printer, error) {
	var p Printer
	if len(raw) == 0 || string(raw) == "null" {
		return p, nil
	}
	err := json.Unmarshal(raw, &p)
	return p, err
}

// Effective merges per-printer settings over the file: when the printer
// has any temperature settings, they replace the file's entirely
// (mixing two hidden lists would make "unhide" impossible to express).
func (p *Printer) EffectiveTemperatures(file Temperatures) Temperatures {
	if len(p.Temperatures.Hidden) > 0 || len(p.Temperatures.Rename) > 0 {
		return p.Temperatures
	}
	return file
}

// HasSettings reports whether anything is stored for this printer.
func (p *Printer) HasSettings() bool {
	return len(p.Temperatures.Hidden) > 0 || len(p.Temperatures.Rename) > 0 || len(p.Presets) > 0 || len(p.Macros) > 0 ||
		p.LoadMacro != "" || p.UnloadMacro != ""
}

// Hidden reports whether object is hidden in t.
func (t *Temperatures) IsHidden(object string) bool { return contains(t.Hidden, object) }

// SetHidden hides or shows object.
func (t *Temperatures) SetHidden(object string, hidden bool) {
	t.Hidden = setMember(t.Hidden, object, hidden)
}

// Label returns object's rename, or "" when it has none.
func (t *Temperatures) Label(object string) string { return t.Rename[object] }

// SetLabel renames object; an empty label removes the rename.
func (t *Temperatures) SetLabel(object, label string) {
	if label == "" {
		delete(t.Rename, object)
		if len(t.Rename) == 0 {
			t.Rename = nil
		}
		return
	}
	if t.Rename == nil {
		t.Rename = map[string]string{}
	}
	t.Rename[object] = label
}

// SortPresets orders presets by name.
func SortPresets(ps []Preset) {
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
}
