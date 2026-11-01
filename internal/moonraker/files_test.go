package moonraker_test

import (
	"encoding/json"
	"testing"

	"github.com/justinh-rahb/moontop/internal/moonraker"
)

// A Qidi Q2's listing: its slicer writes numbers as strings, and some
// fields as lists or null. None of that may fail the whole folder.
func TestDirectoryLenientMetadata(t *testing.T) {
	raw := `{"dirs": [], "disk_usage": {"total": 1e10, "used": 1, "free": 5e9}, "files": [
		{"filename": "q2.gcode", "modified": 1.7e9, "size": 100,
		 "layer_height": "0.2", "first_layer_height": "0.25 ", "object_height": null,
		 "estimated_time": "1234", "filament_total": [512.5, 10], "filament_weight_total": "1,5",
		 "nozzle_diameter": "nan?", "filament_type": ["PLA", "PETG"], "slicer": "QIDIStudio",
		 "slicer_version": 2.0, "print_start_time": null, "first_layer_extr_temp": "220;220"},
		{"filename": "plain.gcode", "modified": 1, "size": 1}]}`
	var d moonraker.Directory
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	f := d.Files[0]
	checks := map[string][2]float64{
		"layer_height":   {float64(f.LayerHeight), 0.2},
		"first_layer":    {float64(f.FirstLayerHeight), 0.25},
		"object_height":  {float64(f.ObjectHeight), 0},
		"estimated_time": {float64(f.EstimatedTime), 1234},
		"filament_total": {float64(f.FilamentTotal), 512.5},
		"weight":         {float64(f.FilamentWeightTotal), 1},
		"nozzle":         {float64(f.NozzleDiameter), 0},
		"extr_temp":      {float64(f.FirstLayerExtrTemp), 220},
		"start":          {float64(f.PrintStartTime), 0},
	}
	for k, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s = %v, want %v", k, v[0], v[1])
		}
	}
	if f.FilamentType != "PLA, PETG" || f.Slicer != "QIDIStudio" || f.SlicerVersion != "2.0" {
		t.Errorf("text fields: %q %q %q", f.FilamentType, f.Slicer, f.SlicerVersion)
	}
	if len(d.Files) != 2 || d.Disk.Free != 5e9 {
		t.Errorf("listing: %+v", d)
	}
}
