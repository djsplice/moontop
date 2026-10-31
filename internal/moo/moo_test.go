package moo

import (
	"strings"
	"testing"
)

func TestSay(t *testing.T) {
	got := strings.Join(Say("More cowbell.", 40), "\n")
	if !strings.HasPrefix(got, " _______________\n< More cowbell. >\n ---------------\n      \\") || !strings.Contains(got, "_ o o _") {
		t.Fatalf("one line:\n%s", got)
	}
	long := Say("I've got a fever, and the only prescription is more cowbell.", 24)
	if !strings.HasPrefix(long[1], "/ ") || !strings.HasPrefix(long[len(long)-len(cow)-2], `\ `) {
		t.Fatalf("multi-line bubble:\n%s", strings.Join(long, "\n"))
	}
	for _, l := range long[1 : len(long)-len(cow)-1] {
		if len(l) != len(long[1]) {
			t.Fatalf("ragged bubble:\n%s", strings.Join(long, "\n"))
		}
	}
}

func TestTrigger(t *testing.T) {
	for in, want := range map[string][2]bool{"cowbell": {true, true}, " More Cowbell ": {true, true}, "MOO": {true, false}, "M300": {false, false}} {
		if c, b := Trigger(in); c != want[0] || b != want[1] {
			t.Errorf("Trigger(%q) = %v %v", in, c, b)
		}
	}
}
