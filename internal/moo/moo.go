// Package moo has nothing to do with 3D printing.
//
// Requested on the Moontop Discord: "No no, you don't understand. It
// needs more cowbell." (legitsu, 2026-10-31). Type COWBELL (or MOO) at
// a console prompt, or run `moontop moo`. Not in the help, on purpose.
package moo

import (
	"math/rand"
	"strings"
)

// Sayings the cow picks from.
var Sayings = []string{
	"More cowbell.",
	"Needs more cowbell.",
	"I've got a fever, and the only prescription is more cowbell.",
	"Your first layer is fine. Your cowbell is not.",
	"Klipper says ready. The cow says moo.",
	"Never trust a print that hasn't heard the cowbell.",
	"Moo. (This APT has nothing on Moontop.)",
}

// cow is original art: a cow wearing a cowbell. Line 0 continues the
// speech bubble's tail.
var cow = []string{
	`      \`,
	`       \   _       _`,
	`          ( \_____/ )`,
	`           ( o   o )_____________`,
	`            \  -  /               )~`,
	`             '---'|---------------|`,
	`             [###] ||           ||`,
	`              \_/  ^^           ^^`,
}

// Say draws the cow saying msg, wrapped to width columns (minimum 20).
func Say(msg string, width int) []string {
	width = max(20, width)
	var wrapped []string
	line := ""
	for _, w := range strings.Fields(msg) {
		if line != "" && len(line)+1+len(w) > width {
			wrapped = append(wrapped, line)
			line = w
			continue
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" || len(wrapped) == 0 {
		wrapped = append(wrapped, line)
	}
	inner := 0
	for _, l := range wrapped {
		inner = max(inner, len(l))
	}
	out := []string{" " + strings.Repeat("_", inner+2)}
	for i, l := range wrapped {
		left, right := "|", "|"
		switch {
		case len(wrapped) == 1:
			left, right = "<", ">"
		case i == 0:
			left, right = "/", `\`
		case i == len(wrapped)-1:
			left, right = `\`, "/"
		}
		out = append(out, left+" "+l+strings.Repeat(" ", inner-len(l))+" "+right)
	}
	out = append(out, " "+strings.Repeat("-", inner+2))
	return append(out, cow...)
}

// Random picks a saying.
func Random() string { return Sayings[rand.Intn(len(Sayings))] }

// Trigger reports whether a console line is the easter egg, and whether
// it asked for the bell (COWBELL rings the printer's beeper, if any).
func Trigger(line string) (cow, bell bool) {
	switch strings.ToUpper(strings.TrimSpace(line)) {
	case "COWBELL", "MORE COWBELL":
		return true, true
	case "MOO":
		return true, false
	}
	return false, false
}

// Bell is two short dings for printers with M300 (a beeper).
const Bell = "M300 S1568 P90\nG4 P60\nM300 S1568 P90"
