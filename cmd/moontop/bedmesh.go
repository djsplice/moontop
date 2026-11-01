package main

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Phase 24: bed mesh (B or F11), screws tilt, manual probe
//
// The mesh is a grid of numbers, the way Marlin prints G29's: back of the
// bed on top, column numbers across, row numbers down, the range above.
// Profiles load, save (under a name) and remove; c calibrates, C clears,
// w runs SAVE_CONFIG so saved profiles survive a restart. t runs
// SCREWS_TILT_CALCULATE when the printer has screws_tilt_adjust and
// shows each screw's turn.
//
// Manual probe: whenever Klipper waits on a paper test (PROBE_CALIBRATE,
// Z_ENDSTOP_CALIBRATE, MANUAL_PROBE…), a dialog takes the screen: ↑↓
// TESTZ by the step, ←→ change the step, enter ACCEPT, esc ABORT.
// ---------------------------------------------------------------------------

type meshScreen struct {
	open   bool
	cursor int
}

func (m *model) meshProfiles() []string {
	profiles, _ := dig(m.raw["bed_mesh"], "profiles").(map[string]any)
	var names []string
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// meshMatrix is the probed matrix (rows from the front, y min, as
// Klipper stores it).
func (m *model) meshMatrix() [][]float64 {
	rows, _ := dig(m.raw["bed_mesh"], "probed_matrix").([]any)
	var out [][]float64
	for _, r := range rows {
		if f, ok := floats(r); ok && len(f) > 0 {
			out = append(out, f)
		}
	}
	return out
}

func hasBedMesh(m *model) (bool, string) {
	_, ok := m.raw["bed_mesh"]
	return ok, "on printers with [bed_mesh]"
}

// renderMeshGrid draws the matrix Marlin-style — back of the bed on
// top, column and row numbers — with each value in a box coloured by
// height: green near zero, through cyan to blue below, yellow to red
// above, scaled to the largest deviation (at least 0.05 mm, so a flat
// bed stays green). When the boxes don't fit in width × height, the
// lines between rows go first, then digits.
func renderMeshGrid(mat [][]float64, width, height int) []string {
	if len(mat) == 0 {
		return []string{consoleInfoStyle.Render("No mesh loaded: c calibrates one, or load a profile.")}
	}
	rows, cols := len(mat), len(mat[0])
	prec, cellW := 3, 8 // " +0.123 "
	if 5+cols*(cellW+1)+1 > width {
		prec, cellW = 2, 7 // " +0.12 "
	}
	rowLines := rows > 0 && 2*rows+2 <= height // lines between rows

	maxDev := 0.05
	for _, r := range mat {
		for _, v := range r {
			maxDev = math.Max(maxDev, math.Abs(v))
		}
	}

	seg := strings.Repeat("─", cellW)
	rule := func(l, mid, r string) string {
		parts := make([]string, cols)
		for i := range parts {
			parts[i] = seg
		}
		return consoleInfoStyle.Render("     " + l + strings.Join(parts, mid) + r)
	}
	head := "     "
	for c := 0; c < cols; c++ {
		head += fmt.Sprintf(" %*d", cellW, c)
	}
	lines := []string{consoleInfoStyle.Render(head), rule("┌", "┬", "┐")}
	bar := consoleInfoStyle.Render("│")
	for r := rows - 1; r >= 0; r-- { // back of the bed on top
		line := consoleInfoStyle.Render(fmt.Sprintf("%4d ", r)) + bar
		for _, v := range mat[r] {
			line += meshCellStyle(v/maxDev).Render(fmt.Sprintf(" %+.*f ", prec, v)) + bar
		}
		lines = append(lines, line)
		if rowLines && r > 0 {
			lines = append(lines, rule("├", "┼", "┤"))
		}
	}
	return append(lines, rule("└", "┴", "┘"))
}

// meshCellStyle colours a cell by t, its height relative to the largest
// deviation (-1 … 1).
func meshCellStyle(t float64) lipgloss.Style {
	below := []string{"46", "50", "45", "39", "33", "27"}      // green → cyan → blue
	above := []string{"46", "154", "226", "220", "208", "196"} // green → yellow → red
	k := int(math.Round(math.Min(1, math.Abs(t)) * 5))
	bg, fg := above[k], "16"
	if t < 0 {
		bg = below[k]
		if k >= 4 {
			fg = "231" // white on the dark blues
		}
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(fg))
}

func meshRange(mat [][]float64) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, r := range mat {
		for _, v := range r {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	return
}

var profileName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

func (m *model) handleMeshKey(msg tea.KeyMsg) tea.Cmd {
	s := &m.mesh
	profiles := m.meshProfiles()
	printing := jobActive(m.job.State)
	switch msg.String() {
	case "esc", "q":
		s.open = false
		return nil
	case "up", "k":
		if len(profiles) > 0 {
			s.cursor = (s.cursor + len(profiles) - 1) % len(profiles)
		}
		return nil
	case "down", "j":
		if len(profiles) > 0 {
			s.cursor = (s.cursor + 1) % len(profiles)
		}
		return nil
	case "c":
		if printing {
			return m.refuse("calibrating waits until the print is done")
		}
		if h, ok := m.raw["toolhead"]["homed_axes"].(string); ok && !(strings.Contains(h, "x") && strings.Contains(h, "y") && strings.Contains(h, "z")) {
			return m.refuse("home all axes first (H in Toolhead)")
		}
		m.confirmation = &pendingConfirmation{prompt: "Probe a new bed mesh (BED_MESH_CALIBRATE)?", onYes: m.sendEchoedCmd("BED_MESH_CALIBRATE")}
		return nil
	case "C":
		return m.sendEchoed("clear mesh", "BED_MESH_CLEAR")
	case "s":
		if printing {
			return m.refuse("saving waits until the print is done")
		}
		cur, _ := dig(m.raw["bed_mesh"], "profile_name").(string)
		m.openLineEdit("Save the current mesh as profile:", cur, func(m *model, v string) (tea.Cmd, error) {
			v = strings.TrimSpace(v)
			if !profileName.MatchString(v) {
				return nil, fmt.Errorf("letters, digits, _ and - only")
			}
			m.appendLog("** saved profiles last until a restart; w runs SAVE_CONFIG to keep them")
			return m.sendEchoed("save mesh", "BED_MESH_PROFILE SAVE="+v), nil
		})
		return nil
	case "w":
		if printing {
			return m.refuse("SAVE_CONFIG restarts Klipper; wait until the print is done")
		}
		m.confirmation = &pendingConfirmation{prompt: "SAVE_CONFIG? Klipper writes printer.cfg and restarts.", onYes: m.sendEchoedCmd("SAVE_CONFIG")}
		return nil
	case "t":
		if _, ok := m.raw["screws_tilt_adjust"]; !ok {
			return m.refuse("this printer has no [screws_tilt_adjust]")
		}
		if printing {
			return m.refuse("screws tilt waits until the print is done")
		}
		return m.sendEchoed("screws tilt", "SCREWS_TILT_CALCULATE")
	}
	if key.Matches(msg, *keys.BedMesh) {
		s.open = false
		return nil
	}
	if len(profiles) == 0 {
		return nil
	}
	p := profiles[min(s.cursor, len(profiles)-1)]
	switch msg.String() {
	case "enter":
		return m.sendEchoed("load mesh", "BED_MESH_PROFILE LOAD="+p)
	case "x":
		m.confirmation = &pendingConfirmation{prompt: "Remove mesh profile " + p + "? (w then saves the removal)",
			onYes: m.sendEchoedCmd("BED_MESH_PROFILE REMOVE=" + p)}
	}
	return nil
}

// screwLines shows SCREWS_TILT_CALCULATE's results, when reported.
func (m *model) screwLines() []string {
	st := m.raw["screws_tilt_adjust"]
	if st == nil {
		return nil
	}
	if st["error"] == true {
		return []string{consoleErrStyle.Render("Screws tilt: the last run failed; see the console.")}
	}
	results, _ := st["results"].(map[string]any)
	if len(results) == 0 {
		return []string{consoleInfoStyle.Render("Screws tilt: t runs SCREWS_TILT_CALCULATE.")}
	}
	var names []string
	for n := range results {
		names = append(names, n)
	}
	sort.Strings(names)
	lines := []string{helpGroupStyle.Render("Screws tilt") + consoleInfoStyle.Render("  (turns as hh:mm, CW / CCW)")}
	for _, n := range names {
		r := results[n]
		label := digS(r, "name")
		if label == "" {
			label = n
		}
		z, _ := digF(r, "z")
		turn := digS(r, "adjust")
		dir := map[string]string{"CW": "clockwise", "CCW": "counter-clockwise"}[digS(r, "sign")]
		state := fmt.Sprintf("%s %s", turn, dir)
		if dig(r, "is_base") == true {
			state = "base"
		}
		lines = append(lines, fmt.Sprintf("  %-20s z %+7.4f   %s", label, z, state))
	}
	return lines
}

func (m model) renderMesh() string {
	title := appTitleStyle.Render(appTitle + "  ·  Bed mesh")
	w := m.width - 4
	mat := m.meshMatrix()
	var lines []string
	cur, _ := dig(m.raw["bed_mesh"], "profile_name").(string)
	if len(mat) > 0 {
		lo, hi := meshRange(mat)
		info := fmt.Sprintf("Profile %s  ·  range %+.3f to %+.3f (%.3f mm)", cur, lo, hi, hi-lo)
		if mn, ok := floats(dig(m.raw["bed_mesh"], "mesh_min")); ok && len(mn) >= 2 {
			if mx, ok := floats(dig(m.raw["bed_mesh"], "mesh_max")); ok && len(mx) >= 2 {
				info += fmt.Sprintf("  ·  X %.0f–%.0f  Y %.0f–%.0f", mn[0], mx[0], mn[1], mx[1])
			}
		}
		lines = append(lines, consoleInfoStyle.Render(info), "")
	}
	lines = append(lines, renderMeshGrid(mat, w, m.height-12)...)
	lines = append(lines, consoleInfoStyle.Render("     (back of the bed on top)"), "")

	profiles := m.meshProfiles()
	lines = append(lines, helpGroupStyle.Render("Profiles"))
	for i, p := range profiles {
		mark, ns := "  ", footerText
		if i == m.mesh.cursor {
			mark, ns = setupCursorStyle.Render("▸ "), jogStepStyle
		}
		active := ""
		if p == cur {
			active = consoleNoticeStyle.Render("  loaded")
		}
		lines = append(lines, mark+ns.Render(p)+active)
	}
	if len(profiles) == 0 {
		lines = append(lines, consoleInfoStyle.Render("  none saved"))
	}
	if sl := m.screwLines(); len(sl) > 0 {
		lines = append(append(lines, ""), sl...)
	}
	for i, l := range lines {
		lines[i] = clipWidth(l, w)
	}
	out := title + "\n\n" + bodyStyle.Render(strings.Join(lines, "\n")) + "\n\n"
	switch {
	case m.lineEdit != nil:
		out += renderLineEdit(m.lineEdit, m.width) + "\n"
	case m.confirmation != nil:
		out += renderPrompt(m.confirmation, m.width) + "\n"
	}
	foot := "↑↓ profile  •  enter load  •  s save as  •  x remove  •  w SAVE_CONFIG  •  c calibrate  •  C clear"
	if _, ok := m.raw["screws_tilt_adjust"]; ok {
		foot += "  •  t screws tilt"
	}
	return out + clipWidth(footerStyle.Render(foot+"  •  esc close"), m.width) + "\n"
}

// --- manual probe --------------------------------------------------------------

var probeSteps = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.5, 1}

func (m *model) manualProbeActive() bool {
	return m.raw["manual_probe"]["is_active"] == true
}

func (m *model) handleProbeKey(msg tea.KeyMsg) tea.Cmd {
	step := probeSteps[m.probeStep]
	switch msg.String() {
	case "up", "k", "+":
		return m.sendEchoed("TESTZ", "TESTZ Z="+trimFloat(step))
	case "down", "j", "-":
		return m.sendEchoed("TESTZ", "TESTZ Z=-"+trimFloat(step))
	case "left", "h":
		m.probeStep = max(0, m.probeStep-1)
	case "right", "l":
		m.probeStep = min(len(probeSteps)-1, m.probeStep+1)
	case "enter":
		return m.sendEchoed("ACCEPT", "ACCEPT")
	case "esc":
		return m.sendEchoed("ABORT", "ABORT")
	}
	return nil
}

func (m model) renderProbe() string {
	mp := m.raw["manual_probe"]
	z, _ := digF(mp, "z_position")
	lo, okLo := digF(mp, "z_position_lower")
	hi, okHi := digF(mp, "z_position_upper")
	lines := []string{
		"Klipper is waiting for a paper test: lower the nozzle until the paper",
		"drags slightly, then accept.",
		"",
		fmt.Sprintf("Z   %s", jogPosStyle.Render(fmt.Sprintf("%.3f", z))),
	}
	if okLo || okHi {
		b := "    between "
		if okLo {
			b += fmt.Sprintf("%.3f", lo)
		} else {
			b += "?"
		}
		b += " and "
		if okHi {
			b += fmt.Sprintf("%.3f", hi)
		} else {
			b += "?"
		}
		lines = append(lines, consoleInfoStyle.Render(b+" (tested)"))
	}
	var steps []string
	for i, s := range probeSteps {
		t := trimFloat(s)
		if i == m.probeStep {
			t = jogStepStyle.Render("[" + t + "]")
		} else {
			t = consoleInfoStyle.Render(t)
		}
		steps = append(steps, t)
	}
	lines = append(lines, "", "Step "+strings.Join(steps, " "))
	box := renderPanel("Manual probe", strings.Join(lines, "\n"), true, min(76, m.width-4), 0)
	title := appTitleStyle.Render(appTitle + "  ·  Manual probe")
	foot := footerStyle.Render("↑↓ raise / lower by the step  •  ←→ step  •  enter ACCEPT  •  esc ABORT  •  ctrl+x e-stop")
	return title + "\n\n" + box + "\n\n" + clipWidth(foot, m.width) + "\n"
}
