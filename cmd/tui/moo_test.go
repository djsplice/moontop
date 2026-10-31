package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moo"
)

func TestMoreCowbell(t *testing.T) {
	m := readyModel(t, fakePrinter(t))
	m.setFocus(focusConsole)
	type_ := func(s string) (model, tea.Cmd) {
		for _, r := range s {
			m = update(t, m, keyRunes(string(r)))
		}
		return updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	}
	// No beeper: the cow appears, nothing is sent.
	m, cmd := type_("cowbell")
	if cmd != nil || !logContains(m, "_ o o _") || logContains(m, ">>> cowbell") {
		t.Fatalf("cowbell without M300: cmd %v", cmd != nil)
	}
	// With a beeper, COWBELL rings it; MOO doesn't.
	m.gcodeCommands = map[string]bool{"M300": true}
	if m, cmd = type_("COWBELL"); sentGcode(t, cmd) != moo.Bell {
		t.Fatal("COWBELL didn't ring the bell")
	}
	if m, cmd = type_("moo"); cmd != nil {
		t.Fatal("MOO sent something")
	}
	if strings.Contains(strings.Join(helpTextForTest(&m), "\n"), "owbell") {
		t.Fatal("the easter egg is in the help")
	}
}

func helpTextForTest(m *model) []string { return m.helpLines() }
