package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
)

// ---------------------------------------------------------------------------
// Phase 23: updates and announcements (U or F9)
//
// Updates: Moonraker's update manager — each component's version, the
// remote version and the commits behind, the incoming commit subjects
// for the selected one. r checks for new versions, enter updates the
// selected component, a updates everything (both confirm; refused while
// printing, as Moonraker refuses too). An update's output streams in
// (notify_update_response) below the list: the long-running operation
// with streamed progress.
//
// Announcements: Moonraker's news feed; d dismisses one.
//
// The status is read at connect, so the dashboard title can show a
// badge when something's waiting.
// ---------------------------------------------------------------------------

const (
	updTab = iota
	newsTab
)

type updatesScreen struct {
	open    bool
	tab     int
	cursor  [2]int
	status  map[string]any // machine.update.status
	news    []moonraker.Announcement
	updErr  string
	newsErr string
	loaded  bool
	log     []string // streamed update output
	running string   // component being updated ("" when idle)
}

type updatesLoadedMsg struct {
	status    map[string]any
	news      []moonraker.Announcement
	updErr    error
	newsErr   error
	refreshed bool
}

type updateActionMsg struct {
	what string
	err  error
}

func loadUpdatesCmd(c *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		var msg updatesLoadedMsg
		msg.status, msg.updErr = c.UpdateStatus()
		msg.news, msg.newsErr = c.Announcements()
		return msg
	}
}

func (m *model) onUpdatesLoaded(msg updatesLoadedMsg) {
	u := &m.updates
	u.loaded = true
	u.status, u.news = msg.status, msg.news
	u.updErr = componentError(msg.updErr, "update_manager")
	u.newsErr = componentError(msg.newsErr, "announcements")
	u.cursor[updTab] = min(u.cursor[updTab], max(0, len(m.updateNames())-1))
	u.cursor[newsTab] = min(u.cursor[newsTab], max(0, len(u.news)-1))
}

// --- reading version_info ------------------------------------------------------

func (m *model) versionInfo() map[string]any {
	vi, _ := dig(m.updates.status, "version_info").(map[string]any)
	return vi
}

// updateNames: system, moonraker, klipper, then the rest by name.
func (m *model) updateNames() []string {
	vi := m.versionInfo()
	rank := map[string]int{"system": 0, "moonraker": 1, "klipper": 2}
	var names []string
	for n := range vi {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		ri, iok := rank[names[i]]
		rj, jok := rank[names[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		}
		return names[i] < names[j]
	})
	return names
}

// componentState: a short state and whether an update is available.
func (m *model) componentState(name string) (state string, available bool) {
	e := m.versionInfo()[name]
	if name == "system" {
		if n, _ := digF(e, "package_count"); n > 0 {
			return fmt.Sprintf("%.0f packages to update", n), true
		}
		return "up to date", false
	}
	switch {
	case dig(e, "corrupt") == true:
		return "corrupt (recover in Mainsail / Fluidd)", false
	case dig(e, "is_valid") == false:
		return "invalid", false
	case dig(e, "is_dirty") == true:
		return "dirty (local changes)", false
	case dig(e, "detached") == true:
		return "detached", false
	}
	behind := len(stringListOrAny(dig(e, "commits_behind")))
	cur, remote := digS(e, "version"), digS(e, "remote_version")
	if behind > 0 {
		return fmt.Sprintf("%d commits behind", behind), true
	}
	if remote != "" && remote != "?" && cur != "" && remote != cur {
		return "update available", true
	}
	return "up to date", false
}

func stringListOrAny(v any) []any {
	l, _ := v.([]any)
	return l
}

// waiting counts available updates and unread announcements (the
// title badge).
func (m *model) waiting() (updates, news int) {
	for _, n := range m.updateNames() {
		if _, ok := m.componentState(n); ok {
			updates++
		}
	}
	return updates, len(m.updates.news)
}

// --- notifications -------------------------------------------------------------

func (m *model) onUpdateResponse(params []json.RawMessage) tea.Cmd {
	p := firstParam(params)
	if msg, _ := p["message"].(string); msg != "" {
		for _, l := range strings.Split(strings.TrimRight(msg, "\n"), "\n") {
			m.updates.log = append(m.updates.log, l)
		}
		if over := len(m.updates.log) - 300; over > 0 {
			m.updates.log = m.updates.log[over:]
		}
	}
	if p["complete"] == true {
		m.updates.running = ""
		return loadUpdatesCmd(m.client)
	}
	return nil
}

func (m *model) onUpdatesChanged(_ []json.RawMessage) tea.Cmd { return loadUpdatesCmd(m.client) }

// --- keys --------------------------------------------------------------------------

func (m *model) handleUpdatesKey(msg tea.KeyMsg) tea.Cmd {
	u := &m.updates
	c := m.client
	names := m.updateNames()
	n := len(names)
	if u.tab == newsTab {
		n = len(u.news)
	}
	switch msg.String() {
	case "esc", "q":
		u.open = false
		return nil
	case "tab", "shift+tab", "left", "right", "h", "l":
		u.tab = 1 - u.tab
		return nil
	case "up", "k":
		if n > 0 {
			u.cursor[u.tab] = (u.cursor[u.tab] + n - 1) % n
		}
		return nil
	case "down", "j":
		if n > 0 {
			u.cursor[u.tab] = (u.cursor[u.tab] + 1) % n
		}
		return nil
	}
	if key.Matches(msg, *keys.Updates) {
		u.open = false
		return nil
	}
	if u.tab == newsTab {
		if msg.String() == "d" && n > 0 {
			a := u.news[min(u.cursor[newsTab], n-1)]
			return m.requireConnected("dismiss", func() tea.Msg {
				return updateActionMsg{what: "dismiss " + a.Title, err: c.DismissAnnouncement(a.EntryID)}
			})
		}
		return nil
	}

	switch msg.String() {
	case "r":
		u.log = append(u.log, "** checking for updates…")
		return m.requireConnected("refresh", func() tea.Msg {
			return updateActionMsg{what: "check for updates", err: c.UpdateRefresh("")}
		})
	case "enter", "a":
		if jobActive(m.job.State) {
			return m.refuse("updates wait until the print is done")
		}
		if u.running != "" {
			return m.refuse(u.running + " is still updating")
		}
		var targets []string
		if msg.String() == "a" {
			for _, nm := range names {
				if _, ok := m.componentState(nm); ok {
					targets = append(targets, nm)
				}
			}
		} else if n > 0 {
			nm := names[min(u.cursor[updTab], n-1)]
			if _, ok := m.componentState(nm); !ok {
				return m.refuse(nm + " has no update to install")
			}
			targets = []string{nm}
		}
		if len(targets) == 0 {
			return m.refuse("nothing to update")
		}
		m.confirmation = &pendingConfirmation{
			prompt: "Update " + strings.Join(targets, ", ") + "? Klipper and Moonraker restart as needed.",
			onYes:  m.runUpdates(targets),
		}
	}
	return nil
}

// runUpdates updates components one after another (Moonraker allows
// one at a time); output streams in through notify_update_response.
func (m *model) runUpdates(targets []string) tea.Cmd {
	c := m.client
	m.updates.running = strings.Join(targets, ", ")
	return func() tea.Msg {
		for _, t := range targets {
			if err := c.Upgrade(t); err != nil {
				return updateActionMsg{what: "update " + t, err: err}
			}
		}
		return updateActionMsg{what: "update " + strings.Join(targets, ", ")}
	}
}

// --- rendering ---------------------------------------------------------------------

func (m model) titleBadge() string {
	upd, news := m.waiting()
	var parts []string
	if upd > 0 {
		parts = append(parts, fmt.Sprintf("⬆ %d update%s", upd, map[bool]string{true: "s"}[upd > 1]))
	}
	if news > 0 {
		parts = append(parts, fmt.Sprintf("✉ %d", news))
	}
	if len(parts) == 0 {
		return ""
	}
	return "   " + jogStepStyle.Render(strings.Join(parts, " · ")) + consoleInfoStyle.Render("  (U)")
}

func (m model) renderUpdates() string {
	u := m.updates
	upd, news := m.waiting()
	tabs := []string{fmt.Sprintf("Updates (%d)", upd), fmt.Sprintf("Announcements (%d)", news)}
	for i, t := range tabs {
		if i == u.tab {
			tabs[i] = jogStepStyle.Underline(true).Render(t)
		} else {
			tabs[i] = consoleInfoStyle.Render(t)
		}
	}
	title := appTitleStyle.Render(appTitle+"  ·  Software") + "   " + strings.Join(tabs, "   ")
	w := m.width - 4
	var lines []string
	foot := ""
	switch {
	case !u.loaded:
		lines = append(lines, consoleInfoStyle.Render("Loading…"))
	case u.tab == updTab && u.updErr != "":
		lines = append(lines, consoleInfoStyle.Render(u.updErr))
	case u.tab == updTab:
		names := m.updateNames()
		cur := min(u.cursor[updTab], max(0, len(names)-1))
		for i, nm := range names {
			e := m.versionInfo()[nm]
			mark, ns := "  ", footerText
			if i == cur {
				mark, ns = setupCursorStyle.Render("▸ "), jogStepStyle
			}
			state, avail := m.componentState(nm)
			st := consoleNoticeStyle
			switch {
			case avail:
				st = jogStepStyle
			case state != "up to date":
				st = consoleErrStyle
			}
			ver := digS(e, "version")
			if r := digS(e, "remote_version"); avail && r != "" && r != "?" && r != ver {
				ver += " → " + r
			}
			lines = append(lines, clipWidth(fmt.Sprintf("%s%s  %-34s  %s", mark, ns.Render(fmt.Sprintf("%-12s", nm)), ver, st.Render(state)), w))
		}
		if len(names) == 0 {
			lines = append(lines, consoleInfoStyle.Render("The update manager lists nothing."))
		}
		// The selected component's incoming commits.
		if len(names) > 0 {
			commits := stringListOrAny(dig(m.versionInfo()[names[cur]], "commits_behind"))
			if len(commits) > 0 {
				lines = append(lines, "", helpGroupStyle.Render("Incoming for "+names[cur]))
				for i, c := range commits {
					if i == 8 {
						lines = append(lines, consoleInfoStyle.Render(fmt.Sprintf("  … and %d more", len(commits)-8)))
						break
					}
					sha := digS(c, "sha")
					if len(sha) > 7 {
						sha = sha[:7]
					}
					lines = append(lines, clipWidth(consoleInfoStyle.Render("  "+sha+"  ")+digS(c, "subject")+consoleInfoStyle.Render("  "+digS(c, "author")), w))
				}
			}
		}
		if len(u.log) > 0 || u.running != "" {
			head := "Output"
			if u.running != "" {
				head = "Updating " + u.running + "…"
			}
			lines = append(lines, "", helpGroupStyle.Render(head))
			fit := max(3, m.height-len(lines)-8)
			log := u.log
			if len(log) > fit {
				log = log[len(log)-fit:]
			}
			for _, l := range log {
				lines = append(lines, clipWidth("  "+styleConsoleLine(l), w))
			}
		}
		foot = "↑↓ select  •  enter update (confirms)  •  a update all  •  r check for updates"
	case u.newsErr != "":
		lines = append(lines, consoleInfoStyle.Render(u.newsErr))
	default:
		cur := min(u.cursor[newsTab], max(0, len(u.news)-1))
		for i, a := range u.news {
			mark, ns := "  ", footerText
			if i == cur {
				mark, ns = setupCursorStyle.Render("▸ "), jogStepStyle
			}
			pri := ""
			if a.Priority == "high" {
				pri = consoleErrStyle.Render("! ")
			}
			when := time.Unix(int64(a.Date), 0).Format("2006-01-02")
			lines = append(lines, clipWidth(mark+pri+ns.Render(a.Title)+consoleInfoStyle.Render("  "+when+"  "+a.Feed), w))
		}
		if len(u.news) == 0 {
			lines = append(lines, consoleInfoStyle.Render("No announcements."))
		} else {
			a := u.news[cur]
			lines = append(lines, "")
			for _, l := range strings.Split(wordWrap(a.Description, min(w, 100)), "\n") {
				lines = append(lines, "  "+l)
			}
			if a.URL != "" {
				lines = append(lines, "", consoleInfoStyle.Render("  "+a.URL))
			}
		}
		foot = "↑↓ select  •  d dismiss"
	}
	out := title + "\n\n" + bodyStyle.Render(strings.Join(lines, "\n")) + "\n\n"
	if m.confirmation != nil {
		out += renderPrompt(m.confirmation, m.width) + "\n"
	}
	keysLine := "tab updates / announcements  •  esc close"
	if foot != "" {
		keysLine = foot + "  •  " + keysLine
	}
	return out + clipWidth(footerStyle.Render(keysLine), m.width) + "\n"
}

// wordWrap breaks text at spaces to width columns.
func wordWrap(s string, width int) string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, w := range strings.Fields(para) {
			if line != "" && len(line)+1+len(w) > width {
				out = append(out, line)
				line = w
				continue
			}
			if line != "" {
				line += " "
			}
			line += w
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
