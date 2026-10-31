package afc

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var objs map[string]map[string]any
	if err := json.Unmarshal(data, &objs); err != nil {
		t.Fatal(err)
	}
	return objs
}

// testdata/afc-lite-u1.json is the sanitized status of a Snapmaker U1
// running AFC-Lite, from retrograde's tests.
func TestU1AFCLite(t *testing.T) {
	m := Parse(loadFixture(t, "../../testdata/afc-lite-u1.json"))
	if m == nil {
		t.Fatal("no model")
	}
	if m.State != "idle" || m.Busy() || m.Error || m.Message != "" || m.Current != "E1" {
		t.Fatalf("model %+v", m)
	}
	if len(m.Units) != 1 || m.Units[0].Name != "U1" || m.Units[0].Type != "unit" || len(m.Units[0].Lanes) != 4 {
		t.Fatalf("units %+v", m.Units)
	}
	var got []string
	for _, l := range m.Lanes {
		got = append(got, l.Name+":"+l.State)
	}
	// "status": "empty" on every lane is ignored in favour of sensors.
	if strings.Join(got, ",") != "E0:In tool,E1:In tool,E2:In tool,E3:Empty" {
		t.Fatalf("states %v", got)
	}
	e0, e1, e3 := m.Lane("E0"), m.Lane("E1"), m.Lane("E3")
	if e0.Active || !e1.Active {
		t.Error("only the current lane is active, even though three are in tool")
	}
	if e0.Material != "ASA" || e0.Filament != "Snapmaker ASA" || e0.Color != "#161616" || e0.Tools[0] != "T0" || e0.Extruder != "extruder" {
		t.Errorf("E0 %+v", e0)
	}
	if e3.Material != "" || e0.HasWeight {
		t.Errorf("NONE material should be empty, absent weight unknown: %+v %+v", e3, e0)
	}
}

func TestFullAFC(t *testing.T) {
	objs := map[string]map[string]any{
		"AFC": {"current_state": "Loading", "current_load": "lane2", "error_state": true,
			"message": map[string]any{"message": "Lane 2 jammed", "type": "error"},
			"units":   []any{"Box_Turtle Turtle_1"}, "lanes": []any{"lane1", "lane2"}},
		"AFC_BoxTurtle Turtle_1": {"lanes": []any{"lane1", "lane2"}},
		"AFC_stepper lane1":      {"map": []any{"T0", "T4"}, "load": true, "prep": true, "weight": 850.0, "color": "FF8800", "status": "None"},
		"AFC_stepper lane2":      {"map": "T1", "load": true, "prep": true, "status": "Tool Loading"},
		"AFC_stepper lane9":      {"map": "T9", "prep": true}, // not in AFC.lanes: not a lane
	}
	m := Parse(objs)
	if !m.Busy() || !m.Error || m.Message != "Lane 2 jammed" || m.MessageType != "error" || m.Current != "lane2" {
		t.Fatalf("model %+v", m)
	}
	if len(m.Units) != 1 || m.Units[0].Name != "Turtle_1" || len(m.Units[0].Lanes) != 2 || len(m.Lanes) != 2 {
		t.Fatalf("units %+v", m.Units)
	}
	l1, l2 := m.Lane("lane1"), m.Lane("lane2")
	if l1.State != "Ready" || strings.Join(l1.Tools, ",") != "T0,T4" || !l1.HasWeight || l1.Weight != 850 || l1.Color != "#ff8800" {
		t.Errorf("lane1 %+v", l1)
	}
	if l2.State != "Tool Loading" || !l2.Active {
		t.Errorf("lane2 %+v", l2)
	}
	if m.Lane("lane9") != nil {
		t.Error("a stepper missing from AFC.lanes was shown as a lane")
	}
	if Parse(map[string]map[string]any{"extruder": {}}) != nil {
		t.Error("no AFC object → nil model")
	}
}

func TestLaneCommands(t *testing.T) {
	m := Parse(loadFixture(t, "../../testdata/afc-lite-u1.json"))
	e0, e1, e3 := m.Lane("E0"), m.Lane("E1"), m.Lane("E3")
	cases := []struct {
		lane    *Lane
		command string
		want    string // "" = error expected
	}{
		{e0, Load, "CHANGE_TOOL LANE=E0"},
		{e1, Load, ""}, // already the active loaded lane
		{e3, Load, ""}, // nothing prepared
		{e0, Unload, "TOOL_UNLOAD LANE=E0"},
		{e3, Unload, ""},
		{e0, Eject, ""}, // still in the tool
		{e3, Eject, "LANE_UNLOAD LANE=E3"},
	}
	for _, c := range cases {
		got, err := LaneCommand(c.lane, c.command)
		if (c.want == "") != (err != nil) || got != c.want {
			t.Errorf("%s %s = %q, %v; want %q", c.command, c.lane.Name, got, err, c.want)
		}
	}
}

func TestSetCommands(t *testing.T) {
	l := &Lane{Name: "E2"}
	ok := map[[2]string]string{
		{Map, "t3"}:          "SET_MAP LANE=E2 MAP=T3",
		{SetMat, "PETG"}:     "SET_MATERIAL LANE=E2 MATERIAL=PETG",
		{SetMat, "PLA Silk"}: `SET_MATERIAL LANE=E2 MATERIAL="PLA Silk"`,
		{SetCol, "#ff8800"}:  "SET_COLOR LANE=E2 COLOR=FF8800",
		{SetWt, "850"}:       "SET_WEIGHT LANE=E2 WEIGHT=850",
		{SetSpl, "12"}:       "SET_SPOOL_ID LANE=E2 SPOOL_ID=12",
		{SetSpl, "0"}:        "SET_SPOOL_ID LANE=E2 SPOOL_ID=",
	}
	for in, want := range ok {
		got, err := SetCommand(l, in[0], in[1])
		if err != nil || got != want {
			t.Errorf("%s %q = %q, %v; want %q", in[0], in[1], got, err, want)
		}
	}
	bad := [][2]string{{Map, "tool1"}, {SetMat, "PLA; M112"}, {SetMat, `a"b`}, {SetCol, "red"}, {SetWt, "-5"}, {SetSpl, "x"}}
	for _, in := range bad {
		if got, err := SetCommand(l, in[0], in[1]); err == nil {
			t.Errorf("%s %q accepted as %q", in[0], in[1], got)
		}
	}
	if _, err := LaneCommand(&Lane{Name: "E0; M112", Prep: true, Load: true}, Load); err == nil {
		t.Error("lane name with a gcode delimiter accepted")
	}
}

// A ViViD has drive and selector steppers next to its lanes; only the
// names in AFC.lanes are lanes.
func TestViViDDriveAndSelectorAreNotLanes(t *testing.T) {
	objs := map[string]map[string]any{
		"AFC":                          {"current_state": "Idle", "units": []any{"ViViD Vivid_1"}, "lanes": []any{"lane5", "lane6"}},
		"AFC_ViViD Vivid_1":            {"lanes": []any{"lane5", "lane6", "Vivid_1_drive", "Vivid_1_selector"}},
		"AFC_lane lane5":               {"unit": "Vivid_1", "map": "T5", "prep": false},
		"AFC_lane lane6":               {"unit": "Vivid_1", "map": "T6", "prep": true, "load": true},
		"AFC_stepper Vivid_1_drive":    {"unit": "Vivid_1"},
		"AFC_stepper Vivid_1_selector": {"unit": "Vivid_1"},
	}
	m := Parse(objs)
	var names []string
	for _, l := range m.Lanes {
		names = append(names, l.Name)
	}
	if strings.Join(names, ",") != "lane5,lane6" || len(m.Units) != 1 || len(m.Units[0].Lanes) != 2 {
		t.Fatalf("lanes %v units %+v", names, m.Units)
	}

	// An AFC too old to report lanes falls back to the lane objects.
	delete(objs["AFC"], "lanes")
	delete(objs, "AFC_stepper Vivid_1_drive")
	delete(objs, "AFC_stepper Vivid_1_selector")
	if m := Parse(objs); len(m.Lanes) != 2 {
		t.Fatalf("fallback lanes %d", len(m.Lanes))
	}
}
