package main

import "github.com/charmbracelet/lipgloss"

// appTitle is the wordmark shown at the top of every screen.
const appTitle = "◐ Moontop"

// ---------------------------------------------------------------------------
// Color palette
//
// Centralized so future theming/configurability has a single source of
// truth. Numeric ANSI 256 codes — picked to work on dark terminals.
// ---------------------------------------------------------------------------

var (
	colorBorderBlurred = lipgloss.Color("63")  // dim blue
	colorBorderFocused = lipgloss.Color("205") // hot pink
	colorTitle         = lipgloss.Color("229") // pale yellow
	colorTextMuted     = lipgloss.Color("241") // gray
	colorAccentWarm    = lipgloss.Color("214") // amber
	colorAccentCool    = lipgloss.Color("205") // pink (same as focus)
	colorErr           = lipgloss.Color("203") // red
	colorOk            = lipgloss.Color("78")  // green
)

// ---------------------------------------------------------------------------
// Reusable styles
//
// All panes use panelStyle/panelTitleStyle for their chrome — no more
// per-pane ad hoc borders. Focus is communicated by swapping the border
// foreground (see renderPanel in layout.go).
// ---------------------------------------------------------------------------

var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderBlurred).
			Padding(0, 1)

	panelTitleStyle = lipgloss.NewStyle().
			Foreground(colorTextMuted).
			Bold(true)

	panelTitleFocusedStyle = panelTitleStyle.
				Foreground(colorBorderFocused)

	appTitleStyle = lipgloss.NewStyle().
			Foreground(colorTitle).
			Bold(true).
			MarginLeft(2)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorTextMuted).
			MarginLeft(2)

	footerFocusStyle = lipgloss.NewStyle().
				Foreground(colorTitle).
				Bold(true)

	bodyStyle = lipgloss.NewStyle().MarginLeft(2)

	// Pane-internal accents (kept in theme.go so all colors are
	// findable in one place).
	jogPosStyle  = lipgloss.NewStyle().Foreground(colorTitle).Bold(true)
	jogPadStyle  = lipgloss.NewStyle().Foreground(colorAccentCool)
	jogStepStyle = lipgloss.NewStyle().Foreground(colorAccentWarm)
	jogHintStyle = lipgloss.NewStyle().Foreground(colorTextMuted)

	errStyle = lipgloss.NewStyle().Foreground(colorErr).Bold(true)

	// Console line styles (see classifyLine in klippy.go).
	consoleErrStyle    = lipgloss.NewStyle().Foreground(colorErr)
	consoleInfoStyle   = lipgloss.NewStyle().Foreground(colorTextMuted)
	consoleCmdStyle    = lipgloss.NewStyle().Foreground(colorAccentWarm)
	consoleNoticeStyle = lipgloss.NewStyle().Foreground(colorOk)

	// Line console (`moontop console`) prompt.
	replPromptStyle = lipgloss.NewStyle().Foreground(colorBorderFocused).Bold(true)

	// Klippy-not-ready banner and danger confirmations (E-stop,
	// restart mid-print).
	bannerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("231")).
			Background(colorErr).
			Bold(true).
			Padding(0, 1).
			MarginLeft(2)
	dangerPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("231")).
				Background(colorErr).
				Bold(true).
				Padding(0, 1)
)
