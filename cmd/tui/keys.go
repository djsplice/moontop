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
	scopeTuning
	scopeTemps
	scopeTempEdit
	scopeConsole
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
	case scopeTuning:
		return "Tuning"
	case scopeTemps:
		return "Temperatures"
	case scopeTempEdit:
		return "Temperatures, editing a target"
	case scopeConsole:
		return "Console"
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
	EStop, Quit, Help, Setup, NextPane, PrevPane, JumpPane, ToggleFilter *key.Binding
	Pause, Resume, Cancel                                                *key.Binding
	Restart, FwRestart                                                   *key.Binding
	Confirm, Deny, HelpScroll                                            *key.Binding
	FilesMove, FilesFilter, FilesStart                                   *key.Binding
	JogX, JogY, JogZ, StepDown, StepUp                                   *key.Binding
	HomeAll, HomeX, HomeY, HomeZ, MotorsOff                              *key.Binding
	TuneMove, TuneEdit, TuneApply                                        *key.Binding
	TempsMove, TempSet, TempOff, TempAllOff, TempPreset                  *key.Binding
	TempEditType, TempEditApply, TempEditCancel                          *key.Binding
	ConsoleSend, ConsoleHistory                                          *key.Binding
}{
	EStop:        bind([]string{"ctrl+x"}, "ctrl+x", "emergency stop (confirms)"),
	Quit:         bind([]string{"ctrl+c", "f10", "q"}, "q/F10", "quit"),
	Help:         bind([]string{"?", "f1"}, "?/F1", "help (this screen)"),
	Setup:        bind([]string{"f2", ","}, "F2/,", "setup: panes, heaters, filters, presets, safety"),
	NextPane:     bind([]string{"tab"}, "tab", "next pane"),
	PrevPane:     bind([]string{"shift+tab"}, "shift+tab", "previous pane"),
	JumpPane:     bind([]string{"1", "2", "3", "4", "5"}, "1–5", "jump to pane by number"),
	ToggleFilter: bind([]string{"ctrl+o"}, "ctrl+o", "show/hide filtered console lines"),

	Pause:  bind([]string{"p"}, "p", "pause print"),
	Resume: bind([]string{"r"}, "r", "resume print"),
	Cancel: bind([]string{"c"}, "c", "cancel print (confirms)"),

	Restart:   bind([]string{"K"}, "K", "restart Klipper (confirms)"),
	FwRestart: bind([]string{"F"}, "F", "firmware restart (confirms)"),

	Confirm:    bind([]string{"y", "Y"}, "y", "confirm a prompt"),
	Deny:       bind([]string{"n", "N", "esc"}, "n/esc", "cancel a prompt"),
	HelpScroll: bind([]string{"up", "down", "k", "j", "pgup", "pgdown"}, "↑↓ j k", "scroll help; esc or ? closes"),

	FilesMove:   bind([]string{"up", "down", "k", "j"}, "↑↓ j k", "select file"),
	FilesFilter: bind([]string{"/"}, "/", "filter files (esc clears)"),
	FilesStart:  bind([]string{"enter"}, "enter", "print selected file (confirms)"),

	JogX:      bind([]string{"left", "right"}, "← →", "jog X by the step"),
	JogY:      bind([]string{"up", "down"}, "↑ ↓", "jog Y by the step"),
	JogZ:      bind([]string{"pgup", "pgdown"}, "PgUp PgDn", "jog Z by the step"),
	StepDown:  bind([]string{"["}, "[", "smaller step"),
	StepUp:    bind([]string{"]"}, "]", "larger step"),
	HomeAll:   bind([]string{"H"}, "H", "home all axes"),
	HomeX:     bind([]string{"x"}, "x", "home X"),
	HomeY:     bind([]string{"y"}, "y", "home Y"),
	HomeZ:     bind([]string{"z"}, "z", "home Z"),
	MotorsOff: bind([]string{"m"}, "m", "motors off (M84)"),

	TuneMove:  bind([]string{"up", "down", "k", "j"}, "↑↓ j k", "select field"),
	TuneEdit:  bind([]string{}, "0–9 .", "type a value"),
	TuneApply: bind([]string{"enter"}, "enter", "apply changed fields"),

	TempsMove:  bind([]string{"up", "down", "k", "j"}, "↑↓ j k", "select heater or sensor"),
	TempSet:    bind([]string{"enter"}, "enter", "set target of the selected heater"),
	TempOff:    bind([]string{"x"}, "x", "turn the selected heater off"),
	TempAllOff: bind([]string{"X"}, "X", "cooldown: all heaters off"),
	TempPreset: bind([]string{"t"}, "t", "preheat from a preset"),

	TempEditType:   bind([]string{}, "0–9 .", "type the target in °C (0 = off)"),
	TempEditApply:  bind([]string{}, "enter", "send the new target"),
	TempEditCancel: bind([]string{}, "esc", "cancel"),

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

func havePresets(m *model) (bool, string) {
	return len(m.presets) > 0, "needs a preset (F2 → Presets, or Mainsail)"
}

func whileIdle(m *model) (bool, string) {
	return !jobActive(m.job.State), "when idle"
}

// registry is the ordered list help and footer render from.
var registry = []keyEntry{
	{b: keys.Help, scope: scopeGlobal, footer: true},
	{b: keys.Setup, scope: scopeGlobal},
	{b: keys.EStop, scope: scopeGlobal, footer: true},
	{b: keys.NextPane, scope: scopeGlobal, footer: true},
	{b: keys.PrevPane, scope: scopeGlobal},
	{b: keys.JumpPane, scope: scopeGlobal},
	{b: keys.ToggleFilter, scope: scopeGlobal},
	{b: keys.Quit, scope: scopeGlobal},

	{b: keys.Pause, scope: scopeJob, short: "pause", when: whilePrinting, footer: true},
	{b: keys.Resume, scope: scopeJob, short: "resume", when: whilePaused, footer: true},
	{b: keys.Cancel, scope: scopeJob, short: "cancel", when: whileJobActive, footer: true},

	{b: keys.Restart, scope: scopeKlipper},
	{b: keys.FwRestart, scope: scopeKlipper},

	{b: keys.FilesMove, scope: scopeFiles, short: "select", footer: true, widget: true},
	{b: keys.FilesFilter, scope: scopeFiles, short: "filter", footer: true, widget: true},
	{b: keys.FilesStart, scope: scopeFiles, short: "print", footer: true},

	{b: keys.JogX, scope: scopeToolhead, short: "X", footer: true},
	{b: keys.JogY, scope: scopeToolhead, short: "Y", footer: true},
	{b: keys.JogZ, scope: scopeToolhead, short: "Z", footer: true},
	{b: keys.StepDown, scope: scopeToolhead},
	{b: keys.StepUp, scope: scopeToolhead},
	{b: keys.HomeAll, scope: scopeToolhead, short: "home", footer: true},
	{b: keys.HomeX, scope: scopeToolhead},
	{b: keys.HomeY, scope: scopeToolhead},
	{b: keys.HomeZ, scope: scopeToolhead},
	{b: keys.MotorsOff, scope: scopeToolhead, when: whileIdle},

	{b: keys.TuneMove, scope: scopeTuning, short: "field", footer: true},
	{b: keys.TuneEdit, scope: scopeTuning, short: "type", footer: true},
	{b: keys.TuneApply, scope: scopeTuning, short: "apply", footer: true},

	{b: keys.TempsMove, scope: scopeTemps, short: "select", footer: true, widget: true},
	{b: keys.TempSet, scope: scopeTemps, short: "set", footer: true},
	{b: keys.TempPreset, scope: scopeTemps, short: "preheat", footer: true, when: havePresets},
	{b: keys.TempOff, scope: scopeTemps, short: "off", footer: true},
	{b: keys.TempAllOff, scope: scopeTemps, short: "all off", footer: true},

	{b: keys.TempEditType, scope: scopeTempEdit, short: "type °C", footer: true},
	{b: keys.TempEditApply, scope: scopeTempEdit, short: "set", footer: true},
	{b: keys.TempEditCancel, scope: scopeTempEdit, short: "cancel", footer: true},

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
			if m.focus != focusConsole {
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
	if !isTextKey(msg) {
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
	}
	return false
}
