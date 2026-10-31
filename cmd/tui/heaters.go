package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
)

// ---------------------------------------------------------------------------
// Phase 15: heater control
//
// enter on a Temperatures row edits its target in place: the Target
// cell becomes a small input, enter sends, esc cancels. The table keeps
// showing the printer's confirmed target everywhere else, and the edited
// row snaps back to confirmed state as soon as the edit ends — the new
// value appears only when Klipper reports it.
// ---------------------------------------------------------------------------

// heaterCommand returns the gcode that sets object's target, the same
// way Mainsail builds it: SET_HEATER_TEMPERATURE for extruders, the bed
// and heater_generic; SET_TEMPERATURE_FAN_TARGET for temperature fans.
// The HEATER / TEMPERATURE_FAN value is the object's last word
// ("heater_generic chamber" → chamber). ok is false for sensors.
func heaterCommand(object string, target float64) (cmd string, ok bool) {
	parts := strings.Fields(object)
	if len(parts) == 0 {
		return "", false
	}
	name := parts[len(parts)-1]
	t := strconv.FormatFloat(target, 'f', -1, 64)
	switch {
	case extruderHeater.MatchString(parts[0]), parts[0] == "heater_bed", parts[0] == "heater_generic":
		return fmt.Sprintf("SET_HEATER_TEMPERATURE HEATER=%s TARGET=%s", name, t), true
	case parts[0] == "temperature_fan":
		return fmt.Sprintf("SET_TEMPERATURE_FAN_TARGET TEMPERATURE_FAN=%s TARGET=%s", name, t), true
	}
	return "", false
}

func canSetTarget(object string) bool {
	_, ok := heaterCommand(object, 0)
	return ok
}

// selectedSensor returns the object name under the table cursor.
func (m *model) selectedSensor() (string, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.shownSensors) {
		return "", false
	}
	return m.shownSensors[i], true
}

func newTempInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 6
	return ti
}

// startTempEdit begins editing the selected row's target.
func (m *model) startTempEdit() {
	name, ok := m.selectedSensor()
	if !ok {
		return
	}
	if !canSetTarget(name) {
		m.appendLog(fmt.Sprintf("!! %s is a sensor; it has no target", m.label(name)))
		return
	}
	m.tempEdit = name
	m.tempInput.SetValue("")
	if s := m.sensors[name]; s != nil && s.Target > 0 {
		m.tempInput.SetValue(strconv.FormatFloat(s.Target, 'f', -1, 64))
	}
	m.tempInput.CursorEnd()
	m.tempInput.Focus()
	m.rebuildRows()
}

func (m *model) endTempEdit() {
	m.tempEdit = ""
	m.tempInput.Blur()
	m.rebuildRows()
}

// handleTempEditKey owns every key while a target is being edited
// (E-stop is handled before this is reached).
func (m *model) handleTempEditKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.endTempEdit()
		return nil
	case "enter":
		obj := m.tempEdit
		raw := strings.TrimSpace(m.tempInput.Value())
		m.endTempEdit()
		if raw == "" {
			return nil
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 {
			m.appendLog(fmt.Sprintf("!! %q is not a temperature", raw))
			return nil
		}
		return m.setTarget(obj, v)
	case "tab", "shift+tab":
		m.endTempEdit()
		if msg.String() == "tab" {
			m.cycleFocus(1)
		} else {
			m.cycleFocus(-1)
		}
		return nil
	}
	// A temperature is digits and a point; drop anything else rather
	// than let it reach the input (or act as a command).
	if isTextKey(msg) && strings.Trim(string(msg.Runes), "0123456789.") != "" {
		return nil
	}
	var cmd tea.Cmd
	m.tempInput, cmd = m.tempInput.Update(msg)
	m.rebuildRows()
	return cmd
}

// setTarget sends one heater's target. Above-max targets are left to
// Klipper to reject (its error lands in the console), same as jog
// bounds — the firmware knows the limits, Moontop doesn't duplicate them.
func (m *model) setTarget(object string, target float64) tea.Cmd {
	cmd, ok := heaterCommand(object, target)
	if !ok {
		return nil
	}
	m.appendLog(">>> " + cmd)
	return m.requireConnected("set temperature", m.sendGcode(cmd))
}

// handleTempsKey is the Temperatures pane when not editing.
func (m *model) handleTempsKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, *keys.TempSet):
		m.startTempEdit()
		return nil
	case key.Matches(msg, *keys.TempOff):
		name, ok := m.selectedSensor()
		if !ok || !canSetTarget(name) {
			return nil
		}
		if jobActive(m.job.State) {
			cmd, _ := heaterCommand(name, 0)
			m.confirmation = &pendingConfirmation{
				prompt: fmt.Sprintf("Turn off %s during the print?", m.label(name)),
				onYes:  m.sendGcode(cmd),
				danger: true,
			}
			return nil
		}
		return m.setTarget(name, 0)
	case key.Matches(msg, *keys.TempAllOff):
		return m.cooldown()
	case key.Matches(msg, *keys.TempPreset):
		m.openPresetPicker()
		return nil
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return cmd
}

// cooldown runs Mainsail's cooldown gcode (TURN_OFF_HEATERS unless the
// printer's Mainsail settings say otherwise). Confirms during a print.
func (m *model) cooldown() tea.Cmd {
	g := m.cooldownGcode
	if g == "" {
		g = "TURN_OFF_HEATERS"
	}
	if jobActive(m.job.State) {
		m.confirmation = &pendingConfirmation{
			prompt: "Turn off all heaters during the print?",
			onYes:  m.sendGcode(g),
			danger: true,
		}
		return nil
	}
	m.appendLog(">>> " + g)
	return m.requireConnected("cooldown", m.sendGcode(g))
}

// ---------------------------------------------------------------------------
// Presets — read from Mainsail's settings in Moonraker's database, so a
// preset made in Mainsail shows up here. Moontop's own presets come with
// the UI-2 config.
// ---------------------------------------------------------------------------

type preset struct {
	Name   string
	Gcode  string
	Values []presetValue // only enabled values, sorted by object
	Source string        // "Moontop" or "Mainsail"
}

type presetValue struct {
	Object string
	Target float64
}

type presetsMsg struct {
	presets  []preset
	cooldown string
	err      error
}

// mainsailPresets mirrors Mainsail's GuiPresetsState as stored under
// namespace "mainsail", key "presets".
type mainsailPresets struct {
	Presets map[string]struct {
		Name   string `json:"name"`
		Gcode  string `json:"gcode"`
		Values map[string]struct {
			Bool  bool    `json:"bool"`
			Value float64 `json:"value"`
		} `json:"values"`
	} `json:"presets"`
	CooldownGcode string `json:"cooldownGcode"`
}

func parseMainsailPresets(raw json.RawMessage) ([]preset, string, error) {
	var mp mainsailPresets
	if err := json.Unmarshal(raw, &mp); err != nil {
		return nil, "", err
	}
	var out []preset
	for _, p := range mp.Presets {
		pr := preset{Name: p.Name, Gcode: strings.TrimSpace(p.Gcode)}
		for obj, v := range p.Values {
			if v.Bool {
				pr.Values = append(pr.Values, presetValue{Object: obj, Target: v.Value})
			}
		}
		sort.Slice(pr.Values, func(i, j int) bool { return pr.Values[i].Object < pr.Values[j].Object })
		out = append(out, pr)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, strings.TrimSpace(mp.CooldownGcode), nil
}

func loadPresetsCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		raw, err := client.DatabaseGetItem("mainsail", "presets")
		if err != nil {
			// No Mainsail namespace / no presets is normal.
			return presetsMsg{err: err}
		}
		ps, cd, err := parseMainsailPresets(raw)
		return presetsMsg{presets: ps, cooldown: cd, err: err}
	}
}

// presetScript builds one script for a preset: a target command per
// enabled heater that exists on this printer, then the preset's gcode.
// Heaters the printer doesn't have are skipped rather than letting
// Klipper abort the whole script on the first unknown one.
func (m *model) presetScript(p preset) (script string, skipped []string) {
	var lines []string
	for _, v := range p.Values {
		if _, ok := m.sensors[v.Object]; !ok {
			skipped = append(skipped, v.Object)
			continue
		}
		if cmd, ok := heaterCommand(v.Object, v.Target); ok {
			lines = append(lines, cmd)
		}
	}
	if p.Gcode != "" {
		lines = append(lines, p.Gcode)
	}
	return strings.Join(lines, "\n"), skipped
}

// presetSummary is "extruder 210 · bed 60" for the picker.
func presetSummary(p preset) string {
	var parts []string
	for _, v := range p.Values {
		parts = append(parts, fmt.Sprintf("%s %g", strings.ToLower(friendlyName(v.Object)), v.Target))
	}
	return strings.Join(parts, " · ")
}

// openPresetPicker shows the presets as numbered options on the shared
// prompt line.
func (m *model) openPresetPicker() {
	if len(m.presets) == 0 {
		m.appendLog("!! no preheat presets: save one in setup (F2 → Presets) or create them in Mainsail")
		return
	}
	var opts []promptOption
	for i, p := range m.presets {
		if i == 9 {
			break
		}
		p := p
		label := p.Name
		if s := presetSummary(p); s != "" {
			label += " (" + s + ")"
		}
		if p.Source == "Mainsail" && len(m.printerCfg.Presets) > 0 {
			label += " · Mainsail"
		}
		opts = append(opts, promptOption{
			key:   strconv.Itoa(i + 1),
			label: label,
			run: func(m *model) tea.Cmd {
				script, skipped := m.presetScript(p)
				if len(skipped) > 0 {
					m.appendLog(fmt.Sprintf("!! preset %q: this printer has no %s; skipped", p.Name, strings.Join(skipped, ", ")))
				}
				if script == "" {
					return nil
				}
				m.appendLog(">>> " + strings.ReplaceAll(script, "\n", "\n>>> "))
				return m.requireConnected("preheat", m.sendGcode(script))
			},
		})
	}
	m.confirmation = &pendingConfirmation{prompt: "Preheat:", options: opts}
}
