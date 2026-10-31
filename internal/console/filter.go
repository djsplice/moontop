package console

import "regexp"

// Filter hides console lines matching Pattern. Filters only apply to
// Klipper output, never to Moontop's own command echoes and notices.
type Filter struct {
	Name    string
	Pattern *regexp.Regexp
}

// FilterSet is an ordered list of filters; a line is hidden when any
// filter matches it.
type FilterSet []Filter

// DefaultFilters are on unless the user turns them off.
//
//   - Heat-wait reports: while M109 / M190 / TEMPERATURE_WAIT block,
//     Klipper prints a "B:26.3 /0.0 T0:168.1 /140.0" line every second.
//     Same pattern as Mainsail's hideWaitTemperatures (on by default
//     there too).
//   - TMC current reports: SET_TMC_CURRENT with no new value echoes
//     "// Run Current: 0.80A Hold Current: 0.80A"; toolchanger macros
//     (the U1's among them) call it on every tool change.
var (
	WaitTempFilter   = Filter{Name: "Heat-wait temperature reports", Pattern: regexp.MustCompile(`^(?:ok\s+)?(B|C|T\d*):`)}
	RunCurrentFilter = Filter{Name: "TMC run current reports", Pattern: regexp.MustCompile(`(?i)^(?://\s*)?run current`)}

	DefaultFilters = FilterSet{WaitTempFilter, RunCurrentFilter}
)

// Hides reports whether line should be hidden. Moontop's own lines
// (command echoes, notices, errors) are never hidden.
func (fs FilterSet) Hides(line string) bool {
	switch ClassifyLine(line) {
	case KindCommand, KindNotice, KindError:
		return false
	}
	for _, f := range fs {
		if f.Pattern.MatchString(line) {
			return true
		}
	}
	return false
}
