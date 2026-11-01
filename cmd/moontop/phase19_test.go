package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/moonraker"
	"github.com/justinh-rahb/moontop/internal/prompt"
)

var runoutPrompt = []string{
	"// action:prompt_begin Filament runout",
	"// action:prompt_text Lane 2 ran out.",
	"// action:prompt_button_group_start",
	"// action:prompt_button Load|LOAD_FILAMENT|primary",
	"// action:prompt_button Unload|UNLOAD_FILAMENT",
	"// action:prompt_button_group_end",
	"// action:prompt_footer_button Cancel print|CANCEL_PRINT|error",
	"// action:prompt_show",
}

func respond(t *testing.T, m model, lines ...string) model {
	t.Helper()
	for _, l := range lines {
		m.onGcodeResponse(rawParams(t, l))
	}
	return m
}

func TestMacroPrompt(t *testing.T) {
	m := readyModel(t, fakePrinter(t))
	m = respond(t, m, append([]string{"echo: before"}, runoutPrompt...)...)
	if m.prompt.Shown == nil {
		t.Fatal("prompt not shown")
	}
	if logContains(m, "action:prompt") || !logContains(m, "echo: before") {
		t.Fatal("prompt protocol leaked into the console (or real output was dropped)")
	}
	v := m.View()
	for _, s := range []string{"Macro prompt", "Filament runout", "Lane 2 ran out.", "[ Load ]", "[ Unload ]", "[ Cancel print ]"} {
		if !strings.Contains(v, s) {
			t.Errorf("prompt view lacks %q:\n%s", s, v)
		}
	}
	// → moves between buttons (footer included); enter sends its gcode
	// and the prompt stays until the printer ends it.
	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if g := sentGcode(t, cmd); g != "UNLOAD_FILAMENT" || m.prompt.Shown == nil {
		t.Fatalf("enter sent %q, shown %v", g, m.prompt.Shown != nil)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if _, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter}); sentGcode(t, cmd) != "CANCEL_PRINT" {
		t.Fatal("footer button not reachable")
	}
	// Other keys don't reach the dashboard; E-stop does.
	m = update(t, m, keyRunes("q"))
	if m.prompt.Shown == nil {
		t.Fatal("q closed the prompt")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.confirmation == nil || !strings.Contains(m.View(), "EMERGENCY") {
		t.Fatal("e-stop not armed over a prompt")
	}
	m = update(t, m, keyRunes("n"))
	// esc closes it from our side, the way Mainsail's ✕ does.
	if _, cmd = updateCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc}); sentGcode(t, cmd) != prompt.Close {
		t.Fatal("esc didn't send prompt_end")
	}
	if m = respond(t, m, "// action:prompt_end"); m.prompt.Shown != nil || strings.Contains(m.View(), "Macro prompt") {
		t.Fatal("prompt_end didn't close it")
	}
}

func TestPromptFromHistory(t *testing.T) {
	m := readyModel(t, fakePrinter(t))
	var entries []moonraker.GcodeStoreEntry
	for _, l := range runoutPrompt {
		entries = append(entries, moonraker.GcodeStoreEntry{Message: l, Type: "response"})
	}
	m = update(t, m, gcodeStoreMsg{entries: entries})
	if m.prompt.Shown == nil || logContains(m, "action:prompt") {
		t.Fatal("an open prompt in the history didn't show (or leaked)")
	}
	// A live prompt_end before the history arrives wins over it.
	m = readyModel(t, fakePrinter(t))
	m = respond(t, m, "// action:prompt_end")
	if m = update(t, m, gcodeStoreMsg{entries: entries}); m.prompt.Shown != nil {
		t.Fatal("history reopened a prompt the printer already closed")
	}
}

func excludeModel(t *testing.T) model {
	t.Helper()
	m := readyModel(t, fakePrinter(t))
	m.raw.merge("exclude_object", map[string]any{
		"objects": []any{
			map[string]any{"name": "CUBE_1", "center": []any{100.0, 100.0}},
			map[string]any{"name": "CUBE_2", "center": []any{150.0, 100.0}},
			map[string]any{"name": "BENCHY", "center": []any{125.0, 150.0}},
		},
		"excluded_objects": []any{"CUBE_1"},
		"current_object":   "CUBE_2",
	})
	return m
}

func TestExcludeObject(t *testing.T) {
	m := excludeModel(t)
	// Only during a print.
	if m = update(t, m, keyRunes("O")); m.exclude.open || !logContains(m, "during a print") {
		t.Fatal("O outside a print")
	}
	m.job.State = "printing"
	if _, job := footerHints(&m); !strings.Contains(strings.Join(job, "|"), "O/F4 objects") {
		t.Fatalf("job hints %v", job)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyF4})
	if !m.exclude.open || m.exclude.cursor != 1 {
		t.Fatalf("F4: open %v cursor %d (want the current object)", m.exclude.open, m.exclude.cursor)
	}
	v := m.View()
	for _, s := range []string{"Objects (3)", "CUBE_1", "excluded", "CUBE_2", "printing now", "at 125, 150"} {
		if !strings.Contains(v, s) {
			t.Errorf("list lacks %q:\n%s", s, v)
		}
	}
	// Already excluded: refused.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp}, tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirmation != nil || !logContains(m, "already excluded") {
		t.Fatal("excluded an excluded object")
	}
	// Exclude BENCHY: confirms, then sends and echoes.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp}, tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirmation == nil || !strings.Contains(m.View(), "Exclude BENCHY?") {
		t.Fatalf("no confirmation in the list:\n%s", m.View())
	}
	m, cmd := updateCmd(t, m, keyRunes("y"))
	msg := cmd()
	et, ok := msg.(echoThen)
	if !ok {
		t.Fatalf("confirm returned %T", msg)
	}
	if sent, _ := et.msg.(gcodeSentMsg); sent.cmd != "EXCLUDE_OBJECT NAME=BENCHY" {
		t.Fatalf("sent %+v", et.msg)
	}
	m = update(t, m, et)
	if !logContains(m, ">>> EXCLUDE_OBJECT NAME=BENCHY") || !m.exclude.open {
		t.Fatal("not echoed, or the list closed")
	}
	if m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc}); m.exclude.open {
		t.Fatal("esc didn't close the list")
	}
}
