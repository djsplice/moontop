package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
	"github.com/justinh-rahb/moonraker-tui/internal/spark"
)

// ---------------------------------------------------------------------------
// Temperature sparklines (Temperatures' History column)
//
// One sample a second per heater and sensor, kept for 20 minutes (what
// Moonraker's temperature store holds, which fills them at startup).
// The column shows the last historyWindow seconds as braille.
// ---------------------------------------------------------------------------

const (
	historyCap    = 1200 // samples: 20 minutes at 1/s
	historyWindow = 300  // seconds shown
	historyMinW   = 8    // column width bounds (8 = 16 columns of dots)
	historyMaxW   = 30
	historyNameW  = 18 // the name column keeps this before History grows
)

type sampleMsg struct{}

func sampleTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return sampleMsg{} })
}

type tempStoreMsg struct {
	store map[string]moonraker.TempHistory
	err   error
}

func loadTempStoreCmd(c *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		s, err := c.TemperatureStore()
		return tempStoreMsg{store: s, err: err}
	}
}

func (m *model) ring(name string) *spark.Ring {
	if m.tempHist == nil {
		m.tempHist = map[string]*spark.Ring{}
	}
	r := m.tempHist[name]
	if r == nil {
		r = spark.NewRing(historyCap)
		m.tempHist[name] = r
	}
	return r
}

// sampleTemps records every sensor's current temperature.
func (m *model) sampleTemps() {
	if !m.ready || !m.connected() {
		return
	}
	for name, s := range m.sensors {
		m.ring(name).Add(s.Current)
	}
	if contains(m.tempCols, "history") {
		m.rebuildRows()
	}
}

// backfillTemps puts Moonraker's stored history before what's been
// sampled since connecting. (No store — an old Moonraker, or it's
// disabled — just means the line starts empty.)
func (m *model) backfillTemps(msg tempStoreMsg) {
	if msg.err != nil {
		return
	}
	for name, h := range msg.store {
		if _, ok := m.sensors[name]; !ok || len(h.Temperatures) == 0 {
			continue
		}
		r := spark.NewRing(historyCap)
		r.Add(h.Temperatures...)
		r.Add(m.ring(name).Samples()...)
		m.tempHist[name] = r
	}
	m.rebuildRows()
}

// sparkline renders one row's history: scaled from the window's low to
// its high or the target, whichever is higher, at least 5 °C tall so a
// steady sensor sits flat on the bottom.
func (m *model) sparkline(name string, target float64) string {
	r := m.tempHist[name]
	if r == nil || m.historyW <= 0 {
		return ""
	}
	samples := r.Samples()
	lo, hi, ok := spark.Range(samples, historyWindow)
	if !ok {
		return ""
	}
	hi = max(hi, target)
	return spark.Line(samples, historyWindow, m.historyW, lo, hi, 5)
}
