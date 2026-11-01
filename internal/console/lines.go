// Package console holds the pieces shared by every Moontop console
// frontend: the full-screen dashboard's console pane, the line-based
// `moontop console` REPL, and one-shot `moontop gcode`.
//
// It deliberately has no styling of its own. Frontends classify lines
// here and map each Kind to whatever styles suit their output.
package console

import (
	"regexp"
	"strings"

	"github.com/justinh-rahb/moontop/internal/moonraker"
)

// Kind classifies one console line for styling.
type Kind int

const (
	// KindNormal is ordinary Klipper output ("ok", temperature reports).
	KindNormal Kind = iota
	// KindError is a Klipper error ("!! Move out of range") or a
	// Moontop-generated failure using the same prefix.
	KindError
	// KindInfo is a Klipper informational echo ("// Klipper state: Ready").
	KindInfo
	// KindCommand is a command that was sent (">>> G28").
	KindCommand
	// KindNotice is a Moontop status notice ("** reconnected").
	KindNotice
)

// Line prefixes. Klipper owns "!!" and "//"; Moontop uses ">>> " for
// sent commands and "** " for its own notices.
const (
	PrefixError   = "!!"
	PrefixInfo    = "//"
	PrefixCommand = ">>> "
	PrefixNotice  = "** "
)

// ClassifyLine reports the Kind of a single (already split) line.
func ClassifyLine(l string) Kind {
	switch {
	case strings.HasPrefix(l, PrefixError):
		return KindError
	case strings.HasPrefix(l, PrefixInfo):
		return KindInfo
	case strings.HasPrefix(l, PrefixCommand):
		return KindCommand
	case strings.HasPrefix(l, PrefixNotice):
		return KindNotice
	}
	return KindNormal
}

// BackfillLines converts server.gcode_store entries to console lines,
// marking commands with PrefixCommand so they render like ones typed
// here. Multi-line messages are split; in a multi-line command (a
// script, such as Moontop's own jogs) every line is marked, and the
// SAVE/RESTORE_GCODE_STATE wrapper Moontop puts around its moves is
// left out — it's plumbing, not something anyone typed.
func BackfillLines(entries []moonraker.GcodeStoreEntry) []string {
	var lines []string
	for _, e := range entries {
		if e.Type != "command" {
			lines = append(lines, strings.Split(e.Message, "\n")...)
			continue
		}
		var cmds []string
		for _, l := range strings.Split(e.Message, "\n") {
			if l = strings.TrimSpace(l); l != "" && !moontopWrapper.MatchString(l) {
				cmds = append(cmds, l)
			}
		}
		// A jog is three lines once unwrapped (G91, G1 …); keep it on one.
		if len(cmds) > 1 && len(cmds) <= 3 && len(cmds) < len(strings.Split(e.Message, "\n")) {
			cmds = []string{strings.Join(cmds, " · ")}
		}
		for _, c := range cmds {
			lines = append(lines, PrefixCommand+c)
		}
	}
	return lines
}

// moontopWrapper matches the state save/restore around Moontop's moves.
var moontopWrapper = regexp.MustCompile(`^(SAVE|RESTORE)_GCODE_STATE NAME=_ui_\w+$`)

// ErrorLine renders an RPC error the way Klipper renders its own:
// "!! <message>". Moonraker passes Klipper's gcode errors through as
// the JSON-RPC error message, sometimes already "!!"-prefixed.
func ErrorLine(err error) string {
	msg := err.Error()
	// Strip the client's "JSON-RPC error 400: " wrapper; the code adds
	// nothing for a person reading the console.
	if i := strings.Index(msg, ": "); i >= 0 && strings.HasPrefix(msg, "JSON-RPC error ") {
		msg = msg[i+2:]
	}
	if strings.HasPrefix(msg, PrefixError) {
		return msg
	}
	return PrefixError + " " + msg
}
