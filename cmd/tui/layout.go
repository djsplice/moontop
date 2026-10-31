package main

import (
	"fmt"
	"strings"

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

	// order is the top-row panes in display order (stacked mode adds
	// the console last); hidden panes are left out of both.
	order  []focusArea
	hidden map[focusArea]bool

	// Outer box sizes per pane. In wide mode every top-row pane has
	// boxH == topRowH; the console's height is auto (0).
	boxW map[focusArea]int
	boxH map[focusArea]int

	topRowH int

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

// w and h return a pane's outer box size (0 = hidden / auto).
func (l layout) w(f focusArea) int { return l.boxW[f] }
func (l layout) h(f focusArea) int { return l.boxH[f] }

// paneSpec is what the solver needs to know about one pane.
type paneSpec struct {
	minW     int // narrowest usable outer width
	naturalW int // width when it doesn't share spare space
	weight   int // share of spare width; 0 = natural width only
	innerH   int // content lines it wants (title included)
}

// layoutInput is everything the solver depends on, gathered by the
// model (see resizeLayout) so computeLayout stays a pure function.
type layoutInput struct {
	termW, termH int
	reservedRows int // e.g. the Klippy banner
	order        []focusArea
	hidden       map[focusArea]bool
	weights      map[focusArea]int
	consolePct   int // 0 = automatic
	sensorCount  int
	afcLines     int // AFC pane content lines (lanes + summary)
	extLines     int // Extruder pane content lines
	outLines     int // Outputs pane content lines
	macroLines   int // Macros pane content lines
}

// contentLines is a bottom pane's content height.
func (in layoutInput) contentLines(f focusArea) int {
	switch f {
	case focusAFC:
		return in.afcLines
	case focusExtruder:
		return in.extLines
	case focusOutputs:
		return in.outLines
	case focusMacros:
		return in.macroLines
	}
	return 0
}

// The bottom row: columns beside the console, left to right, each at
// its natural width; panes in one column stack (Extruder over Outputs).
// bottomPanes is their Tab and stacking order.
var (
	bottomColumns = [][]focusArea{{focusAFC, focusMacros}, {focusExtruder, focusOutputs}}
	bottomPanes   = []focusArea{focusAFC, focusMacros, focusExtruder, focusOutputs}
)

// Bottom-row widths: AFC is 46 columns (at most a third of the row,
// at least 32); the Extruder / Outputs column 30 (at least 26). The
// console keeps at least consoleMinW, or the whole layout stacks.
const (
	extruderNaturalW = 30
	extruderMinW     = 26
	afcMinW          = 32
	consoleMinW      = 40
)

// visibleColumns lists the bottom columns with at least one pane shown,
// each trimmed to its visible panes.
func visibleColumns(hidden map[focusArea]bool) [][]focusArea {
	var cols [][]focusArea
	for _, col := range bottomColumns {
		var shown []focusArea
		for _, f := range col {
			if !hidden[f] {
				shown = append(shown, f)
			}
		}
		if len(shown) > 0 {
			cols = append(cols, shown)
		}
	}
	return cols
}

// bottomWidths gives each visible bottom column its width (keyed by
// its first pane), or ok=false when even their minimums leave the
// console too narrow.
func bottomWidths(availW int, hidden map[focusArea]bool) (map[focusArea]int, bool) {
	w := map[focusArea]int{}
	lo := map[focusArea]int{}
	used := 0
	cols := visibleColumns(hidden)
	for _, col := range cols {
		f := col[0]
		if f == focusAFC {
			w[f], lo[f] = max(afcMinW, min(46, availW/3)), afcMinW
		} else {
			w[f], lo[f] = extruderNaturalW, extruderMinW
		}
		used += w[f]
	}
	if hidden[focusConsole] {
		return w, true
	}
	// Shrink toward the minimums before giving up.
	for _, col := range cols {
		f := col[0]
		for w[f] > lo[f] && availW-used < consoleMinW {
			w[f]--
			used--
		}
	}
	return w, availW-used >= consoleMinW
}

func defaultLayoutInput(termW, termH, sensorCount int) layoutInput {
	return layoutInput{
		termW: termW, termH: termH, sensorCount: sensorCount,
		order:   []focusArea{focusFiles, focusJog, focusTuning, focusTable},
		weights: map[focusArea]int{focusFiles: 1, focusTable: 2},
	}
}

func (in layoutInput) specs() map[focusArea]paneSpec {
	return map[focusArea]paneSpec{
		focusFiles:  {minW: 24, naturalW: 32, weight: in.weights[focusFiles], innerH: 1 + 8},
		focusJog:    {minW: 18, naturalW: 26, weight: in.weights[focusJog], innerH: 1 + jogContentMin},
		focusTuning: {minW: 20, naturalW: 30, weight: in.weights[focusTuning], innerH: 1 + tuningContentLines},
		focusTable:  {minW: 30, naturalW: 46, weight: in.weights[focusTable], innerH: 2 + in.sensorCount},
	}
}

// computeLayout produces a layout for the current terminal size.
//
// Wide mode: the visible top panes in configured order share the row —
// each gets its minimum, panes with a weight split the spare width by
// weight, natural-width panes grow toward their natural width first.
// The console takes the rows below (or its configured percentage).
// When the minimums don't fit, every pane stacks full-width instead.
func computeLayout(in layoutInput) layout {
	if in.termW < minTermW || in.termH < minTermH {
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
	availW := max(1, in.termW-marginL-gutterR)
	availH := max(1, in.termH-chromeV-in.reservedRows)

	var top []focusArea
	for _, f := range in.order {
		if !in.hidden[f] {
			top = append(top, f)
		}
	}
	specs := in.specs()
	minSum := 0
	for _, f := range top {
		minSum += specs[f].minW
	}

	_, bottomFits := bottomWidths(availW, in.hidden)
	var l layout
	if in.termW < minWideW || minSum > availW || !bottomFits {
		l = computeStacked(availW, availH, top, specs, in)
	} else {
		l = computeWide(availW, availH, top, specs, in)
	}
	l.order = top
	l.hidden = in.hidden
	return l
}

func computeWide(availW, availH int, top []focusArea, specs map[focusArea]paneSpec, in layoutInput) layout {
	l := layout{mode: layoutWide, boxW: map[focusArea]int{}, boxH: map[focusArea]int{}}

	// 1. Minimums.
	spare := availW
	for _, f := range top {
		l.boxW[f] = specs[f].minW
		spare -= specs[f].minW
	}
	// 2. Natural-width panes grow toward their natural width, but no
	//    further than a fifth of the row, so on a narrow terminal the
	//    sharing panes (files, temperatures) aren't squeezed out.
	for _, f := range top {
		if specs[f].weight == 0 {
			target := max(specs[f].minW, min(specs[f].naturalW, availW/5))
			grow := max(0, min(spare, target-l.boxW[f]))
			l.boxW[f] += grow
			spare -= grow
		}
	}
	// 3. Weighted panes split the rest; with none, natural-width panes
	//    share it equally so the row is always full.
	total := 0
	for _, f := range top {
		total += specs[f].weight
	}
	var takers []focusArea
	for _, f := range top {
		if total == 0 || specs[f].weight > 0 {
			takers = append(takers, f)
		}
	}
	given := 0
	for i, f := range takers {
		share := spare / max(1, len(takers))
		if total > 0 {
			share = spare * specs[f].weight / total
		}
		if i == len(takers)-1 {
			share = spare - given // remainder to the last, so the row is exact
		}
		l.boxW[f] += share
		given += share
	}

	// Heights: the top row hugs its tallest pane unless the console has a
	// configured share; a hidden console gives the row everything.
	innerH := 0
	for _, f := range top {
		innerH = max(innerH, specs[f].innerH)
	}
	topRowH := innerH + 2
	if pct := in.consolePct; pct > 0 {
		topRowH = availH - availH*pct/100
	}
	topRowH = max(8, min(topRowH, availH-5))
	switch {
	case len(top) == 0:
		topRowH = 0
	case in.hidden[focusConsole]:
		topRowH = availH
	}
	for _, f := range top {
		l.boxH[f] = topRowH
	}
	l.topRowH = topRowH

	// Bottom row: the columns at their natural widths beside the
	// console, as tall as the space below the top row. In a column of
	// two, the first pane (AFC, Extruder) gets its content height and
	// the second (Macros, Outputs) the rest. With the console hidden, the columns
	// share the row.
	bottomH := availH - topRowH
	consoleW := availW
	widths, _ := bottomWidths(availW, in.hidden)
	cols := visibleColumns(in.hidden)
	for _, col := range cols {
		consoleW -= widths[col[0]]
	}
	if in.hidden[focusConsole] && len(cols) > 0 {
		consoleW = 0
		left := availW
		for i, col := range cols {
			wf := availW / len(cols)
			if i == len(cols)-1 {
				wf = left
			}
			widths[col[0]] = wf
			left -= wf
		}
	}
	for _, col := range cols {
		for i, f := range col {
			l.boxW[f] = widths[col[0]]
			l.boxH[f] = bottomH
			if len(col) == 2 {
				first := max(3, min(in.contentLines(col[0])+3, bottomH-5))
				if i == 0 {
					l.boxH[f] = first
				} else {
					l.boxH[f] = bottomH - first
				}
			}
		}
	}
	l.boxW[focusConsole] = consoleW
	l.consoleViewportH = max(3, bottomH-2-1-1)
	l.inputW = consoleW - panelInnerXReserve() - 2
	l.tableContentW = max(10, l.boxW[focusTable]-panelInnerXReserve())
	l.filesContentW = l.boxW[focusFiles] - panelInnerXReserve()
	l.filesContentH = topRowH - 2 - 1
	return l
}

// computeStacked: narrow terminal — every pane full-width, stacked in
// the configured order, console last.
func computeStacked(availW, availH int, top []focusArea, specs map[focusArea]paneSpec, in layoutInput) layout {
	l := layout{mode: layoutStacked, boxW: map[focusArea]int{}, boxH: map[focusArea]int{}}
	used := 0
	for _, f := range top {
		h := specs[f].innerH + 2
		if f == focusFiles {
			h = 1 + 6 + 2
		}
		l.boxW[f] = availW
		l.boxH[f] = h
		used += h
	}
	for _, f := range bottomPanes {
		if in.hidden[f] {
			continue
		}
		lines := in.contentLines(f)
		h := lines + 1 + 2 // content + title + borders
		l.boxW[f] = availW
		l.boxH[f] = h
		used += h
	}
	consoleOuter := availH - used
	if !in.hidden[focusConsole] && consoleOuter < 7 {
		consoleOuter = 7
		over := availH - used - consoleOuter
		// over is negative if we overflow — shrink files first, then table.
		for over < 0 && l.boxH[focusFiles] > 6 {
			l.boxH[focusFiles]--
			over++
		}
		for over < 0 && l.boxH[focusTable] > 5 {
			l.boxH[focusTable]--
			over++
		}
	}
	l.boxW[focusConsole] = availW
	l.consoleViewportH = max(3, consoleOuter-2-1-1)
	contentW := max(30, availW-panelInnerXReserve())
	l.inputW = availW - panelInnerXReserve() - 2
	l.tableContentW = contentW
	l.filesContentW = contentW
	l.filesContentH = l.boxH[focusFiles] - 2 - 1
	return l
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
	// Never grow past the box: a tall body would push the row apart.
	if outerH > 2 {
		if lines := strings.Split(content, "\n"); len(lines) > outerH-2 {
			content = strings.Join(lines[:outerH-2], "\n")
		}
	}
	return style.Render(content)
}

// ---------------------------------------------------------------------------
// Footer
//
// Context-sensitive: shows the active pane's keybindings, plus the
// always-on global ones.
// ---------------------------------------------------------------------------

// renderFooter draws the status line. compact uses tighter separators
// and omits the pane name (the focused pane's border already shows it);
// fitFooter switches to it when the full line doesn't fit.
func renderFooter(paneName, paneKeys, jobKeys string, job printJob, barView, connView string, compact bool) string {
	sepText := "  •  "
	if compact {
		sepText = " · "
	}
	sep := footerText.Render(sepText)
	out := connView
	if !compact {
		out += sep + footerFocusStyle.Render(paneName)
		if paneKeys != "" {
			out += footerText.Render("  " + paneKeys)
		}
	} else if paneKeys != "" {
		out += sep + footerText.Render(paneKeys)
	}
	out += sep + renderJobSummary(job, barView)
	if jobKeys != "" {
		out += sep + footerText.Render(jobKeys)
	}
	// Always visible: the way to discover everything else, and the
	// way to stop the machine.
	out += sep + footerFocusStyle.Render("?") + footerText.Render(" help") +
		sep + footerFocusStyle.Render("F2") + footerText.Render(" setup") +
		sep + footerFocusStyle.Render("ctrl+x") + footerText.Render(" e-stop")
	return lipgloss.NewStyle().MarginLeft(2).Render(out)
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

// renderConfirmation displays a y/n prompt as a banner between the
// body and the footer. Single source of truth for confirmation UI —
// every action that arms m.confirmation shows up here, no per-pane
// rendering.
//
// danger renders the prompt as a red block — used for E-stop and for
// restarts while a print is active.
// renderPrompt draws whichever prompt is pending: y/n, or numbered
// options (the preset picker).
func renderPrompt(c *pendingConfirmation, width int) string {
	if len(c.options) == 0 {
		return renderConfirmation(c.prompt, c.danger)
	}
	head := footerFocusStyle.Foreground(colorAccentWarm).Render("? " + c.prompt)
	tail := footerText.Render(fmt.Sprintf("   1–%d choose  •  esc cancel", len(c.options)))
	var items []string
	for _, o := range c.options {
		items = append(items, footerFocusStyle.Render(o.key)+" "+footerText.Render(o.label))
	}
	// One line when it fits, otherwise one option per line.
	line := head + " " + strings.Join(items, footerText.Render("  ·  ")) + tail
	if width <= 0 || lipgloss.Width(line)+2 <= width {
		return lipgloss.NewStyle().MarginLeft(2).Render(line)
	}
	return lipgloss.NewStyle().MarginLeft(2).Render(head + tail + "\n  " + strings.Join(items, "\n  "))
}

func renderConfirmation(prompt string, danger bool) string {
	q := footerFocusStyle.Foreground(colorAccentWarm).Render("? " + prompt)
	if danger {
		q = dangerPromptStyle.Render("! " + prompt)
	}
	hint := footerText.Render("   y to confirm   •   n / esc to cancel")
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
	switch job.State {
	case "printing":
		pct := fmt.Sprintf("%d%%", int(job.Progress*100+0.5))
		eta := formatETA(job.Duration, job.Progress)
		head := footerText.Render(fmt.Sprintf("Printing: %s  ", job.Filename))
		tail := footerText.Render(fmt.Sprintf("  %s  ETA %s", pct, eta))
		return head + barView + tail
	case "paused":
		return footerText.Render(fmt.Sprintf("Paused: %s", job.Filename))
	case "complete":
		return footerText.Render(fmt.Sprintf("Complete: %s", job.Filename))
	case "error":
		return footerText.Render(fmt.Sprintf("Error: %s", job.Filename))
	case "cancelled":
		return footerText.Render(fmt.Sprintf("Cancelled: %s", job.Filename))
	default:
		return footerText.Render("Standby")
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
