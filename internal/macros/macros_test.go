package macros

import (
	"fmt"
	"testing"
)

func TestParams(t *testing.T) {
	tmpl := `
{% set bed = params.BED_TEMP|default(60)|float %}
{% set ext = params['EXTRUDER_TEMP'] | default( "210" ) %}
{% set mesh = params.get('MESH', 1) %}
{% if params.CHAMBER %}M141 S{params.CHAMBER}{% endif %}
{% for k in params.keys() %}{% endfor %}
{% set again = params.bed_temp|default(70) %}
{% set s = params.get("SPEED") %}`
	got := fmt.Sprint(Params(tmpl))
	want := "[{BED_TEMP 60} {EXTRUDER_TEMP 210} {MESH 1} {CHAMBER } {SPEED }]"
	if got != want {
		t.Fatalf("Params = %s\n want %s", got, want)
	}
}

func TestList(t *testing.T) {
	objects := []string{"gcode_macro PRINT_START", "gcode_macro _HELPER", "gcode_macro cancel_print", "fan", "gcode_macro LOAD"}
	settings := map[string]any{
		"gcode_macro print_start":  map[string]any{"description": "Start a print", "gcode": "G28\nM190 S{params.BED|default(60)}"},
		"gcode_macro cancel_print": map[string]any{"description": "G-Code macro", "gcode": "CANCEL_PRINT_BASE"},
	}
	got := List(objects, settings)
	if len(got) != 3 || got[0].Name != "CANCEL_PRINT" || got[1].Name != "LOAD" || got[2].Name != "PRINT_START" {
		t.Fatalf("List = %+v", got)
	}
	if got[0].Description != "" || got[2].Description != "Start a print" || fmt.Sprint(got[2].Params) != "[{BED 60}]" {
		t.Fatalf("details %+v", got)
	}
	if got[1].Params != nil { // no settings for LOAD: no params, still listed
		t.Fatalf("LOAD params %v", got[1].Params)
	}
}
