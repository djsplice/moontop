package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Layout
//
// One pass on tea.WindowSizeMsg → one layout struct → render functions
// read from it. Resize math lives here, not scattered across views.
//
// All dimensions are *outer* box sizes (border + padding + content)
// unless the field name says otherwise.
// ---------------------------------------------------------------------------

type layoutMode int

const (
	// layoutWide: temp + jog side by side on top, console full-width below.
	// This is the normal Phase 5 layout.
	layoutWide layoutMode = iota

	// layoutStacked: terminal is too narrow for two columns; stack all
	// three panes vertically (temp → jog → console).
	layoutStacked

	// layoutMinimal: terminal can't even fit the stacked layout; show
	// a "too small" message instead of attempting to render.
	layoutMinimal
)

// jogContentMin is the number of content lines renderJog needs.
// 3 (pos) + 1 + 3 (pad) + 1 + 1 (step) + 1 + 3 (hint) = 13.
const jogContentMin = 13

// Minimum terminal sizes. Below the wide threshold we stack; below the
// minimum threshold we give up and print a message.
const (
	minWideW = 90
	minTermW = 50
	minTermH = 16
)

type layout struct {
	mode layoutMode

	// Wide and stacked modes both populate these.
	tableBoxW   int
	jogBoxW     int
	filesBoxW   int
	tuningBoxW  int
	consoleBoxW int

	// Wide mode: shared outer height for the top row.
	topRowH int

	// Stacked mode: per-pane outer heights.
	tableH  int
	jogH    int
	filesH  int
	tuningH int

	// Console pane: viewport (inner content) height.
	consoleViewportH int
	inputW           int

	// Inner table content width — bubbles/table column widths must sum
	// to this to avoid wrapping its header row.
	tableContentW int

	// Inner files list content size — used to size bubbles/list.
	filesContentW int
	filesContentH int
}

// computeLayout produces a layout for the current terminal size.
//
// sensorCount is the number of rows the temp table wants to show — used
// to hug the top-row height to actual content when there's room, and
// to compute the inner table height.
//
// reservedRows is extra vertical chrome outside the panes (currently the
// Klippy banner); it shrinks the space handed to the panes.
func computeLayout(termW, termH, sensorCount, reservedRows int) layout {
	if termW < minTermW || termH < minTermH {
		return layout{mode: layoutMinimal}
	}

	// Reserve outer-frame chrome: app title (1) + sep (1) + body block +
	// sep (1) + footer (1) + trailing newline (1) = 4 chrome lines plus
	// 1 left margin + 3 right gutter columns.
	const (
		chromeV = 5 // title + sep + sep + footer + trailing-line safety
		marginL = 2
		gutterR = 4
	)

	availW := termW - marginL - gutterR
	availH := termH - chromeV - reservedRows
	if availW < 1 {
		availW = 1
	}
	if availH < 1 {
		availH = 1
	}

	if termW < minWideW {
		return computeStacked(availW, availH, sensorCount)
	}
	return computeWide(availW, availH, sensorCount)
}

// computeWide: temp + jog + tuning + files side by side on top,
// console below. Width budget: jog and tuning are roughly fixed (they
// hold compact widgets), files takes a chunk, table absorbs the rest.
func computeWide(availW, availH, sensorCount int) layout {
	jogBoxW := 26
	if jogBoxW > availW/5 {
		jogBoxW = availW / 5
	}
	tuningBoxW := 30
	if tuningBoxW > availW/5 {
		tuningBoxW = availW / 5
	}
	filesBoxW := (availW - jogBoxW - tuningBoxW) * 1 / 3
	if filesBoxW < 28 {
		filesBoxW = 28
	}
	tableBoxW := availW - jogBoxW - tuningBoxW - filesBoxW
	if tableBoxW < 32 {
		tableBoxW = 32
	}

	tableContentW := tableBoxW - panelInnerXReserve()
	if tableContentW < 30 {
		tableContentW = 30
	}

	// Outer top-row height = max(pane inner needs) + borders.
	tableInner := 2 + sensorCount
	jogInner := 1 + jogContentMin    // title + jog body
	filesInner := 1 + 8              // title + ~8 rows minimum
	tuningInner := 1 + tuningRowsMin // title + label/input rows + error line
	innerH := tableInner
	for _, h := range []int{jogInner, filesInner, tuningInner} {
		if h > innerH {
			innerH = h
		}
	}
	topRowH := innerH + 2
	if topRowH > availH-5 {
		topRowH = availH - 5
		if topRowH < 8 {
			topRowH = 8
		}
	}

	consoleOuter := availH - topRowH
	consoleViewportH := consoleOuter - 2 - 1 - 1
	if consoleViewportH < 3 {
		consoleViewportH = 3
	}

	return layout{
		mode:             layoutWide,
		tableBoxW:        tableBoxW,
		jogBoxW:          jogBoxW,
		filesBoxW:        filesBoxW,
		tuningBoxW:       tuningBoxW,
		consoleBoxW:      availW,
		topRowH:          topRowH,
		consoleViewportH: consoleViewportH,
		inputW:           availW - panelInnerXReserve() - 2,
		tableContentW:    tableContentW,
		filesContentW:    filesBoxW - panelInnerXReserve(),
		filesContentH:    topRowH - 2 - 1,
	}
}

// tuningRowsMin counts the inner lines the tuning pane needs:
// 4 fields × (label + input) + 1 line for inline errors = 9.
const tuningRowsMin = 9

// computeStacked: narrow terminal — every pane full-width, stacked.
func computeStacked(availW, availH, sensorCount int) layout {
	tableInner := 2 + sensorCount
	jogInner := 1 + jogContentMin
	filesInner := 1 + 6
	tuningInner := 1 + tuningRowsMin

	tableH := tableInner + 2
	jogH := jogInner + 2
	filesH := filesInner + 2
	tuningH := tuningInner + 2

	consoleOuter := availH - tableH - jogH - filesH - tuningH
	if consoleOuter < 7 {
		consoleOuter = 7
		over := availH - tableH - jogH - filesH - tuningH - consoleOuter
		// over is negative if we overflow — shrink files first, then table.
		for over < 0 && filesH > 6 {
			filesH--
			over++
		}
		for over < 0 && tableH > 5 {
			tableH--
			over++
		}
	}

	consoleViewportH := consoleOuter - 2 - 1 - 1
	if consoleViewportH < 3 {
		consoleViewportH = 3
	}

	contentW := availW - panelInnerXReserve()
	if contentW < 30 {
		contentW = 30
	}

	return layout{
		mode:             layoutStacked,
		tableBoxW:        availW,
		jogBoxW:          availW,
		filesBoxW:        availW,
		tuningBoxW:       availW,
		consoleBoxW:      availW,
		tableH:           tableH,
		jogH:             jogH,
		filesH:           filesH,
		tuningH:          tuningH,
		consoleViewportH: consoleViewportH,
		inputW:           availW - panelInnerXReserve() - 2,
		tableContentW:    contentW,
		filesContentW:    contentW,
		filesContentH:    filesH - 2 - 1,
	}
}

// panelInnerXReserve returns the columns consumed by a panel's border
// + padding on both sides. Encapsulated here so the panel chrome
// constant lives next to renderPanel.
func panelInnerXReserve() int {
	return 4 // 1 border + 1 padding per side
}

// ---------------------------------------------------------------------------
// renderPanel
//
// All three panes funnel through this — same border, same title bar,
// same focus-color swap. Body text is rendered as-is inside.
// ---------------------------------------------------------------------------

// renderPanel produces a bordered, titled box of exactly outerW x outerH
// rendered columns/rows.
//
// IMPORTANT: lipgloss Width/Height set the *content area* (padding
// included, borders not). To make the rendered output exactly outerW
// wide, subtract 2 for the left+right border. Same for height with the
// top+bottom border. Getting this off-by-2 is what made the rightmost
// pane overflow into the terminal's right edge before.
func renderPanel(title, body string, focused bool, outerW, outerH int) string {
	innerW := outerW - 2
	if innerW < 1 {
		innerW = 1
	}
	style := panelStyle.Width(innerW)
	if outerH > 0 {
		innerH := outerH - 2
		if innerH < 1 {
			innerH = 1
		}
		style = style.Height(innerH)
	}
	titleStyle := panelTitleStyle
	if focused {
		style = style.BorderForeground(colorBorderFocused)
		titleStyle = panelTitleFocusedStyle
	}

	bar := titleStyle.Render(title)
	content := lipgloss.JoinVertical(lipgloss.Left, bar, body)
	return style.Render(content)
}

// ---------------------------------------------------------------------------
// Footer
//
// Context-sensitive: shows the active pane's keybindings, plus the
// always-on global ones.
// ---------------------------------------------------------------------------

func renderFooter(f focusArea, job printJob, barView, connView string) string {
	var paneName, paneKeys string
	switch f {
	case focusTable:
		paneName = "Temperatures"
		paneKeys = "↑/↓: select"
	case focusConsole:
		paneName = "Console"
		paneKeys = "type gcode + enter to send"
	case focusJog:
		paneName = "Toolhead"
		paneKeys = "arrows: jog XY  •  PgUp/PgDn: jog Z  •  [/]: step  •  H: home"
	case focusFiles:
		paneName = "Files"
		paneKeys = "↑/↓: select  •  /: filter  •  enter: print"
	case focusTuning:
		paneName = "Tuning"
		paneKeys = "↑/↓: field  •  enter: apply"
	}
	conn := connView + footerStyle.Render("  •  ")
	left := footerFocusStyle.Render(paneName)
	mid := footerStyle.Render("  " + paneKeys)
	jobText := renderJobSummary(job, barView)
	jobKeys := footerStyle.Render(jobControlHints(job))
	right := footerStyle.Render("  •  ctrl+x: e-stop  •  tab: next pane  •  ctrl+c: quit")
	return lipgloss.NewStyle().MarginLeft(2).Render(conn + left + mid + jobText + jobKeys + right)
}

// renderConnIndicator renders the colored ●-prefixed connection state
// chip for the start of the footer. Pulled out of renderFooter so
// main.go can build it from its connState/nextRetryIn fields without
// the layout file needing to know the connState enum.
func renderConnIndicator(label string, ok bool, warn bool) string {
	style := lipgloss.NewStyle().Foreground(colorOk).Bold(true)
	if warn {
		style = style.Foreground(colorAccentWarm)
	}
	if !ok && !warn {
		style = style.Foreground(colorErr)
	}
	return style.Render("● " + label)
}

// jobControlHints returns the state-conditional p/r/c hint segment
// for the footer. Empty when no job is active so the footer stays
// uncluttered during standby.
func jobControlHints(job printJob) string {
	switch job.State {
	case "printing":
		return "  •  p: pause  •  c: cancel"
	case "paused":
		return "  •  r: resume  •  c: cancel"
	default:
		return ""
	}
}

// renderConfirmation displays a y/n prompt as a banner between the
// body and the footer. Single source of truth for confirmation UI —
// every action that arms m.confirmation shows up here, no per-pane
// rendering.
//
// danger renders the prompt as a red block — used for E-stop and for
// restarts while a print is active.
func renderConfirmation(prompt string, danger bool) string {
	q := footerFocusStyle.Foreground(colorAccentWarm).Render("? " + prompt)
	if danger {
		q = dangerPromptStyle.Render("! " + prompt)
	}
	hint := footerStyle.Render("   y to confirm   •   n / esc to cancel")
	return lipgloss.NewStyle().MarginLeft(2).Render(q + hint)
}

// renderJobSummary renders the printer-state summary shown in the
// footer. Only the "printing" state embeds the live progress bar +
// percentage + ETA; every other state is a plain status string,
// matching Phase 8's "footer text state is enough" treatment.
//
// The bar widget itself lives on the model (it has animation state),
// so the caller renders its View() and passes the result here as
// barView — that way layout.go stays free of the bubbles/progress
// dependency.
func renderJobSummary(job printJob, barView string) string {
	bullet := footerStyle.Render("  •  ")
	switch job.State {
	case "printing":
		pct := fmt.Sprintf("%d%%", int(job.Progress*100+0.5))
		eta := formatETA(job.Duration, job.Progress)
		head := footerStyle.Render(fmt.Sprintf("Printing: %s  ", job.Filename))
		tail := footerStyle.Render(fmt.Sprintf("  %s  ETA %s", pct, eta))
		return bullet + head + barView + tail
	case "paused":
		return bullet + footerStyle.Render(fmt.Sprintf("Paused: %s", job.Filename))
	case "complete":
		return bullet + footerStyle.Render(fmt.Sprintf("Complete: %s", job.Filename))
	case "error":
		return bullet + footerStyle.Render(fmt.Sprintf("Error: %s", job.Filename))
	case "cancelled":
		return bullet + footerStyle.Render(fmt.Sprintf("Cancelled: %s", job.Filename))
	default:
		return bullet + footerStyle.Render("Standby")
	}
}

// formatETA derives time-remaining from elapsed seconds and current
// progress (0-1). Returns "calculating…" for the first slice of a
// print where progress hasn't moved off zero — divide-by-zero guard.
func formatETA(elapsedSec, progress float64) string {
	if progress < 0.01 {
		return "calculating…"
	}
	total := elapsedSec / progress
	remaining := total - elapsedSec
	if remaining < 0 {
		remaining = 0
	}
	return formatDuration(remaining)
}

func formatDuration(secs float64) string {
	s := int(secs)
	h := s / 3600
	m := (s % 3600) / 60
	ss := s % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, ss)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, ss)
	}
	return fmt.Sprintf("%ds", ss)
}

// ---------------------------------------------------------------------------
// Minimum-size message
// ---------------------------------------------------------------------------

func renderMinimal(termW, termH int) string {
	return fmt.Sprintf(
		"\n  Terminal too small (%dx%d).\n  Need at least %dx%d.\n",
		termW, termH, minTermW, minTermH,
	)
}
