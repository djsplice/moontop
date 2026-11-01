package main

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/config"
	"github.com/justinh-rahb/moontop/internal/moonraker"
)

func TestWideLayoutFillsTheRowExactly(t *testing.T) {
	for _, w := range []int{100, 120, 160, 237} {
		l := computeLayout(defaultLayoutInput(w, 50, 4))
		sum := 0
		for _, f := range l.order {
			sum += l.w(f)
		}
		if l.mode == layoutWide && sum != w-6 {
			t.Errorf("%d cols: panes sum to %d, want %d", w, sum, w-6)
		}
	}
	// Minimums that can't fit fall back to stacking.
	in := defaultLayoutInput(95, 50, 4)
	if computeLayout(in).mode != layoutStacked {
		t.Error("95 columns can't fit the minimums; should stack")
	}
}

func TestReorderPanes(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m = openCategory(t, m, "Panes")
	m = press(t, m, "down", "down", "down", "[", "[", "[") // Temperatures to the front
	if got := strings.Join(m.cfg.Panes.Ordered(), ","); got != "Temperatures,Files,Toolhead,Tuning" {
		t.Fatalf("order %s", got)
	}
	if m.setup.item != 0 {
		t.Fatalf("cursor didn't follow the moved pane: %d", m.setup.item)
	}
	if !strings.Contains(fileText(t, m), `order = ["Temperatures", "Files", "Toolhead", "Tuning"]`) {
		t.Fatalf("file %q", fileText(t, m))
	}
	m = press(t, m, "esc", "esc")
	if m.layout.order[0] != focusTable {
		t.Fatalf("layout order %v", m.layout.order)
	}
	first := strings.Index(m.View(), "4 Temperatures")
	if first < 0 || first > strings.Index(m.View(), "1 Files") {
		t.Fatal("Temperatures not drawn first")
	}
	// Tab follows the screen; numbers stay with the pane.
	m.setFocus(focusTable)
	m = press(t, m, "tab")
	if m.focus != focusFiles {
		t.Fatalf("tab from Temperatures → %v", m.focus)
	}
	m = press(t, m, "4")
	if m.focus != focusTable {
		t.Fatal("4 should still be Temperatures")
	}
}

func TestPaneWidthAndConsoleHeight(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	before := m.layout
	m = openCategory(t, m, "Panes")
	m = press(t, m, "enter") // Files width
	m.setup.input.SetValue("0")
	m = press(t, m, "enter")
	if m.layout.w(focusFiles) >= before.w(focusFiles) || m.layout.w(focusTable) <= before.w(focusTable) {
		t.Fatalf("files natural width: files %d→%d table %d→%d", before.w(focusFiles), m.layout.w(focusFiles), before.w(focusTable), m.layout.w(focusTable))
	}
	m = press(t, m, "down", "down", "down", "down", "down", "enter") // Console height (after Extruder)
	m.setup.input.SetValue("5")
	m = press(t, m, "enter")
	if !strings.Contains(m.setup.status, "15 to 85") {
		t.Fatalf("bad height accepted: %q", m.setup.status)
	}
	m.setup.input.SetValue("60")
	m = press(t, m, "enter")
	availH := 50 - 5
	if want := availH - availH*60/100; m.layout.topRowH != want {
		t.Fatalf("top row %d, want %d", m.layout.topRowH, want)
	}
	if !strings.Contains(fileText(t, m), "console_height = 60") || !strings.Contains(fileText(t, m), "Files = 0") {
		t.Fatalf("file %q", fileText(t, m))
	}
}

func TestPowerColumn(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	// Subscribed for every heater/sensor.
	var params struct {
		Objects map[string][]string `json:"objects"`
	}
	_ = json.Unmarshal(srv.CallsTo("printer.objects.subscribe")[0].Params, &params)
	if !strings.HasPrefix(strings.Join(params.Objects["extruder"], ","), "temperature,target,power") {
		t.Fatalf("extruder fields %v", params.Objects["extruder"])
	}
	m = openCategory(t, m, "Columns")
	m = press(t, m, "down", "down", "down", "space") // power
	m = press(t, m, "esc", "esc")
	if strings.Join(m.tempCols, ",") != "current,target,power,history" {
		t.Fatalf("cols %v", m.tempCols)
	}
	m = update(t, m, notificationMsg{Method: moonraker.MethodStatusUpdate,
		Params: rawParams(t, map[string]any{"extruder": map[string]any{"power": 0.42}}, 1.0)})
	tv := m.table.View()
	if !strings.Contains(tv, "Power") || !strings.Contains(tv, "42%") {
		t.Fatalf("table:\n%s", tv)
	}
	// Narrow: State goes first, then Power.
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 50})
	if strings.Join(m.tempCols, ",") != "current,target" {
		t.Fatalf("narrow cols %v", m.tempCols)
	}
}

func TestFileColumnsAndSort(t *testing.T) {
	srv := fakePrinter(t)
	m := setupModel(t, srv)
	m = update(t, m, filesLoadedMsg{files: []moonraker.FileInfo{
		{Path: "b-newer.gcode", Size: 2048, ModifiedTime: 2000},
		{Path: "a-older.gcode", Size: 1024, ModifiedTime: 1000},
	}})
	first := func() fileItem { return m.files.Items()[0].(fileItem) }
	if first().info.Path != "b-newer.gcode" || first().Description() != "2.0 KB" {
		t.Fatalf("default: %q %q", first().info.Path, first().Description())
	}
	m = openCategory(t, m, "Columns")
	for i := 0; i < len(config.TempColumns)+1; i++ { // to "Files: Modified"
		m = press(t, m, "down")
	}
	m = press(t, m, "space", "down", "space") // modified on, sort by name
	if first().info.Path != "a-older.gcode" {
		t.Fatalf("sort by name: %q", first().info.Path)
	}
	if d := first().Description(); !strings.HasPrefix(d, "1.0 KB  ·  19") {
		t.Fatalf("description %q", d)
	}
	if !strings.Contains(fileText(t, m), `files_sort = "name"`) {
		t.Fatalf("file %q", fileText(t, m))
	}
}
