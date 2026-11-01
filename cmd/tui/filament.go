package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Filament load / unload (Extruder pane) and gantry levelling (Toolhead)
//
// There's no standard load or unload command: printers ship macros named
// LOAD_FILAMENT, M701, QIDI_LOAD_FILAMENT, M603… So Moontop picks the
// first one the printer has from a list of common names, then any macro
// whose name says it loads or unloads filament. Setup → Extruder
// overrides the choice per printer. A macro with parameters opens its
// form, as from the Macros pane.
//
// Levelling: Z_TILT_ADJUST when the printer has z_tilt, QUAD_GANTRY_LEVEL
// when it has quad_gantry_level, with their "applied" state shown.
// ---------------------------------------------------------------------------

var (
	loadNames   = []string{"LOAD_FILAMENT", "FILAMENT_LOAD", "M701"}
	unloadNames = []string{"UNLOAD_FILAMENT", "FILAMENT_UNLOAD", "M702", "M603"}
)

// filamentCommand returns the load (or unload) command: the per-printer
// override, else the first known name the printer has, else a macro
// named like one. "" when there's none.
func (m *model) filamentCommand(unload bool) string {
	override, names := m.printerCfg.LoadMacro, loadNames
	if unload {
		override, names = m.printerCfg.UnloadMacro, unloadNames
	}
	if override != "" {
		return override
	}
	if m.gcodeCommands == nil {
		return ""
	}
	for _, n := range names {
		if m.gcodeCommands[n] {
			return n
		}
	}
	// QIDI_LOAD_FILAMENT, AUTO_UNLOAD_FILAMENT…: a visible macro whose
	// name contains the phrase (and, for load, isn't an unload).
	for _, mc := range m.macros {
		n := mc.Name
		switch {
		case unload && (strings.Contains(n, "UNLOAD_FILAMENT") || strings.Contains(n, "FILAMENT_UNLOAD")):
			return n
		case !unload && (strings.Contains(n, "LOAD_FILAMENT") || strings.Contains(n, "FILAMENT_LOAD")) && !strings.Contains(n, "UNLOAD"):
			return n
		}
	}
	return ""
}

func (m *model) runFilament(unload bool) tea.Cmd {
	what := map[bool]string{false: "load", true: "unload"}[unload]
	name := m.filamentCommand(unload)
	if name == "" {
		return m.refuse(fmt.Sprintf("no %s command found on this printer; set one in setup (F2 → Extruder)", what))
	}
	if ok, why := canMove(m); !ok {
		return m.refuse(what + " is off " + why)
	}
	for _, mc := range m.macros {
		if mc.Name == strings.ToUpper(name) {
			return m.runMacro(mc) // a form first when it takes parameters
		}
	}
	return m.sendEchoed(what+" filament", name)
}

func haveFilamentCmds(m *model) (bool, string) {
	return m.filamentCommand(false) != "" || m.filamentCommand(true) != "", "needs a load / unload macro (F2 → Extruder)"
}

// --- levelling --------------------------------------------------------------

// leveling describes the printer's gantry levelling, if any.
type leveling struct {
	command string // QUAD_GANTRY_LEVEL / Z_TILT_ADJUST
	label   string // "QGL" / "Z tilt"
	applied bool
}

func (m *model) leveling() (leveling, bool) {
	for _, l := range []struct{ object, command, label string }{
		{"quad_gantry_level", "QUAD_GANTRY_LEVEL", "QGL"},
		{"z_tilt", "Z_TILT_ADJUST", "Z tilt"},
	} {
		if obj, ok := m.raw[l.object]; ok {
			applied, _ := obj["applied"].(bool)
			return leveling{command: l.command, label: l.label, applied: applied}, true
		}
	}
	return leveling{}, false
}

func isLevelingObject(name string) bool { return name == "quad_gantry_level" || name == "z_tilt" }

func hasLeveling(m *model) (bool, string) {
	_, ok := m.leveling()
	return ok, "on printers with z_tilt or quad_gantry_level"
}

func (m *model) runLeveling() tea.Cmd {
	lv, ok := m.leveling()
	if !ok {
		return m.refuse("this printer has no z_tilt or quad_gantry_level")
	}
	if why := m.moveRefusal(-1); why != "" {
		return m.refuse(why)
	}
	return m.sendEchoed(lv.label, lv.command)
}

// levelingLine is the Toolhead pane's extra hint line ("" when none).
func (m *model) levelingLine(width int) string {
	lv, ok := m.leveling()
	if !ok {
		return ""
	}
	state := jogHintStyle.Render("not applied")
	if lv.applied {
		state = consoleNoticeStyle.Render("applied ✓")
	}
	return clipWidth(jogHintStyle.Render("l "+lv.label+"  ")+state, width)
}
