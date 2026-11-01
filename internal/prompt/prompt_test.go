package prompt

import (
	"fmt"
	"testing"
)

func feed(s *State, lines ...string) (changed bool) {
	for _, l := range lines {
		if s.Feed(l) {
			changed = true
		}
	}
	return
}

func TestPrompt(t *testing.T) {
	var s State
	if !feed(&s,
		"// action:prompt_begin Filament runout",
		"// action:prompt_text Lane 2 ran out.",
		"// action:prompt_text Load more, then resume.",
		"// action:prompt_button_group_start",
		"// action:prompt_button Load|LOAD_FILAMENT|primary",
		"// action:prompt_button Unload|UNLOAD_FILAMENT",
		"// action:prompt_button_group_end",
		"// action:prompt_button Resume||warning",
		"// action:prompt_footer_button Cancel print|CANCEL_PRINT|error",
		"echo: not a prompt line",
		"// action:prompt_show",
	) {
		t.Fatal("show didn't change state")
	}
	p := s.Shown
	got := fmt.Sprintf("%s %q %v %v", p.Title, p.Text, p.Buttons, p.Footer)
	want := `Filament runout ["Lane 2 ran out." "Load more, then resume."] [{Load LOAD_FILAMENT primary 0} {Unload UNLOAD_FILAMENT  0} {Resume Resume warning 1}] [{Cancel print CANCEL_PRINT error 0}]`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if !s.Feed("// action:prompt_end") || s.Shown != nil {
		t.Fatal("end didn't close it")
	}
	// Lines before begin, and a show with nothing built, do nothing.
	if feed(&s, "// action:prompt_button Stray", "// action:prompt_show") || s.Shown != nil {
		t.Fatal("a show without begin opened a prompt")
	}
	for line, want := range map[string]bool{"// action:prompt_show": true, "action:prompt_end": true, "// action:cancel": false, "ok": false} {
		if IsPromptLine(line) != want {
			t.Errorf("IsPromptLine(%q) != %v", line, want)
		}
	}
}
