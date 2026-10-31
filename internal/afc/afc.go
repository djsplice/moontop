// Package afc reads Armored Turtle's AFC (AFC-Klipper-Add-On) status
// objects into a view model and builds its gcode commands.
//
// The model follows Fluidd's AFC implementation (the most complete
// client), with the compatibility rules from Justin's retrograde
// (github.com/justinh-rahb/retrograde, src/lib/afc.js), which were
// validated against a Snapmaker U1 running AFC-Lite:
//
//   - AFC-Lite reports lane status "empty" and filament_status "unknown"
//     even with filament loaded, so the lane state comes from the
//     sensor booleans (tool_loaded / load / prep), and the active lane
//     comes from AFC.current_load / current_lane, separately — several
//     lanes can be "in tool" on a toolchanger.
//   - AFC.message may be a plain string or {message, type}.
//   - Units are "type name" strings ("Box_Turtle Turtle_1",
//     "unit U1"); the unit object is "AFC_<type without _> <name>",
//     matched case-insensitively.
//   - Lane objects are "AFC_lane <name>" or the older "AFC_stepper <name>".
//   - A weight that isn't reported is unknown, not zero.
package afc

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// IsObject reports whether a Klipper object belongs to AFC.
func IsObject(name string) bool { return name == "AFC" || strings.HasPrefix(name, "AFC_") }

// Lane is one AFC lane as the UI shows it.
type Lane struct {
	Name       string
	Unit       string
	Extruder   string
	Tools      []string // from map (string or list)
	Material   string   // "" when unset or "NONE"
	Filament   string   // filament_name
	Color      string   // "#rrggbb", "" when unknown
	Weight     float64
	HasWeight  bool
	SpoolID    int
	Runout     string
	Load, Prep bool
	ToolLoaded bool
	Active     bool   // the lane AFC considers current
	State      string // In tool / Ready / Prep / Empty / Unknown, or a busy status
}

// Unit groups lanes.
type Unit struct {
	Name, Type string
	Lanes      []*Lane
}

// Model is the AFC view of the printer.
type Model struct {
	State       string // current_state as reported
	Current     string // current lane name
	Next        string
	Error       bool
	Message     string
	MessageType string
	Units       []Unit
	Lanes       []*Lane // all lanes, unit order
}

// Parse builds the model from raw objects (object name → fields). It
// returns nil when the printer has no AFC.
func Parse(objects map[string]map[string]any) *Model {
	afc := objects["AFC"]
	if len(afc) == 0 {
		return nil
	}
	m := &Model{
		State: str(afc["current_state"]),
		Error: afc["error_state"] == true,
		Next:  str(afc["next_lane"]),
	}
	if m.State == "" {
		m.State = "Unknown"
	}
	m.Current = str(afc["current_load"])
	if m.Current == "" {
		m.Current = str(afc["current_lane"])
	}
	switch msg := afc["message"].(type) {
	case string:
		m.Message = msg
	case map[string]any:
		m.Message, m.MessageType = str(msg["message"]), str(msg["type"])
	}

	// Lane names: AFC.lanes first, then any lane objects it didn't list.
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, n := range strs(afc["lanes"]) {
		add(n)
	}
	var extra []string
	for k := range objects {
		if strings.HasPrefix(k, "AFC_lane ") || strings.HasPrefix(k, "AFC_stepper ") {
			extra = append(extra, k[strings.IndexByte(k, ' ')+1:])
		}
	}
	sort.Strings(extra)
	for _, n := range extra {
		add(n)
	}

	byName := map[string]*Lane{}
	var lanes []*Lane
	for _, n := range names {
		raw := objects["AFC_stepper "+n]
		if raw == nil {
			raw = objects["AFC_lane "+n]
		}
		if raw == nil {
			continue
		}
		l := parseLane(n, raw, m.Current)
		byName[n] = l
		lanes = append(lanes, l)
	}

	// Units, in AFC.units order; lanes not claimed by any unit go last.
	assigned := map[string]bool{}
	for _, u := range strs(afc["units"]) {
		typ, name := u, u
		if i := strings.IndexByte(u, ' '); i >= 0 {
			typ, name = u[:i], u[i+1:]
		}
		want := strings.ToLower("AFC_" + strings.ReplaceAll(typ, "_", "") + " " + name)
		var members []string
		for k, v := range objects {
			if strings.ToLower(k) == want {
				members = strs(v["lanes"])
			}
		}
		unit := Unit{Name: name, Type: typ}
		for _, l := range lanes {
			if contains(members, l.Name) || l.Unit == name {
				if !assigned[l.Name] {
					unit.Lanes = append(unit.Lanes, l)
					assigned[l.Name] = true
				}
			}
		}
		m.Units = append(m.Units, unit)
	}
	var rest []*Lane
	for _, l := range lanes {
		if !assigned[l.Name] {
			rest = append(rest, l)
		}
	}
	if len(rest) > 0 {
		m.Units = append(m.Units, Unit{Name: "Lanes", Lanes: rest})
	}
	for _, u := range m.Units {
		m.Lanes = append(m.Lanes, u.Lanes...)
	}
	return m
}

// statuses AFC reports when nothing is happening; anything else (Tool
// Loading, Ejecting, Calibrating…) is shown as the lane's state.
var idleStatuses = map[string]bool{"": true, "none": true, "empty": true, "loaded": true, "tooled": true, "tool loaded": true}

func parseLane(name string, raw map[string]any, current string) *Lane {
	l := &Lane{
		Name:       name,
		Unit:       str(raw["unit"]),
		Extruder:   str(raw["extruder"]),
		Filament:   str(raw["filament_name"]),
		Runout:     str(raw["runout_lane"]),
		Load:       raw["load"] == true,
		Prep:       raw["prep"] == true,
		ToolLoaded: raw["tool_loaded"] == true,
		Active:     name == current,
	}
	switch v := raw["map"].(type) {
	case string:
		if v != "" {
			l.Tools = []string{v}
		}
	case []any:
		l.Tools = strs(v)
	}
	if mat := str(raw["material"]); mat != "" && !strings.EqualFold(mat, "NONE") {
		l.Material = mat
	}
	if c := str(raw["color"]); colorRe.MatchString(c) {
		l.Color = "#" + strings.ToLower(strings.TrimPrefix(c, "#"))
	}
	if w, ok := raw["weight"].(float64); ok {
		l.Weight, l.HasWeight = w, true
	}
	if id, ok := raw["spool_id"].(float64); ok {
		l.SpoolID = int(id)
	}
	status := str(raw["status"])
	switch {
	case !idleStatuses[strings.ToLower(status)]:
		l.State = status
	case l.ToolLoaded:
		l.State = "In tool"
	case l.Load && l.Prep:
		l.State = "Ready"
	case l.Prep:
		l.State = "Prep"
	case raw["prep"] == false:
		l.State = "Empty"
	default:
		l.State = "Unknown"
	}
	return l
}

var colorRe = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)

// Lane returns the named lane, or nil.
func (m *Model) Lane(name string) *Lane {
	for _, l := range m.Lanes {
		if l.Name == name {
			return l
		}
	}
	return nil
}

// Busy reports whether AFC is in the middle of something (anything but
// idle/initialized/error, case-insensitive: AFC-Lite says "idle").
func (m *Model) Busy() bool {
	switch strings.ToLower(m.State) {
	case "idle", "initialized", "error", "unknown":
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

// Action names map to AFC commands.
const (
	Load   = "CHANGE_TOOL"
	Unload = "TOOL_UNLOAD"
	Eject  = "LANE_UNLOAD"
	Map    = "SET_MAP"
	SetMat = "SET_MATERIAL"
	SetWt  = "SET_WEIGHT"
	SetCol = "SET_COLOR"
	SetSpl = "SET_SPOOL_ID"
	Clear  = "AFC_CLEAR_MESSAGE"
	Runout = "SET_RUNOUT"
)

// Param formats a gcode parameter value. Values containing gcode
// delimiters are refused rather than escaped — a lane name or material
// must never be able to add a command — and whitespace is quoted.
func Param(v string) (string, error) {
	if v == "" || len(v) > 120 || strings.ContainsAny(v, "\"'\\#;=") || strings.IndexFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", fmt.Errorf("unsupported characters in %q", v)
	}
	if strings.ContainsAny(v, " \t") {
		return `"` + v + `"`, nil
	}
	return v, nil
}

var toolRe = regexp.MustCompile(`^T\d{1,3}$`)

// LaneCommand checks a lane action against the lane's state and returns
// its gcode. The checks are retrograde's: load needs a prepared lane
// that isn't already the active loaded one; unload needs filament in
// the tool; eject needs it out of the tool first.
func LaneCommand(l *Lane, command string) (string, error) {
	name, err := Param(l.Name)
	if err != nil {
		return "", err
	}
	switch command {
	case Load:
		if !l.Prep || !l.Load {
			return "", fmt.Errorf("lane %s has no prepared filament to load", l.Name)
		}
		if l.Active && l.ToolLoaded {
			return "", fmt.Errorf("lane %s is already the active loaded lane", l.Name)
		}
	case Unload:
		if !l.ToolLoaded {
			return "", fmt.Errorf("lane %s has no filament in the tool", l.Name)
		}
	case Eject:
		if l.ToolLoaded {
			return "", fmt.Errorf("unload lane %s from the tool before ejecting it", l.Name)
		}
	default:
		return "", fmt.Errorf("unknown lane action %s", command)
	}
	return command + " LANE=" + name, nil
}

// SetCommand builds a lane setting edit from user input.
func SetCommand(l *Lane, command, value string) (string, error) {
	name, err := Param(l.Name)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	var arg string
	switch command {
	case Map:
		v := strings.ToUpper(value)
		if !toolRe.MatchString(v) {
			return "", fmt.Errorf("enter a tool such as T0")
		}
		arg = "MAP=" + v
	case SetMat:
		v, err := Param(value)
		if err != nil {
			return "", err
		}
		arg = "MATERIAL=" + v
	case SetCol:
		if !colorRe.MatchString(value) {
			return "", fmt.Errorf("enter a six-digit hex colour such as FF8800")
		}
		arg = "COLOR=" + strings.ToUpper(strings.TrimPrefix(value, "#"))
	case SetWt:
		var w float64
		if _, err := fmt.Sscanf(value, "%g", &w); err != nil || w < 0 {
			return "", fmt.Errorf("enter a weight in grams, 0 or more")
		}
		arg = "WEIGHT=" + strings.TrimSpace(value)
	case SetSpl:
		var id int
		if _, err := fmt.Sscanf(value, "%d", &id); err != nil || id < 0 {
			return "", fmt.Errorf("enter a spool ID, 0 to clear")
		}
		arg = "SPOOL_ID="
		if id > 0 {
			arg += fmt.Sprint(id)
		}
	default:
		return "", fmt.Errorf("unknown setting %s", command)
	}
	return command + " LANE=" + name + " " + arg, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func strs(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
