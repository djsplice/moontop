package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// appTitle is the wordmark shown at the top of every screen.
const appTitle = "◐ Moontop"

// ---------------------------------------------------------------------------
// Colour schemes
//
// Every colour the UI uses comes from the current theme; applyTheme
// swaps the palette and rebuilds the shared styles below, so a scheme
// change takes effect on the next frame. Data colours (filament
// swatches, LED colours, the bed mesh's height scale) are the data's
// own and don't change with the theme.
// ---------------------------------------------------------------------------

type theme struct {
	name, about string

	borderBlurred, borderFocused lipgloss.TerminalColor
	title, muted                 lipgloss.TerminalColor
	warm, cool                   lipgloss.TerminalColor
	err, ok                      lipgloss.TerminalColor
	onDanger                     lipgloss.TerminalColor // text on an err background
	rule, selectedBg             lipgloss.TerminalColor // table header rule, selected row
	text                         lipgloss.TerminalColor // plain text; nil = the terminal's own
}

func col(s string) lipgloss.Color { return lipgloss.Color(s) }

// themes, in the order the setup screen cycles through them. The first
// is the default.
var themes = []theme{
	{name: "moontop", about: "the original: blue panes, pink focus, amber accents",
		borderBlurred: col("63"), borderFocused: col("205"), title: col("229"), muted: col("241"),
		warm: col("214"), cool: col("205"), err: col("203"), ok: col("78"), onDanger: col("231"), rule: col("240"), selectedBg: col("236")},
	{name: "phosphor", about: "all green, like a P1 phosphor monitor",
		borderBlurred: col("#1f5f2f"), borderFocused: col("#5cf08d"), title: col("#a6f7bf"), muted: col("#3f8a55"),
		warm: col("#7dff9e"), cool: col("#5cf08d"), err: col("#d4ffe0"), ok: col("#39d26a"), onDanger: col("#03140a"), rule: col("#1f5f2f"), selectedBg: col("#0d2a17"), text: col("#7fd99a")},
	{name: "amber", about: "all amber, like a P3 phosphor monitor",
		borderBlurred: col("#6b4a12"), borderFocused: col("#ffb000"), title: col("#ffd27a"), muted: col("#9a6d1c"),
		warm: col("#ffc23d"), cool: col("#ffb000"), err: col("#fff0c9"), ok: col("#e8a317"), onDanger: col("#1a1000"), rule: col("#6b4a12"), selectedBg: col("#2e2008"), text: col("#f2b84b")},
	{name: "terminal", about: "your terminal's own 16 colours",
		borderBlurred: col("8"), borderFocused: col("6"), title: col("15"), muted: col("8"),
		warm: col("3"), cool: col("6"), err: col("1"), ok: col("2"), onDanger: col("15"), rule: col("8"), selectedBg: col("0")},
	{name: "mono", about: "no colour: greys, bold and dim only",
		borderBlurred: col("240"), borderFocused: col("255"), title: col("255"), muted: col("244"),
		warm: col("252"), cool: col("255"), err: col("255"), ok: col("250"), onDanger: col("232"), rule: col("240"), selectedBg: col("237")},
	{name: "solarized", about: "Solarized Dark",
		borderBlurred: col("#073642"), borderFocused: col("#268bd2"), title: col("#eee8d5"), muted: col("#586e75"),
		warm: col("#b58900"), cool: col("#2aa198"), err: col("#dc322f"), ok: col("#859900"), onDanger: col("#fdf6e3"), rule: col("#073642"), selectedBg: col("#073642"), text: col("#93a1a1")},
	{name: "gruvbox", about: "Gruvbox Dark",
		borderBlurred: col("#504945"), borderFocused: col("#fe8019"), title: col("#fbf1c7"), muted: col("#928374"),
		warm: col("#fabd2f"), cool: col("#83a598"), err: col("#fb4934"), ok: col("#b8bb26"), onDanger: col("#1d2021"), rule: col("#504945"), selectedBg: col("#3c3836"), text: col("#ebdbb2")},
	{name: "nord", about: "Nord",
		borderBlurred: col("#4c566a"), borderFocused: col("#88c0d0"), title: col("#eceff4"), muted: col("#7b88a1"),
		warm: col("#ebcb8b"), cool: col("#81a1c1"), err: col("#bf616a"), ok: col("#a3be8c"), onDanger: col("#2e3440"), rule: col("#4c566a"), selectedBg: col("#3b4252"), text: col("#d8dee9")},
	{name: "dracula", about: "Dracula",
		borderBlurred: col("#44475a"), borderFocused: col("#bd93f9"), title: col("#f8f8f2"), muted: col("#6272a4"),
		warm: col("#ffb86c"), cool: col("#ff79c6"), err: col("#ff5555"), ok: col("#50fa7b"), onDanger: col("#282a36"), rule: col("#44475a"), selectedBg: col("#44475a"), text: col("#f8f8f2")},
	{name: "catppuccin", about: "Catppuccin Mocha",
		borderBlurred: col("#45475a"), borderFocused: col("#cba6f7"), title: col("#cdd6f4"), muted: col("#7f849c"),
		warm: col("#fab387"), cool: col("#89b4fa"), err: col("#f38ba8"), ok: col("#a6e3a1"), onDanger: col("#1e1e2e"), rule: col("#45475a"), selectedBg: col("#313244"), text: col("#cdd6f4")},
	{name: "tokyo-night", about: "Tokyo Night",
		borderBlurred: col("#3b4261"), borderFocused: col("#7aa2f7"), title: col("#c0caf5"), muted: col("#565f89"),
		warm: col("#e0af68"), cool: col("#bb9af7"), err: col("#f7768e"), ok: col("#9ece6a"), onDanger: col("#1a1b26"), rule: col("#3b4261"), selectedBg: col("#292e42"), text: col("#a9b1d6")},
	{name: "light", about: "for light terminal backgrounds",
		borderBlurred: col("#a0a8b8"), borderFocused: col("#c0306a"), title: col("#1f2430"), muted: col("#6b7280"),
		warm: col("#a15c00"), cool: col("#2a5db0"), err: col("#c62828"), ok: col("#2e7d32"), onDanger: col("#ffffff"), rule: col("#c8ccd4"), selectedBg: col("#e4e7ee")},
}

// themeByName finds a theme; unknown or empty names give the default.
func themeByName(name string) theme {
	for _, t := range themes {
		if t.name == name {
			return t
		}
	}
	return themes[0]
}

// currentTheme is the name of the applied theme.
var currentTheme string

// applyTheme sets the palette and rebuilds every shared style.
func applyTheme(name string) {
	t := themeByName(name)
	currentTheme = t.name
	colorBorderBlurred, colorBorderFocused = t.borderBlurred, t.borderFocused
	colorTitle, colorTextMuted = t.title, t.muted
	colorAccentWarm, colorAccentCool = t.warm, t.cool
	colorErr, colorOk = t.err, t.ok
	colorOnDanger, colorRule, colorSelectedBg = t.onDanger, t.rule, t.selectedBg
	colorText = t.text
	buildStyles()
}

func init() { applyTheme("") }

var (
	colorBorderBlurred, colorBorderFocused lipgloss.TerminalColor
	colorTitle, colorTextMuted             lipgloss.TerminalColor
	colorAccentWarm, colorAccentCool       lipgloss.TerminalColor
	colorErr, colorOk                      lipgloss.TerminalColor
	colorOnDanger, colorRule               lipgloss.TerminalColor
	colorSelectedBg, colorText             lipgloss.TerminalColor
)

// tintView gives unstyled text the theme's text colour: it starts each
// line in that colour and restores it after every reset, since styled
// segments end with a full SGR reset. A theme without a text colour, or
// a terminal without colour, leaves the view untouched.
func tintView(s string) string {
	if colorText == nil {
		return s
	}
	probe := lipgloss.NewStyle().Foreground(colorText).Render("x")
	i := strings.Index(probe, "x")
	if i <= 0 {
		return s
	}
	on := probe[:i]
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+on)
	s = strings.ReplaceAll(s, "\x1b[m", "\x1b[m"+on)
	return on + strings.ReplaceAll(s, "\n", "\n"+on) + "\x1b[0m"
}

// ---------------------------------------------------------------------------
// Reusable styles
//
// All panes use panelStyle/panelTitleStyle for their chrome — no more
// per-pane ad hoc borders. Focus is communicated by swapping the border
// foreground (see renderPanel in layout.go).
// ---------------------------------------------------------------------------

var (
	panelStyle, panelTitleStyle, panelTitleFocusedStyle  lipgloss.Style
	appTitleStyle, footerStyle, footerText               lipgloss.Style
	footerFocusStyle, bodyStyle                          lipgloss.Style
	jogPosStyle, jogPadStyle, jogStepStyle, jogHintStyle lipgloss.Style
	errStyle                                             lipgloss.Style
	consoleErrStyle, consoleInfoStyle                    lipgloss.Style
	consoleCmdStyle, consoleNoticeStyle                  lipgloss.Style
	helpGroupStyle, helpKeyStyle, setupCursorStyle       lipgloss.Style
	replPromptStyle, bannerStyle, dangerPromptStyle      lipgloss.Style
	connLabelStyle                                       lipgloss.Style
)

func buildStyles() {
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

	// footerText is footerStyle without its line margin, for segments
	// joined inside one footer line.
	footerText = footerStyle.UnsetMarginLeft()

	footerFocusStyle = lipgloss.NewStyle().
		Foreground(colorTitle).
		Bold(true)

	bodyStyle = lipgloss.NewStyle().MarginLeft(2)

	// Pane-internal accents.
	jogPosStyle = lipgloss.NewStyle().Foreground(colorTitle).Bold(true)
	jogPadStyle = lipgloss.NewStyle().Foreground(colorAccentCool)
	jogStepStyle = lipgloss.NewStyle().Foreground(colorAccentWarm)
	jogHintStyle = lipgloss.NewStyle().Foreground(colorTextMuted)

	errStyle = lipgloss.NewStyle().Foreground(colorErr).Bold(true)

	// Console line styles (see classifyLine in klippy.go).
	consoleErrStyle = lipgloss.NewStyle().Foreground(colorErr)
	consoleInfoStyle = lipgloss.NewStyle().Foreground(colorTextMuted)
	consoleCmdStyle = lipgloss.NewStyle().Foreground(colorAccentWarm)
	consoleNoticeStyle = lipgloss.NewStyle().Foreground(colorOk)

	// Help overlay.
	helpGroupStyle = lipgloss.NewStyle().Foreground(colorTitle).Bold(true)
	helpKeyStyle = lipgloss.NewStyle().Foreground(colorAccentWarm)

	// Setup screen cursor.
	setupCursorStyle = lipgloss.NewStyle().Foreground(colorBorderFocused).Bold(true)

	// Line console (`moontop console`) prompt.
	replPromptStyle = lipgloss.NewStyle().Foreground(colorBorderFocused).Bold(true)

	// Klippy-not-ready banner and danger confirmations (E-stop,
	// restart mid-print).
	bannerStyle = lipgloss.NewStyle().
		Foreground(colorOnDanger).
		Background(colorErr).
		Bold(true).
		Padding(0, 1).
		MarginLeft(2)
	dangerPromptStyle = lipgloss.NewStyle().
		Foreground(colorOnDanger).
		Background(colorErr).
		Bold(true).
		Padding(0, 1)

	// Connection manager labels.
	connLabelStyle = lipgloss.NewStyle().Foreground(colorTextMuted).Width(10)
}

// tableStyles are the Temperatures table's header rule and selected row.
func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorRule).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(colorTitle).
		Background(colorSelectedBg).
		Bold(false)
	return s
}

// colorHex is a palette colour as the string Bubbles' progress bar wants.
func colorHex(tc lipgloss.TerminalColor) string {
	if s, ok := tc.(lipgloss.Color); ok {
		return string(s)
	}
	return ""
}

// fileDelegate is the Files list's row renderer in the current theme.
func fileDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.NormalTitle = d.Styles.NormalTitle.Foreground(colorTitle)
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(colorTextMuted)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(colorBorderFocused).BorderForeground(colorBorderFocused)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(colorAccentCool).BorderForeground(colorBorderFocused)
	d.Styles.DimmedTitle = d.Styles.DimmedTitle.Foreground(colorTextMuted)
	d.Styles.DimmedDesc = d.Styles.DimmedDesc.Foreground(colorTextMuted)
	d.Styles.FilterMatch = d.Styles.FilterMatch.Foreground(colorAccentWarm)
	return d
}

func newProgressBar() progress.Model {
	p := progress.New(progress.WithSolidFill(colorHex(colorBorderFocused)))
	p.EmptyColor = colorHex(colorRule)
	p.ShowPercentage = false // we render percentage ourselves alongside ETA
	p.Width = 24
	return p
}

// restyle re-applies the theme to the Bubbles components that copy
// their styles when created.
func (m *model) restyle() {
	m.table.SetStyles(tableStyles())
	m.files.SetDelegate(fileDelegate())
	m.files.Styles.FilterPrompt = m.files.Styles.FilterPrompt.Foreground(colorAccentWarm)
	m.files.Styles.FilterCursor = m.files.Styles.FilterCursor.Foreground(colorBorderFocused)
	m.input.CompletionStyle = consoleInfoStyle
	m.input.PlaceholderStyle = consoleInfoStyle
	m.input.PromptStyle = lipgloss.NewStyle().Foreground(colorBorderFocused)
	w := m.progress.Width
	m.progress = newProgressBar()
	m.progress.Width = w
}

// setTheme switches the scheme and saves it.
func (m *model) setTheme(name string) tea.Cmd {
	m.cfg.Display.Theme = name
	if name == themes[0].name {
		m.cfg.Display.Theme = ""
	}
	applyTheme(name)
	m.restyle()
	return m.saveFile()
}
