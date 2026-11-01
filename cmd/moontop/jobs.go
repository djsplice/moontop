package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/moonraker"
)

// ---------------------------------------------------------------------------
// Phase 21: the Jobs screen (J or F7) — print history, totals, job queue
//
// Both come from optional Moonraker components ([history], [job_queue]);
// when one isn't enabled its tab says so instead of failing. The screen
// reloads on notify_history_changed / notify_job_queue_changed while
// open. From Files, a adds the selected file to the queue.
// ---------------------------------------------------------------------------

const (
	jobsHistory = iota
	jobsQueue
)

type jobsScreen struct {
	open     bool
	tab      int
	cursor   [2]int
	history  []moonraker.HistoryJob
	totals   *moonraker.HistoryTotals
	queue    moonraker.JobQueue
	histErr  string
	queueErr string
	loaded   bool
}

type jobsLoadedMsg struct {
	history  []moonraker.HistoryJob
	totals   *moonraker.HistoryTotals
	queue    moonraker.JobQueue
	histErr  error
	queueErr error
}

// jobsActionMsg reports a queue or history change; success reloads.
type jobsActionMsg struct {
	what string
	err  error
}

func loadJobsCmd(c *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		var msg jobsLoadedMsg
		msg.history, msg.histErr = c.History(200)
		if msg.histErr == nil {
			if t, err := c.HistoryTotals(); err == nil {
				msg.totals = &t
			}
		}
		msg.queue, msg.queueErr = c.JobQueueStatus()
		return msg
	}
}

func jobsAction(what string, f func() error) tea.Cmd {
	return func() tea.Msg { return jobsActionMsg{what: what, err: f()} }
}

// componentError turns "method not found" into "not enabled".
func componentError(err error, section string) string {
	if err == nil {
		return ""
	}
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		return fmt.Sprintf("Not enabled on this printer (add [%s] to moonraker.conf).", section)
	}
	return err.Error()
}

func (m *model) openJobs() tea.Cmd {
	m.jobs.open = true
	return loadJobsCmd(m.client)
}

func (m *model) onJobsLoaded(msg jobsLoadedMsg) {
	m.jobs.loaded = true
	m.jobs.history, m.jobs.totals, m.jobs.queue = msg.history, msg.totals, msg.queue
	m.jobs.histErr = componentError(msg.histErr, "history")
	m.jobs.queueErr = componentError(msg.queueErr, "job_queue")
	for i, n := range []int{len(m.jobs.history), len(m.jobs.queue.Jobs)} {
		m.jobs.cursor[i] = min(m.jobs.cursor[i], max(0, n-1))
	}
}

// onJobsChanged handles the two notifications: reload when on screen.
func (m *model) onJobsChanged(_ []json.RawMessage) tea.Cmd {
	if m.jobs.open {
		return loadJobsCmd(m.client)
	}
	return nil
}

// queueFile adds a file to the job queue (Files pane, History tab).
func (m *model) queueFile(name string) tea.Cmd {
	m.appendLog(">>> queue " + name)
	c := m.client
	return m.requireConnected("queue", jobsAction("add to queue", func() error { return c.JobQueueAdd(name) }))
}

func (m *model) handleJobsKey(msg tea.KeyMsg) tea.Cmd {
	j := &m.jobs
	n := len(j.history)
	if j.tab == jobsQueue {
		n = len(j.queue.Jobs)
	}
	c := m.client
	switch msg.String() {
	case "esc", "q":
		j.open = false
		return nil
	case "tab", "shift+tab", "left", "right", "h", "l":
		j.tab = 1 - j.tab
		return nil
	case "up", "k":
		if n > 0 {
			j.cursor[j.tab] = (j.cursor[j.tab] + n - 1) % n
		}
		return nil
	case "down", "j":
		if n > 0 {
			j.cursor[j.tab] = (j.cursor[j.tab] + 1) % n
		}
		return nil
	case "r":
		return loadJobsCmd(c)
	}
	if key.Matches(msg, *keys.Jobs) {
		j.open = false
		return nil
	}
	if n == 0 {
		return nil
	}
	i := min(j.cursor[j.tab], n-1)

	if j.tab == jobsQueue {
		switch msg.String() {
		case "x", "delete":
			q := j.queue.Jobs[i]
			m.appendLog(">>> unqueue " + q.Filename)
			return m.requireConnected("queue", jobsAction("remove from queue", func() error { return c.JobQueueRemove(q.JobID) }))
		case "s":
			if j.queue.State == "paused" {
				m.appendLog(">>> start the job queue")
				return m.requireConnected("queue", jobsAction("start queue", c.JobQueueStart))
			}
			m.appendLog(">>> pause the job queue")
			return m.requireConnected("queue", jobsAction("pause queue", c.JobQueuePause))
		}
		return nil
	}

	h := j.history[i]
	switch msg.String() {
	case "enter":
		switch {
		case !h.Exists:
			return m.refuse(h.Filename + " is no longer on the printer")
		case jobActive(m.job.State):
			return m.refuse("a print is already running; a queues it instead")
		}
		m.confirmation = &pendingConfirmation{prompt: "Print " + h.Filename + " again?", onYes: m.startPrint(h.Filename)}
	case "a":
		if !h.Exists {
			return m.refuse(h.Filename + " is no longer on the printer")
		}
		return m.queueFile(h.Filename)
	case "x", "delete":
		m.confirmation = &pendingConfirmation{
			prompt: "Delete the history record for " + h.Filename + "? (The file stays.)",
			onYes:  jobsAction("delete history record", func() error { return c.HistoryDelete(h.JobID) }),
		}
	}
	return nil
}

// --- rendering ---------------------------------------------------------------

func statusMark(status string) string {
	switch status {
	case "completed":
		return consoleNoticeStyle.Render("✓")
	case "cancelled":
		return consoleInfoStyle.Render("✗")
	case "in_progress":
		return jogStepStyle.Render("▶")
	}
	return consoleErrStyle.Render("!") // error, klippy_shutdown, server_exit…
}

// shortDuration is "1h12m", "5m", "40s", "—" for none.
func shortDuration(secs float64) string {
	d := time.Duration(secs) * time.Second
	switch {
	case secs <= 0:
		return "—"
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func formatFilament(mm float64) string {
	switch {
	case mm <= 0:
		return ""
	case mm >= 1e6:
		return fmt.Sprintf("%.1f km", mm/1e6)
	case mm >= 1000:
		return fmt.Sprintf("%.1f m", mm/1000)
	}
	return fmt.Sprintf("%.0f mm", mm)
}

func (m model) renderJobs() string {
	j := m.jobs
	tabs := []string{"History", fmt.Sprintf("Queue (%d)", len(j.queue.Jobs))}
	for i, t := range tabs {
		if i == j.tab {
			tabs[i] = jogStepStyle.Underline(true).Render(t)
		} else {
			tabs[i] = consoleInfoStyle.Render(t)
		}
	}
	title := appTitleStyle.Render(appTitle+"  ·  Jobs") + "   " + strings.Join(tabs, "   ")
	w := m.width - 4
	rowsFit := max(3, m.height-8)
	var lines []string
	var foot string

	window := func(rows []string, cur int) []string {
		if len(rows) <= rowsFit {
			return rows
		}
		start := max(0, cur-rowsFit+1)
		return rows[start : start+rowsFit]
	}

	switch {
	case !j.loaded:
		lines = append(lines, consoleInfoStyle.Render("Loading…"))
	case j.tab == jobsHistory && j.histErr != "":
		lines = append(lines, consoleInfoStyle.Render(j.histErr))
	case j.tab == jobsHistory:
		if t := j.totals; t != nil {
			lines = append(lines, consoleInfoStyle.Render(fmt.Sprintf("%d jobs · %s printing · %s filament · longest print %s",
				t.TotalJobs, shortDuration(t.TotalPrintTime), formatFilament(t.TotalFilamentUsed), shortDuration(t.LongestPrint))), "")
		}
		nameW := min(48, max(16, w-50))
		var rows []string
		for i, h := range j.history {
			mark := "  "
			ns := footerText
			if i == j.cursor[jobsHistory] {
				mark, ns = setupCursorStyle.Render("▸ "), jogStepStyle
			}
			name := fmt.Sprintf("%-*s", nameW, ellipsizeMiddle(h.Filename, nameW))
			if !h.Exists {
				ns = consoleInfoStyle
			}
			when := time.Unix(int64(h.StartTime), 0).Format("2006-01-02 15:04")
			row := fmt.Sprintf("%s%s %s  %s  %8s  %8s  %s", mark, statusMark(h.Status), ns.Render(name),
				consoleInfoStyle.Render(when), shortDuration(h.PrintDuration), formatFilament(h.FilamentUsed),
				consoleInfoStyle.Render(strings.ReplaceAll(h.Status, "_", " ")))
			rows = append(rows, clipWidth(row, w))
		}
		if len(rows) == 0 {
			rows = append(rows, consoleInfoStyle.Render("No prints yet."))
		}
		lines = append(lines, window(rows, j.cursor[jobsHistory])...)
		foot = "↑↓ select  •  enter print again (confirms)  •  a add to queue  •  x delete record"
	case j.queueErr != "":
		lines = append(lines, consoleInfoStyle.Render(j.queueErr))
	default:
		state := j.queue.State
		action := "s pauses it"
		if state == "paused" {
			action = "s starts it"
		}
		lines = append(lines, consoleInfoStyle.Render(fmt.Sprintf("Queue is %s · %s · from Files or History, a adds a file", state, action)), "")
		var rows []string
		for i, q := range j.queue.Jobs {
			mark, ns := "  ", footerText
			if i == j.cursor[jobsQueue] {
				mark, ns = setupCursorStyle.Render("▸ "), jogStepStyle
			}
			rows = append(rows, clipWidth(fmt.Sprintf("%s%d  %s  %s", mark, i+1, ns.Render(q.Filename),
				consoleInfoStyle.Render("queued "+shortDuration(q.TimeInQueue)+" ago")), w))
		}
		if len(rows) == 0 {
			rows = append(rows, consoleInfoStyle.Render("Nothing queued."))
		}
		lines = append(lines, window(rows, j.cursor[jobsQueue])...)
		foot = "↑↓ select  •  x remove  •  s start / pause the queue"
	}
	out := title + "\n\n" + bodyStyle.Render(strings.Join(lines, "\n")) + "\n\n"
	if m.confirmation != nil {
		out += renderPrompt(m.confirmation, m.width) + "\n"
	}
	keysLine := "tab history / queue  •  r refresh  •  esc close"
	if foot != "" {
		keysLine = foot + "  •  " + keysLine
	}
	return out + clipWidth(footerStyle.Render(keysLine), m.width) + "\n"
}
