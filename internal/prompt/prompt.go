// Package prompt follows Klipper macro prompts: the "// action:prompt_*"
// lines a macro sends with RESPOND, the protocol Mainsail and Fluidd
// draw as dialogs.
//
//	action:prompt_begin <title>          start a new prompt
//	action:prompt_text <text>            a line of text
//	action:prompt_button <label>|<gcode>|<colour>
//	action:prompt_button_group_start / _end   buttons on one row
//	action:prompt_footer_button <label>|<gcode>|<colour>
//	action:prompt_show                   show it
//	action:prompt_end                    close it
//
// gcode defaults to the label; colour (primary, secondary, info,
// warning, error) is optional.
package prompt

import "strings"

// Button is one prompt button.
type Button struct {
	Label, Gcode, Color string
	Row                 int // buttons in a group share a row
}

// Prompt is the dialog a macro has built.
type Prompt struct {
	Title   string
	Text    []string
	Buttons []Button
	Footer  []Button
}

// State tracks the prompt being built and the one on screen.
type State struct {
	building *Prompt
	inGroup  bool
	rows     int
	Shown    *Prompt // nil when no prompt is showing
}

const prefix = "action:prompt_"

// IsPromptLine reports whether a console line is prompt protocol.
func IsPromptLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "//")), prefix)
}

// Close is the gcode that closes a prompt from the client side (what
// Mainsail's close button sends).
const Close = "RESPOND TYPE=command MSG=action:prompt_end"

// Feed processes one console line and reports whether the shown prompt
// changed (opened, closed or replaced).
func (s *State) Feed(line string) bool {
	l := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "//"))
	rest, ok := strings.CutPrefix(l, prefix)
	if !ok {
		return false
	}
	cmd, arg, _ := strings.Cut(rest, " ")
	arg = strings.TrimSpace(arg)
	switch cmd {
	case "begin":
		s.building, s.inGroup, s.rows = &Prompt{Title: arg}, false, 0
		// A new begin replaces whatever was showing.
		if s.Shown != nil {
			s.Shown = nil
			return true
		}
	case "text":
		if s.building != nil {
			s.building.Text = append(s.building.Text, arg)
		}
	case "button", "footer_button":
		if s.building == nil {
			return false
		}
		b := parseButton(arg)
		if cmd == "footer_button" {
			s.building.Footer = append(s.building.Footer, b)
			return false
		}
		b.Row = s.rows
		if !s.inGroup {
			s.rows++
		}
		s.building.Buttons = append(s.building.Buttons, b)
	case "button_group_start":
		s.inGroup = true
	case "button_group_end":
		if s.inGroup {
			s.inGroup = false
			s.rows++
		}
	case "show":
		if s.building != nil {
			s.Shown = s.building
			return true
		}
	case "end":
		s.building = nil
		if s.Shown != nil {
			s.Shown = nil
			return true
		}
	}
	return false
}

func parseButton(arg string) Button {
	parts := strings.SplitN(arg, "|", 3)
	b := Button{Label: strings.TrimSpace(parts[0])}
	if len(parts) > 1 {
		b.Gcode = strings.TrimSpace(parts[1])
	}
	if b.Gcode == "" {
		b.Gcode = b.Label
	}
	if len(parts) > 2 {
		b.Color = strings.ToLower(strings.TrimSpace(parts[2]))
	}
	return b
}
