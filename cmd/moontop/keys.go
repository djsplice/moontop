package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Keymap registry (UI-1)
//
// Every dashboard key is declared once, here, as a key.Binding plus the
// metadata the UI needs: which scope it belongs to, when it applies,
// and whether it earns a footer slot. Key handling matches against
// these bindings (key.Matches), and the footer and the help overlay are
// rendered from registry — so a key can't exist without a hint, and a
// hint can't advertise a key that does nothing.
//
// Some bindings are handled by a Bubbles widget (the file list's
// filter, the table's cursor). They're still declared here so help can
// list them; `widget: true` marks them.
// ---------------------------------------------------------------------------

type keyScope int

const (
	scopeGlobal keyScope = iota
	scopeJob
	scopeKlipper
	scopeFiles
	scopeToolhead
	scopeToolheadEdit
	scopeTuning
	scopeExtruder
	scopeOutputs
	scopeMacros
	scopeTemps
	scopeTempEdit
	scopeConsole
	scopeAFC
	scopeHelp
)

func (s keyScope) title() string {
	switch s {
	case scopeGlobal:
		return "Global"
	case scopeJob:
		return "Print job"
	case scopeKlipper:
		return "Klipper"
	case scopeFiles:
		return "Files"
	case scopeToolhead:
		return "Toolhead"
	case scopeToolheadEdit:
		return "Typing a value (Toolhead, Tuning, Extruder, Outputs)"
	case scopeExtruder:
		return "Extruder"
	case scopeOutputs:
		return "Outputs (fans, pins, LEDs)"
	case scopeMacros:
		return "Macros (pinned; F3 or g lists all)"
	case scopeTuning:
		return "Tuning"
	case scopeTemps:
		return "Temperatures"
	case scopeTempEdit:
		return "Temperatures, editing a target"
	case scopeConsole:
		return "Console"
	case scopeAFC:
		return "AFC lanes"
	case scopeHelp:
		return "Prompts and help"
	}
	return "?"
}

// keyEntry is one registry row.
type keyEntry struct {
	b     *key.Binding
	scope keyScope
	// when reports whether the action applies right now; why says
	// what it's waiting for ("while printing"). nil = always.
	when   func(m *model) (ok bool, why string)
	footer bool   // shown in the footer when its scope is in play
	short  string // footer label; the help text is used when empty
	widget bool   // handled by a Bubbles widget, listed for help only
}

func (e keyEntry) active(m *model) (bool, string) {
	if e.when == nil {
		return true, ""
	}
	return e.when(m)
}

func bind(keys []string, help, desc string) *key.Binding {
	b := key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	return &b
}

// keys holds every dashboard binding by name, for handlers.
var keys = struct {
	EStop, Quit, Help, Setup, NextPane, PrevPane, JumpPane, ToggleFilter, Jobs, Machine, Updates, BedMesh, Logs, Config, Printers *key.Binding
	Pause, Resume, Cancel, Exclude                                                                                                *key.Binding
	Restart, FwRestart                                                                                                            *key.Binding
	Confirm, Deny, HelpScroll                                                                                                     *key.Binding
	FilesMove, FilesFilter, FilesStart, FilesQueue, FilesManage                                                                   *key.Binding
	JogX, JogY, JogZ, StepDown, StepUp                                                                                            *key.Binding
	ThMove, ThType, ThEdit, ThEditApply, ThEditCancel                                                                             *key.Binding
	HomeAll, HomeX, HomeY, HomeZ, MotorsOff                                                                                       *key.Binding
	FieldMove, FieldAdjust, FieldType, FieldEdit, Extrude, Retract                                                                *key.Binding
	OutputOff, LEDColor                                                                                                           *key.Binding
	Macros, MacroMove, MacroRun, MacroUnpin                                                                                       *key.Binding
	FilLoad, FilUnload, Level, Endstops                                                                                           *key.Binding
	TempsMove, TempSet, TempOff, TempAllOff, TempPreset                                                                           *key.Binding
	TempEditType, TempEditApply, TempEditCancel                                                                                   *key.Binding
	ConsoleSend, ConsoleHistory                                                                                                   *key.Binding
	AfcMove, AfcLoad, AfcUnload, AfcEject, AfcClear                                                                               *key.Binding
	AfcMap, AfcMaterial, AfcWeight, AfcColor, AfcSpool                                                                            *key.Binding
}{
	EStop:        bind([]string{"ctrl+x"}, "ctrl+x", "emergency stop (confirms)"),
	Quit:         bind([]string{"ctrl+c", "f10", "q"}, "q/F10", "quit"),
	Help:         bind([]string{"?", "f1"}, "?/F1", "help (this screen)"),
	Setup:        bind([]string{"f2", ","}, "F2/,", "setup: panes, heaters, filters, presets, safety"),
	NextPane:     bind([]string{"tab"}, "tab", "next pane"),
	PrevPane:     bind([]string{"shift+tab"}, "shift+tab", "previous pane"),
	JumpPane:     bind([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}, "1–9", "jump to pane by number"),
	ToggleFilter: bind([]string{"ctrl+o"}, "ctrl+o", "show/hide filtered console lines"),
	Macros:       bind([]string{"f3", "g"}, "F3/g", "macros: all of them, filter, run, pin"),
	Jobs:         bind([]string{"f7", "J"}, "F7/J", "jobs: files, print history, totals, job queue"),
	Machine:      bind([]string{"f8", "M"}, "F8/M", "machine: host, MCUs, services, power devices"),
	Updates:      bind([]string{"f9", "U"}, "F9/U", "software updates and announcements"),
	BedMesh:      bind([]string{"f11", "B"}, "F11/B", "bed mesh: grid, profiles, calibrate, screws tilt"),
	Logs:         bind([]string{"f12", "L"}, "F12/L", "logs: klippy.log, moonraker.log… (follow, filter, roll over)"),
	Config:       bind([]string{"E"}, "E", "config files: read, edit in $EDITOR"),
	Printers:     bind([]string{"P"}, "P", "printers: back to the connection screen to switch"),

	Pause:   bind([]string{"p"}, "p", "pause print"),
	Exclude: bind([]string{"f4", "O"}, "O/F4", "objects: exclude one from the print (confirms)"),
	Resume:  bind([]string{"r"}, "r", "resume print"),
	Cancel:  bind([]string{"c"}, "c", "cancel print (confirms)"),

	// F5 / F6 work from every pane, including while the console takes
	// text (where K and F are just letters).
	Restart:   bind([]string{"f5", "K"}, "F5/K", "restart Klipper (confirms)"),
	FwRestart: bind([]string{"f6", "F"}, "F6/F", "firmware restart (confirms)"),

	Confirm:    bind([]string{"y", "Y"}, "y", "confirm a prompt"),
	Deny:       bind([]string{"n", "N", "esc"}, "n/esc", "cancel a prompt"),
	HelpScroll: bind([]string{"up", "down", "k", "j", "pgup", "pgdown"}, "↑↓ j k", "scroll help; esc or ? closes"),

	FilesMove:   bind([]string{"up", "down", "k", "j"}, "↑↓ j k", "select file"),
	FilesFilter: bind([]string{"/"}, "/", "filter files (esc clears)"),
	FilesStart:  bind([]string{"enter"}, "enter", "print selected file (confirms)"),
	FilesQueue:  bind([]string{"a"}, "a", "add the selected file to the job queue"),
	FilesManage: bind([]string{"f"}, "f", "file manager: folders, details, rename, move, delete, upload"),

	// Toolhead: WASD jogs XY, space jumps (Z up), c crouches (Z down);
	// the arrows move the field cursor and change the step.
	JogX:         bind([]string{"a", "d"}, "a d", "jog X − / + by the step"),
	JogY:         bind([]string{"w", "s"}, "w s", "jog Y + / − by the step"),
	JogZ:         bind([]string{" ", "c", "pgup", "pgdown"}, "space c", "jog Z up (jump) / down (crouch); PgUp PgDn too"),
	StepDown:     bind([]string{"left", "["}, "← [", "smaller step"),
	StepUp:       bind([]string{"right", "]"}, "→ ]", "larger step"),
	ThMove:       bind([]string{"up", "down", "k", "j"}, "↑↓ j k", "select X, Y, Z or step"),
	ThType:       bind([]string{}, "0–9 . -", "type a position to move to (or a custom step)"),
	ThEdit:       bind([]string{"enter"}, "enter", "edit the selected field"),
	ThEditApply:  bind([]string{}, "enter", "set it (Toolhead: move there, absolute)"),
	ThEditCancel: bind([]string{}, "esc", "cancel"),
	HomeAll:      bind([]string{"H"}, "H", "home all axes"),
	HomeX:        bind([]string{"x"}, "x", "home X"),
	HomeY:        bind([]string{"y"}, "y", "home Y"),
	HomeZ:        bind([]string{"z"}, "z", "home Z"),
	MotorsOff:    bind([]string{"m"}, "m", "motors off (M84)"),

	// Tuning and Extruder rows (fields.go).
	FieldMove:   bind([]string{"up", "down", "k", "j"}, "↑↓ j k", "select a row"),
	FieldAdjust: bind([]string{"left", "right", "shift+left", "shift+right"}, "← →", "adjust by its step, at once (shift: ×10)"),
	FieldType:   bind([]string{}, "0–9 .", "type a value; enter sets it, esc cancels"),
	FieldEdit:   bind([]string{"enter"}, "enter", "edit the selected row"),
	Extrude:     bind([]string{"+", "="}, "+", "extrude the length at the speed"),
	Retract:     bind([]string{"-"}, "-", "retract the length at the speed"),
	FilLoad:     bind([]string{"l"}, "l", "load filament (the printer's load macro; setup → Extruder)"),
	FilUnload:   bind([]string{"u"}, "u", "unload filament (the printer's unload macro)"),
	Level:       bind([]string{"l"}, "l", "level the gantry: QUAD_GANTRY_LEVEL or Z_TILT_ADJUST"),
	Endstops:    bind([]string{"e"}, "e", "query endstops (shown in the console)"),
	MacroMove:   bind([]string{"up", "down", "k", "j"}, "↑↓ j k", "select a pinned macro"),
	MacroRun:    bind([]string{"enter"}, "enter", "run it (a form first when it takes parameters)"),
	MacroUnpin:  bind([]string{"x"}, "x", "unpin it from this pane"),
	OutputOff:   bind([]string{"x"}, "x", "turn the selected fan, pin or LED off"),
	LEDColor:    bind([]string{"o"}, "o", "set the selected LED's colour (hex)"),

	TempsMove:  bind([]string{"up", "down", "k", "j"}, "↑↓ j k", "select heater or sensor"),
	TempSet:    bind([]string{"enter"}, "enter", "set target of the selected heater"),
	TempOff:    bind([]string{"x"}, "x", "turn the selected heater off"),
	TempAllOff: bind([]string{"X"}, "X", "cooldown: all heaters off"),
	TempPreset: bind([]string{"t"}, "t", "preheat from a preset"),

	TempEditType:   bind([]string{}, "0–9 .", "type the target in °C (0 = off)"),
	TempEditApply:  bind([]string{}, "enter", "send the new target"),
	TempEditCancel: bind([]string{}, "esc", "cancel"),

	AfcMove:     bind([]string{"up", "down", "k", "j"}, "↑↓ j k", "select lane"),
	AfcLoad:     bind([]string{"enter"}, "enter", "load the lane into its tool (CHANGE_TOOL, confirms)"),
	AfcUnload:   bind([]string{"u"}, "u", "unload the lane from its tool (TOOL_UNLOAD, confirms)"),
	AfcEject:    bind([]string{"e"}, "e", "eject the lane's filament (LANE_UNLOAD, confirms)"),
	AfcClear:    bind([]string{"x"}, "x", "clear the AFC message"),
	AfcMap:      bind([]string{"t"}, "t", "set the lane's tool (SET_MAP)"),
	AfcMaterial: bind([]string{"m"}, "m", "set material"),
	AfcWeight:   bind([]string{"w"}, "w", "set remaining weight"),
	AfcColor:    bind([]string{"o"}, "o", "set colour"),
	AfcSpool:    bind([]string{"s"}, "s", "set Spoolman spool ID"),

	ConsoleSend:    bind([]string{"enter"}, "enter", "send gcode"),
	ConsoleHistory: bind([]string{"up", "down"}, "↑ ↓", "command history"),
}

func whilePrinting(m *model) (bool, string) {
	return m.job.State == "printing", "while printing"
}

func whilePaused(m *model) (bool, string) {
	return m.job.State == "paused", "while paused"
}

func whileJobActive(m *model) (bool, string) {
	return jobActive(m.job.State), "while a print is active"
}

func afcIdle(m *model) (bool, string) {
	why := m.afcBlocked(m.afcModel(), false)
	return why == "", why
}

func afcHasMessage(m *model) (bool, string) {
	am := m.afcModel()
	return am != nil && am.Message != "", "when AFC shows a message"
}

func havePresets(m *model) (bool, string) {
	return len(m.presets) > 0, "needs a preset (F2 → Presets, or Mainsail)"
}

func ledSelected(m *model) (bool, string) {
	i := m.outFields.cursor
	return i < len(m.outputs) && m.outputs[i].kind == outLED, "on an LED row"
}

func whileIdle(m *model) (bool, string) {
	return !jobActive(m.job.State), "when idle"
}

// registry is the ordered list help and footer render from.
var registry = []keyEntry{
	{b: keys.Help, scope: scopeGlobal, footer: true},
	{b: keys.Setup, scope: scopeGlobal},
	{b: keys.Macros, scope: scopeGlobal},
	{b: keys.Jobs, scope: scopeGlobal},
	{b: keys.Machine, scope: scopeGlobal},
	{b: keys.Printers, scope: scopeGlobal},
	{b: keys.Updates, scope: scopeGlobal},
	{b: keys.BedMesh, scope: scopeGlobal, when: hasBedMesh},
	{b: keys.Logs, scope: scopeGlobal},
	{b: keys.Config, scope: scopeGlobal},
	{b: keys.EStop, scope: scopeGlobal, footer: true},
	{b: keys.NextPane, scope: scopeGlobal, footer: true},
	{b: keys.PrevPane, scope: scopeGlobal},
	{b: keys.JumpPane, scope: scopeGlobal},
	{b: keys.ToggleFilter, scope: scopeGlobal},
	{b: keys.Quit, scope: scopeGlobal},

	{b: keys.Pause, scope: scopeJob, short: "pause", when: whilePrinting, footer: true},
	{b: keys.Resume, scope: scopeJob, short: "resume", when: whilePaused, footer: true},
	{b: keys.Cancel, scope: scopeJob, short: "cancel", when: whileJobActive, footer: true},
	{b: keys.Exclude, scope: scopeJob, short: "objects", when: canExclude, footer: true},

	{b: keys.Restart, scope: scopeKlipper},
	{b: keys.FwRestart, scope: scopeKlipper},

	{b: keys.FilesMove, scope: scopeFiles, short: "select", footer: true, widget: true},
	{b: keys.FilesFilter, scope: scopeFiles, short: "filter", footer: true, widget: true},
	{b: keys.FilesStart, scope: scopeFiles, short: "print", footer: true},
	{b: keys.FilesQueue, scope: scopeFiles, short: "queue", footer: true},
	{b: keys.FilesManage, scope: scopeFiles, short: "manage", footer: true},

	{b: keys.JogX, scope: scopeToolhead, short: "X", footer: true, when: canMove},
	{b: keys.JogY, scope: scopeToolhead, short: "Y", footer: true, when: canMove},
	{b: keys.JogZ, scope: scopeToolhead, short: "Z", footer: true, when: canMove},
	{b: keys.ThMove, scope: scopeToolhead, short: "field", footer: true},
	{b: keys.ThType, scope: scopeToolhead, short: "type", footer: true},
	{b: keys.ThEdit, scope: scopeToolhead},
	{b: keys.StepDown, scope: scopeToolhead, short: "step", footer: true},
	{b: keys.StepUp, scope: scopeToolhead},
	{b: keys.HomeAll, scope: scopeToolhead, short: "home", footer: true, when: canMove},
	{b: keys.HomeX, scope: scopeToolhead, when: canMove},
	{b: keys.HomeY, scope: scopeToolhead, when: canMove},
	{b: keys.HomeZ, scope: scopeToolhead, when: canMove},

	{b: keys.MotorsOff, scope: scopeToolhead, when: whileIdle},
	{b: keys.Level, scope: scopeToolhead, short: "level", footer: true, when: hasLeveling},
	{b: keys.Endstops, scope: scopeToolhead},

	{b: keys.ThType, scope: scopeToolheadEdit, short: "type", footer: true},
	{b: keys.ThEditApply, scope: scopeToolheadEdit, short: "set", footer: true},
	{b: keys.ThEditCancel, scope: scopeToolheadEdit, short: "cancel", footer: true},

	{b: keys.FieldMove, scope: scopeTuning, short: "row", footer: true},
	{b: keys.FieldAdjust, scope: scopeTuning, short: "adjust", footer: true},
	{b: keys.FieldType, scope: scopeTuning, short: "type", footer: true},
	{b: keys.FieldEdit, scope: scopeTuning},

	{b: keys.Extrude, scope: scopeExtruder, short: "extrude", footer: true, when: canExtrude},
	{b: keys.Retract, scope: scopeExtruder, short: "retract", footer: true, when: canExtrude},
	{b: keys.FilLoad, scope: scopeExtruder, short: "load", footer: true, when: haveFilamentCmds},
	{b: keys.FilUnload, scope: scopeExtruder, short: "unload", footer: true, when: haveFilamentCmds},
	{b: keys.FieldMove, scope: scopeExtruder, short: "row", footer: true},
	{b: keys.FieldAdjust, scope: scopeExtruder, short: "adjust", footer: true},
	{b: keys.FieldType, scope: scopeExtruder, short: "type"},
	{b: keys.FieldEdit, scope: scopeExtruder},

	{b: keys.MacroMove, scope: scopeMacros, short: "select", footer: true},
	{b: keys.MacroRun, scope: scopeMacros, short: "run", footer: true},
	{b: keys.MacroUnpin, scope: scopeMacros, short: "unpin", footer: true},
	{b: keys.Macros, scope: scopeMacros, short: "all macros", footer: true},

	{b: keys.FieldMove, scope: scopeOutputs, short: "row", footer: true},
	{b: keys.FieldAdjust, scope: scopeOutputs, short: "adjust", footer: true},
	{b: keys.OutputOff, scope: scopeOutputs, short: "off", footer: true},
	{b: keys.LEDColor, scope: scopeOutputs, short: "colour", footer: true, when: ledSelected},
	{b: keys.FieldType, scope: scopeOutputs, short: "type"},
	{b: keys.FieldEdit, scope: scopeOutputs},

	{b: keys.TempsMove, scope: scopeTemps, short: "select", footer: true, widget: true},
	{b: keys.TempSet, scope: scopeTemps, short: "set", footer: true},
	{b: keys.TempPreset, scope: scopeTemps, short: "preheat", footer: true, when: havePresets},
	{b: keys.TempOff, scope: scopeTemps, short: "off", footer: true},
	{b: keys.TempAllOff, scope: scopeTemps, short: "all off", footer: true},

	{b: keys.TempEditType, scope: scopeTempEdit, short: "type °C", footer: true},
	{b: keys.TempEditApply, scope: scopeTempEdit, short: "set", footer: true},
	{b: keys.TempEditCancel, scope: scopeTempEdit, short: "cancel", footer: true},

	{b: keys.AfcMove, scope: scopeAFC, short: "lane", footer: true},
	{b: keys.AfcLoad, scope: scopeAFC, short: "load", footer: true, when: afcIdle},
	{b: keys.AfcUnload, scope: scopeAFC, short: "unload", footer: true, when: afcIdle},
	{b: keys.AfcEject, scope: scopeAFC, short: "eject", footer: true, when: afcIdle},
	{b: keys.AfcMap, scope: scopeAFC, short: "tool", when: afcIdle},
	{b: keys.AfcMaterial, scope: scopeAFC, short: "material", when: afcIdle},
	{b: keys.AfcWeight, scope: scopeAFC, short: "weight", when: afcIdle},
	{b: keys.AfcColor, scope: scopeAFC, short: "colour", when: afcIdle},
	{b: keys.AfcSpool, scope: scopeAFC, short: "spool", when: afcIdle},
	{b: keys.AfcClear, scope: scopeAFC, short: "clear msg", footer: true, when: afcHasMessage},

	{b: keys.ConsoleSend, scope: scopeConsole, short: "send", footer: true},
	{b: keys.ConsoleHistory, scope: scopeConsole, short: "history", footer: true},

	{b: keys.Confirm, scope: scopeHelp},
	{b: keys.Deny, scope: scopeHelp},
	{b: keys.HelpScroll, scope: scopeHelp},
}

// paneScope maps a focused pane to its key scope.
func paneScope(f focusArea) keyScope {
	switch f {
	case focusFiles:
		return scopeFiles
	case focusJog:
		return scopeToolhead
	case focusTuning:
		return scopeTuning
	case focusTable:
		return scopeTemps
	case focusConsole:
		return scopeConsole
	case focusAFC:
		return scopeAFC
	case focusExtruder:
		return scopeExtruder
	case focusOutputs:
		return scopeOutputs
	case focusMacros:
		return scopeMacros
	}
	return scopeGlobal
}

// footerHints returns the focused pane's footer keys and the job keys
// that apply right now, each as "key label" items. The caller joins
// them and drops pane items if the line is too wide.
func footerHints(m *model) (pane, job []string) {
	ps := paneScope(m.focus)
	if m.tempEdit != "" {
		ps = scopeTempEdit
	}
	if m.thEditing || m.editingField() {
		ps = scopeToolheadEdit
	}
	var p, j []string
	for _, e := range registry {
		if !e.footer {
			continue
		}
		if ok, _ := e.active(m); !ok {
			continue
		}
		h := e.b.Help()
		label := e.short
		if label == "" {
			label = shortDesc(h.Desc)
		}
		switch e.scope {
		case ps:
			p = append(p, h.Key+" "+label)
		case scopeJob:
			// Hidden in the console (keys type text there) and when the
			// focused pane uses the same key (c is Z down in Toolhead).
			if m.focus != focusConsole && !m.paneShadows(e.b) {
				j = append(j, h.Key+" "+label)
			}
		}
	}
	return p, j
}

// shortDesc drops a trailing "(confirms)"-style note for tight spaces.
func shortDesc(d string) string {
	if i := strings.Index(d, " ("); i > 0 {
		return d[:i]
	}
	return d
}

// ---------------------------------------------------------------------------
// Text capture
//
// One rule replaces the old per-key "not in console" guards: while the
// focused widget is taking text, printable keys are text, not commands.
// Ctrl- and function keys, tab and arrows always remain commands.
// ---------------------------------------------------------------------------

func isTextKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace
}

// capturesText reports whether msg is text for the focused widget.
func (m *model) capturesText(msg tea.KeyMsg) bool {
	// Before the first subscribe only the not-ready screen is drawn;
	// no pane is visible to take text.
	if !isTextKey(msg) || !m.ready {
		return false
	}
	switch m.focus {
	case focusConsole:
		return true
	case focusFiles:
		// While typing a filter, every printable key is filter text —
		// otherwise "q" in a filename would quit.
		return m.files.FilterState() == list.Filtering
	case focusTuning:
		// Only characters a number can contain; letters stay commands.
		return strings.Trim(string(msg.Runes), "0123456789.-") == ""
	case focusExtruder, focusOutputs:
		// Digits type a value; - stays retract (no row here is negative).
		return msg.Type == tea.KeyRunes && strings.Trim(string(msg.Runes), "0123456789.") == ""
	case focusJog:
		// Digits start typing a position. Number keys therefore don't
		// jump panes from here (as in Tuning); tab still moves on.
		return msg.Type == tea.KeyRunes && strings.Trim(string(msg.Runes), "0123456789.-") == ""
	}
	return false
}

// paneClaims reports whether the focused pane binds msg itself. A
// pane key wins over a job or Klipper key that shares it — in the
// Toolhead pane, c is Z down, not cancel print. Global navigation
// (help, setup, tab, quit) is never shadowed: no pane binds those.
func (m *model) paneClaims(msg tea.KeyMsg) bool {
	ps := paneScope(m.focus)
	for _, e := range registry {
		if e.scope == ps && !e.widget && key.Matches(msg, *e.b) {
			return true
		}
	}
	return false
}

// paneShadows reports whether the focused pane binds any of b's keys.
func (m *model) paneShadows(b *key.Binding) bool {
	for _, k := range b.Keys() {
		if m.paneClaims(keyMsgFor(k)) {
			return true
		}
	}
	return false
}

// keyMsgFor builds the KeyMsg for a single-character key ("c", " ").
// Only those can collide with job and Klipper keys; anything else
// returns a KeyMsg that matches nothing.
func keyMsgFor(k string) tea.KeyMsg {
	if k == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	if r := []rune(k); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}
	}
	return tea.KeyMsg{Type: -1000}
}
