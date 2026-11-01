package main

import (
	"strings"
	"testing"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker/moonrakertest"
)

func TestTemperatureSparklines(t *testing.T) {
	srv := fakePrinter(t)
	// Five minutes of history: the extruder heating from 25 to 210 °C.
	var ramp, flat []float64
	for i := 0; i < 300; i++ {
		ramp = append(ramp, 25+float64(i)*185/299)
		flat = append(flat, 24)
	}
	srv.Handle("server.temperature_store", moonrakertest.Result(map[string]any{
		"extruder":   map[string]any{"temperatures": ramp, "targets": []float64{210}},
		"heater_bed": map[string]any{"temperatures": flat},
		"not_shown":  map[string]any{"temperatures": flat},
	}))
	m := readyModel(t, srv)
	m = update(t, m, loadTempStoreCmd(m.client)())

	cell := func(label string) string {
		for _, r := range m.table.Rows() {
			if r[0] == label {
				return r[len(r)-1]
			}
		}
		t.Fatalf("no row %q", label)
		return ""
	}
	ext, bed := cell("Extruder"), cell("Bed")
	if len([]rune(ext)) != m.historyW || []rune(ext)[0] != '⣀' || []rune(ext)[m.historyW-1] != '⣿' {
		t.Fatalf("extruder ramp = %q (width %d)", ext, m.historyW)
	}
	if strings.Trim(bed, "⣀") != "" {
		t.Fatalf("steady bed should sit on the bottom row: %q", bed)
	}
	if _, ok := m.tempHist["not_shown"]; ok {
		t.Fatal("history kept for an object we don't show")
	}
	// Sampling appends.
	before := len(m.tempHist["extruder"].Samples())
	m = update(t, m, sampleMsg{})
	if len(m.tempHist["extruder"].Samples()) != before+1 {
		t.Fatal("sample not recorded")
	}
	if !strings.Contains(m.View(), "History") {
		t.Fatal("History column missing at 160 columns")
	}
}
