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
// Phase 17: the Outputs pane (pane 8) — fans, output pins, LEDs
//
// Rows are built from what each object is, not from a fixed list:
//
//   fan                          part fan, M106 / M107
//   fan_generic <n>              SET_FAN_SPEED
//   heater_fan, controller_fan   read-only (Klipper drives them)
//   output_pin <n>               SET_PIN: a percentage when PWM, on/off
//                                when digital (from configfile pwm and
//                                scale, read once at startup)
//   led, neopixel, dotstar,      SET_LED: ←→ brightness, o colour
//   pca9533, pca9632 <n>
//
// Objects whose name starts with "_" are hidden, as in Mainsail. All of
// these may be changed during a print.
// ---------------------------------------------------------------------------

type outputKind int

const (
	outPartFan outputKind = iota
	outGenericFan
	outAutoFan
	outPin
	outLED
)

// output is one controllable (or watched) object.
type output struct {
	object string // Klipper object name, "fan_generic aux"
	name   string // section name, "aux"
	kind   outputKind
	pwm    bool    // output_pin: a PWM pin (else digital on/off)
	scale  float64 // output_pin: VALUE range is 0..scale
	white  bool    // LED has a white channel
}

var ledPrefixes = []string{"led", "neopixel", "dotstar", "pca9533", "pca9632"}

// classifyOutput reports whether object is an output and what kind.
func classifyOutput(object string) (output, bool) {
	typ, name, _ := strings.Cut(object, " ")
	if strings.HasPrefix(name, "_") {
		return output{}, false
	}
	o := output{object: object, name: name, scale: 1}
	switch {
	case object == "fan":
		o.kind, o.name = outPartFan, "fan"
	case typ == "fan_generic" && name != "":
		o.kind = outGenericFan
	case (typ == "heater_fan" || typ == "controller_fan") && name != "":
		o.kind = outAutoFan
	case typ == "output_pin" && name != "":
		o.kind = outPin
	case name != "" && contains(ledPrefixes, typ):
		o.kind = outLED
	default:
		return output{}, false
	}
	return o, true
}

func isOutputObject(object string) bool {
	_, ok := classifyOutput(object)
	return ok
}

// outputFields subscribes to.
func outputSubscription(o output) []string {
	switch o.kind {
	case outPin:
		return []string{"value"}
	case outLED:
		return []string{"color_data"}
	}
	return []string{"speed", "rpm"}
}

// discoverOutputs lists the printer's outputs in display order (part
// fan, fans, pins, LEDs; by name within each), with pin and LED details
// from configfile settings (nil if they couldn't be read).
func discoverOutputs(objects []string, settings map[string]any) []output {
	var outs []output
	for _, obj := range objects {
		o, ok := classifyOutput(obj)
		if !ok {
			continue
		}
		sec, _ := settings[strings.ToLower(obj)].(map[string]any)
		switch o.kind {
		case outPin:
			o.pwm, _ = sec["pwm"].(bool)
			if s, ok := sec["scale"].(float64); ok && s > 0 {
				o.scale = s
			}
		case outLED:
			order := fmt.Sprint(sec["color_order"]) // "GRBW" or ["GRBW"]
			o.white = strings.Contains(strings.ToUpper(order), "W") || strings.HasPrefix(obj, "pca")
		}
		outs = append(outs, o)
	}
	sort.SliceStable(outs, func(i, j int) bool {
		if outs[i].kind != outs[j].kind {
			return outs[i].kind < outs[j].kind
		}
		return outs[i].name < outs[j].name
	})
	return outs
}

// outputLabel is the row label: "Part fan", "aux", "caselight".
func outputLabel(o output) string {
	if o.kind == outPartFan {
		return "Part fan"
	}
	return strings.ReplaceAll(o.name, "_", " ")
}

func (m *model) outputValue(o output, field string) (float64, bool) {
	v, ok := m.raw[o.object][field].(float64)
	return v, ok
}

// ledColor is the first LED's [r g b w] (0..1), as Mainsail shows it.
func (m *model) ledColor(o output) ([4]float64, bool) {
	var c [4]float64
	list, ok := m.raw[o.object]["color_data"].([]any)
	if !ok || len(list) == 0 {
		return c, false
	}
	ch, ok := floats(list[0])
	if !ok {
		return c, false
	}
	copy(c[:], ch)
	return c, true
}

func ledHex(c [4]float64) string {
	b := func(f float64) int { return int(math.Round(math.Max(0, math.Min(1, f)) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", b(c[0]), b(c[1]), b(c[2]))
}

func readOnly(m *model) (bool, string) { return false, "set by Klipper (heater or controller fan)" }

// setLED sends a colour; channels 0..1.
func (m *model) setLED(o output, c [4]float64) tea.Cmd {
	f := func(v float64) string { return trimFloat(roundTo(math.Max(0, math.Min(1, v)), 3)) }
	g := fmt.Sprintf("SET_LED LED=%s RED=%s GREEN=%s BLUE=%s", o.name, f(c[0]), f(c[1]), f(c[2]))
	if o.white {
		g += " WHITE=" + f(c[3])
	}
	return m.sendEchoed("set LED", g)
}

// outputRow builds one output's field row.
func outputRow(o output) numField {
	r := numField{label: outputLabel(o), unit: "%", prec: 0, step: 5, min: 0, max: 100}
	speed := func(m *model) (float64, bool) {
		v, ok := m.outputValue(o, "speed")
		return math.Round(v * 100), ok
	}
	switch o.kind {
	case outPartFan:
		r.value = speed
		r.set = func(m *model, v float64) tea.Cmd {
			if v == 0 {
				return m.sendEchoed("part fan", "M107")
			}
			return m.sendEchoed("part fan", fmt.Sprintf("M106 S%d", int(math.Round(v*255/100))))
		}
	case outGenericFan:
		r.value = speed
		r.set = func(m *model, v float64) tea.Cmd {
			return m.sendEchoed("fan", fmt.Sprintf("SET_FAN_SPEED FAN=%s SPEED=%s", o.name, trimFloat(v/100)))
		}
	case outAutoFan:
		r.value = speed
		r.when = readOnly
		r.showM = func(m *model, v float64) string {
			s := fmt.Sprintf("%.0f %%", v)
			if rpm, ok := m.outputValue(o, "rpm"); ok {
				s = fmt.Sprintf("%.0f%% %.0frpm", v, rpm)
			}
			return s
		}
	case outPin:
		val := func(m *model) (float64, bool) { return m.outputValue(o, "value") }
		switch {
		case !o.pwm:
			r.unit, r.step, r.max = "", 0, 1
			r.choices = []float64{0, 1}
			r.value = func(m *model) (float64, bool) {
				v, ok := val(m)
				return math.Round(v), ok
			}
			r.show = func(v float64) string { return map[bool]string{true: "on", false: "off"}[v >= 0.5] }
			r.set = func(m *model, v float64) tea.Cmd {
				if v != 0 && v != 1 {
					return m.refuse(outputLabel(o) + " is on/off: type 1 or 0")
				}
				return m.sendEchoed("set pin", fmt.Sprintf("SET_PIN PIN=%s VALUE=%s", o.name, trimFloat(v)))
			}
		case o.scale == 1:
			r.value = func(m *model) (float64, bool) {
				v, ok := val(m)
				return math.Round(v * 100), ok
			}
			r.set = func(m *model, v float64) tea.Cmd {
				return m.sendEchoed("set pin", fmt.Sprintf("SET_PIN PIN=%s VALUE=%s", o.name, trimFloat(v/100)))
			}
		default: // a scaled pin: show and set in its own units
			r.unit, r.prec, r.max, r.step = "", 2, o.scale, o.scale/20
			r.value = func(m *model) (float64, bool) {
				v, ok := val(m)
				return v * o.scale, ok
			}
			r.set = func(m *model, v float64) tea.Cmd {
				return m.sendEchoed("set pin", fmt.Sprintf("SET_PIN PIN=%s VALUE=%s", o.name, trimFloat(v)))
			}
		}
	case outLED:
		// The row's number is brightness: the brightest channel. ←→
		// and typed values scale the current colour; from off, white.
		r.step = 10
		r.value = func(m *model) (float64, bool) {
			c, ok := m.ledColor(o)
			return math.Round(math.Max(math.Max(c[0], c[1]), math.Max(c[2], c[3])) * 100), ok
		}
		r.showM = func(m *model, v float64) string {
			c, _ := m.ledColor(o)
			sw := lipgloss.NewStyle().Foreground(lipgloss.Color(ledHex(c))).Render("██")
			if v == 0 {
				sw = jogHintStyle.Render("··")
			}
			return fmt.Sprintf("%s %.0f %%", sw, v)
		}
		r.set = func(m *model, v float64) tea.Cmd {
			c, _ := m.ledColor(o)
			peak := math.Max(math.Max(c[0], c[1]), math.Max(c[2], c[3]))
			if peak == 0 {
				c = [4]float64{1, 1, 1, 0}
				if o.white {
					c = [4]float64{0, 0, 0, 1}
				}
				peak = 1
			}
			k := v / 100 / peak
			for i := range c {
				c[i] *= k
			}
			return m.setLED(o, c)
		}
	}
	return r
}

func outputFields(m *model) []numField {
	rows := make([]numField, len(m.outputs))
	for i, o := range m.outputs {
		rows[i] = outputRow(o)
	}
	return rows
}

var hexColor = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)

func (m *model) handleOutputsKey(msg tea.KeyMsg) tea.Cmd {
	rows := outputFields(m)
	if cmd, handled := m.handleFieldKey(&m.outFields, rows, msg); handled {
		return cmd
	}
	if len(rows) == 0 {
		return nil
	}
	o, row := m.outputs[m.outFields.cursor], rows[m.outFields.cursor]
	switch {
	case key.Matches(msg, *keys.OutputOff):
		if ok, why := row.available(m); !ok {
			return m.refuse(row.label + ": " + why)
		}
		return row.set(m, 0)
	case key.Matches(msg, *keys.LEDColor):
		if o.kind != outLED {
			return m.refuse(row.label + " isn't an LED")
		}
		c, _ := m.ledColor(o)
		m.openLineEdit(fmt.Sprintf("%s colour (hex, e.g. FF8800):", row.label), strings.TrimPrefix(ledHex(c), "#"), func(m *model, v string) (tea.Cmd, error) {
			v = strings.TrimSpace(v)
			if !hexColor.MatchString(v) {
				return nil, fmt.Errorf("enter a six-digit hex colour such as FF8800")
			}
			var r, g, b int
			fmt.Sscanf(strings.TrimPrefix(v, "#"), "%02x%02x%02x", &r, &g, &b)
			return m.setLED(o, [4]float64{float64(r) / 255, float64(g) / 255, float64(b) / 255, 0}), nil
		})
	}
	return nil
}

// outputsContentLines: one row per output, a blank line and a hint.
func outputsContentLines(m *model) int { return len(m.outputs) + 2 }

func (m model) renderOutputs() string {
	w := m.layout.w(focusOutputs) - panelInnerXReserve()
	rows := outputFields(&m)
	// Content lines the box holds (minus borders and title). When the
	// rows and hint don't all fit, drop the hint and scroll the rows so
	// the cursor stays in view.
	avail := len(rows) + 2
	if h := m.layout.h(focusOutputs); h > 0 {
		avail = max(1, h-3)
	}
	if avail < len(rows)+2 {
		return strings.Join(m.renderFieldsWindow(&m.outFields, rows, m.focus == focusOutputs, w, avail), "\n")
	}
	lines := m.renderFields(&m.outFields, rows, m.focus == focusOutputs, w)
	hint := "←→ adjust  x off  o colour"
	if w < 26 {
		hint = "←→ adj  x off  o col"
	}
	lines = append(lines, "", jogHintStyle.Render(clipWidth(hint, w)))
	return strings.Join(lines, "\n")
}
