package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsWhenEmpty(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Console.WaitTempsHidden() || !c.Console.RunCurrentHidden() {
		t.Error("filters should default on")
	}
	if !c.Safety.EStopConfirms() || !c.Safety.RestartConfirmsIdle() {
		t.Error("confirmations should default on")
	}
	if c.Toolhead.XY() != 1500 || c.Toolhead.Z() != 600 || len(c.Toolhead.Steps()) != 6 {
		t.Errorf("toolhead defaults %d %d %v", c.Toolhead.XY(), c.Toolhead.Z(), c.Toolhead.Steps())
	}
}

func TestSaveStoresOnlyChangesAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := Save(path, &Config{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if body := strings.TrimSpace(strings.TrimPrefix(string(data), fileHeader)); body != "" {
		t.Fatalf("empty config wrote settings:\n%s", body)
	}

	c := &Config{}
	c.Panes.SetPaneHidden("Tuning", true)
	c.Console.HideWaitTemps = Bool(false)
	c.Console.Filters = []Filter{{Name: "ok lines", Pattern: `^ok$`}}
	c.Toolhead.StepSizes = []float64{5, 0.5}
	c.Safety.ConfirmRestartIdle = Bool(false)
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	for _, want := range []string{"hidden = [\"Tuning\"]", "hide_wait_temperatures = false", "pattern = \"^ok$\"", "confirm_restart_when_idle = false"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("file missing %q:\n%s", want, data)
		}
	}
	for _, unwanted := range []string{"hide_run_current", "confirm_estop", "xy_feedrate", "[temperatures]"} {
		if strings.Contains(string(data), unwanted) {
			t.Errorf("file stored default %q:\n%s", unwanted, data)
		}
	}

	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Panes.PaneHidden("Tuning") || back.Console.WaitTempsHidden() || !back.Console.RunCurrentHidden() ||
		back.Safety.RestartConfirmsIdle() || back.Toolhead.Steps()[0] != 0.5 || back.Console.Filters[0].Pattern != `^ok$` {
		t.Fatalf("round trip lost settings: %+v", back)
	}
}

func TestLoadReportsBadTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(path, []byte("[panes\nhidden = 3"), 0o644)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	if p, _ := DefaultPath(); p != "/x/cfg/moontop/config.toml" {
		t.Fatal(p)
	}
}

func TestPrinterSettings(t *testing.T) {
	p, err := ParsePrinter(json.RawMessage(`{"temperatures":{"hidden":["temperature_sensor mcu"],"rename":{"extruder1":"T1 ASA"}},"presets":[{"name":"PLA","targets":{"extruder":210}}],"future":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Temperatures.IsHidden("temperature_sensor mcu") || p.Temperatures.Label("extruder1") != "T1 ASA" || p.Presets[0].Targets["extruder"] != 210 {
		t.Fatalf("parsed %+v", p)
	}
	file := Temperatures{Hidden: []string{"heater_bed"}}
	if eff := p.EffectiveTemperatures(file); eff.IsHidden("heater_bed") {
		t.Error("printer settings should replace the file's")
	}
	empty := Printer{}
	if eff := empty.EffectiveTemperatures(file); !eff.IsHidden("heater_bed") {
		t.Error("file settings should apply when the printer has none")
	}
	p.Temperatures.SetLabel("extruder1", "")
	p.Temperatures.SetHidden("temperature_sensor mcu", false)
	if p.Temperatures.Rename != nil || p.Temperatures.Hidden != nil {
		t.Errorf("clearing left %+v", p.Temperatures)
	}
	if _, err := ParsePrinter(nil); err != nil {
		t.Error(err)
	}
}

func TestValidateFilter(t *testing.T) {
	if ValidateFilter(`^ok$`) != nil || ValidateFilter(`(`) == nil || ValidateFilter("") == nil {
		t.Fatal("validation wrong")
	}
}

func TestPaneOrderAndWidths(t *testing.T) {
	p := Panes{Order: []string{"Temperatures", "Bogus", "Files", "Files"}}
	if got := strings.Join(p.Ordered(), ","); got != "Temperatures,Files,Toolhead,Tuning" {
		t.Fatalf("Ordered = %s", got)
	}
	p = Panes{}
	p.Move("Tuning", -1)
	if got := strings.Join(p.Ordered(), ","); got != "Files,Tuning,Toolhead,Temperatures" {
		t.Fatalf("after move: %s", got)
	}
	p.Move("Tuning", 1) // back to default order: stored as nothing
	if p.Order != nil {
		t.Fatalf("default order stored: %v", p.Order)
	}
	p.Move("Files", -1) // already first: no-op
	if p.Order != nil {
		t.Fatal("out-of-range move changed the order")
	}
	if p.WidthOf("Temperatures") != 2 || p.WidthOf("Toolhead") != 0 {
		t.Fatal("default widths")
	}
	p.SetWidth("Toolhead", 3)
	p.SetWidth("Files", 1) // default: not stored
	if p.WidthOf("Toolhead") != 3 || len(p.Width) != 1 {
		t.Fatalf("widths %v", p.Width)
	}
	p.SetWidth("Toolhead", 0)
	if p.Width != nil {
		t.Fatalf("default width stored: %v", p.Width)
	}
}

func TestColumns(t *testing.T) {
	var c Columns
	if strings.Join(c.Temps(), ",") != "state,current,target,history" || strings.Join(c.FileCols(), ",") != "size" {
		t.Fatal("defaults")
	}
	c.ToggleTemp("power")
	c.ToggleTemp("state")
	c.ToggleTemp("history")
	if got := strings.Join(c.Temps(), ","); got != "current,target,power" {
		t.Fatalf("temps %s", got)
	}
	if got := strings.Join(c.Temperatures, ","); got != "current,target,power" {
		t.Fatalf("stored out of display order: %s", got)
	}
	c.ToggleTemp("current")
	c.ToggleTemp("power")
	if c.ToggleTemp("target") {
		t.Fatal("removing the last column should be refused")
	}
	c.ToggleFile("size")
	if len(c.FileCols()) != 0 {
		t.Fatalf("no file columns: %v", c.FileCols())
	}
	c.ToggleFile("size")
	if c.Files != nil {
		t.Fatalf("back to default should store nothing: %v", c.Files)
	}
}
