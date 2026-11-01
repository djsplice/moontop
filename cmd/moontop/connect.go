package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moontop/internal/config"
	"github.com/justinh-rahb/moontop/internal/moonraker"
)

// ---------------------------------------------------------------------------
// Connection manager
//
// Started when no host was given (no -host, no $MOONRAKER_HOST), and
// when a host refuses the connection for want of credentials. It is its
// own small Bubble Tea program that runs before the dashboard or the
// line console and hands them a connected client:
//
//   host    type a host, or ↑↓ through recent ones (the last used is
//           preselected); enter connects
//   auth    the printer needs a login or an API key; enter signs in
//
// Remembered hosts and their credentials (API keys and refresh tokens,
// never passwords) live in hosts.json (internal/config/hosts.go).
// ---------------------------------------------------------------------------

type connectStage int

const (
	stageHost connectStage = iota
	stageAuth
)

type authMethod int

const (
	authLogin authMethod = iota
	authAPIKey
)

// Auth form rows.
const (
	authRowMethod = iota
	authRowUser
	authRowPass
	authRowSource
	authRowKey
)

type connectKeys struct {
	Connect, Up, Down, Next, Prev, Toggle, Forget, Default, Back, Quit key.Binding
}

var connKeys = connectKeys{
	Connect: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "connect")),
	Up:      key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑↓", "recent")),
	Down:    key.NewBinding(key.WithKeys("down", "ctrl+n")),
	Next:    key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next field")),
	Prev:    key.NewBinding(key.WithKeys("shift+tab", "up")),
	Toggle:  key.NewBinding(key.WithKeys("left", "right", " "), key.WithHelp("←→", "change")),
	Forget:  key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "forget")),
	Default: key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "connect at start")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:    key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
}

type connectModel struct {
	hosts     *config.Hosts
	hostsPath string
	envAPIKey string // $MOONRAKER_API_KEY, tried before anything saved
	now       func() time.Time

	stage   connectStage
	host    textinput.Model
	sel     int    // index into hosts.Entries, -1 when the text was typed
	target  string // normalized host being connected to
	busy    string // "connecting…", "signing in…"
	errText string
	info    string // a passing note (default set / cleared)

	// auth form
	method  authMethod
	row     int
	user    textinput.Model
	pass    textinput.Model
	key     textinput.Model
	sources []string
	source  int

	width, height int
	inline        bool // line console: render compactly, no full screen

	client *moonraker.Client // set when connected; the program then quits
	quit   bool
}

type connectedMsg struct {
	client *moonraker.Client
	creds  moonraker.Credentials
	name   string
	err    error
	access moonraker.AccessInfo
}

type loginMsg struct {
	creds moonraker.Credentials
	err   error
}

func newConnectModel(hosts *config.Hosts, hostsPath string, inline bool) connectModel {
	in := func(placeholder string) textinput.Model {
		t := textinput.New()
		t.Prompt = ""
		t.Placeholder = placeholder
		t.PlaceholderStyle = consoleInfoStyle
		t.CharLimit = 512
		t.Width = 40
		return t
	}
	m := connectModel{
		hosts:     hosts,
		hostsPath: hostsPath,
		envAPIKey: os.Getenv("MOONRAKER_API_KEY"),
		now:       time.Now,
		host:      in("printer.local:7125"),
		user:      in("username"),
		pass:      in("password"),
		key:       in("API key from Moonraker"),
		sel:       -1,
		inline:    inline,
	}
	m.pass.EchoMode = textinput.EchoPassword
	m.pass.EchoCharacter = '•'
	m.key.EchoMode = textinput.EchoPassword
	m.key.EchoCharacter = '•'
	if len(hosts.Entries) > 0 {
		m.sel = 0
		m.host.SetValue(hosts.Entries[0].Host)
		m.host.CursorEnd()
	}
	m.host.Focus()
	return m
}

func (m connectModel) Init() tea.Cmd { return textinput.Blink }

// credentialsFor is what to try first for host: $MOONRAKER_API_KEY, else
// whatever was saved.
func (m connectModel) credentialsFor(host string) moonraker.Credentials {
	if m.envAPIKey != "" {
		return moonraker.Credentials{APIKey: m.envAPIKey}
	}
	if e, ok := m.hosts.Lookup(host); ok {
		return moonraker.Credentials{APIKey: e.APIKey, Username: e.Username, RefreshToken: e.RefreshToken}
	}
	return moonraker.Credentials{}
}

// connectCmd dials host with cr. On a refusal it also fetches
// /access/info so the auth form can offer the right login sources.
func connectCmd(host string, cr moonraker.Credentials) tea.Cmd {
	return func() tea.Msg {
		c, err := moonraker.NewWithCredentials(host, cr)
		if err != nil {
			msg := connectedMsg{err: err}
			if errors.Is(err, moonraker.ErrUnauthorized) {
				msg.access, _ = moonraker.GetAccessInfo(host)
			}
			return msg
		}
		name := ""
		done := make(chan struct{})
		go func() {
			if pi, err := c.GetPrinterInfo(); err == nil {
				name = pi.Hostname
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		return connectedMsg{client: c, creds: c.Credentials(), name: name}
	}
}

func loginCmd(host, user, pass, source string) tea.Cmd {
	return func() tea.Msg {
		cr, err := moonraker.Login(host, user, pass, source)
		return loginMsg{creds: cr, err: err}
	}
}

func (m connectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case connectedMsg:
		m.busy = ""
		if msg.err != nil {
			if errors.Is(msg.err, moonraker.ErrUnauthorized) {
				return m.enterAuth(msg.access), textinput.Blink
			}
			m.errText = friendlyDialError(msg.err)
			return m, nil
		}
		m.remember(msg.creds, msg.name)
		m.client = msg.client
		return m, tea.Quit

	case loginMsg:
		if msg.err != nil {
			m.busy = ""
			if errors.Is(msg.err, moonraker.ErrUnauthorized) {
				m.errText = "wrong username or password"
			} else {
				m.errText = msg.err.Error()
			}
			return m, nil
		}
		m.busy = "connecting…"
		return m, connectCmd(m.target, msg.creds)

	case tea.KeyMsg:
		if key.Matches(msg, connKeys.Quit) {
			m.quit = true
			return m, tea.Quit
		}
		if m.busy != "" {
			return m, nil
		}
		if m.stage == stageAuth {
			return m.updateAuth(msg)
		}
		return m.updateHost(msg)
	}
	return m, nil
}

func (m connectModel) updateHost(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.hosts.Entries)
	switch {
	case key.Matches(msg, connKeys.Back):
		m.quit = true
		return m, tea.Quit
	case key.Matches(msg, connKeys.Connect):
		h := moonraker.NormalizeHost(m.host.Value())
		if h == "" {
			m.errText = "type the printer's address, e.g. voron.local or 192.168.1.50:7125"
			return m, nil
		}
		m.target, m.errText, m.busy = h, "", "connecting to "+h+"…"
		return m, connectCmd(h, m.credentialsFor(h))
	case key.Matches(msg, connKeys.Up) && n > 0:
		if m.sel <= 0 {
			m.sel = n - 1
		} else {
			m.sel--
		}
		m.pick()
		return m, nil
	case key.Matches(msg, connKeys.Down) && n > 0:
		m.sel = (m.sel + 1) % n
		m.pick()
		return m, nil
	case key.Matches(msg, connKeys.Default) && m.sel >= 0 && m.sel < n:
		h := m.hosts.Entries[m.sel].Host
		m.hosts.ToggleDefault(h)
		m.save()
		if m.hosts.IsDefault(h) {
			m.errText = ""
			m.info = h + " is the default: Moontop connects to it at start (P in the dashboard comes back here)"
		} else {
			m.info = "no default: Moontop starts here"
		}
		return m, nil
	case key.Matches(msg, connKeys.Forget) && m.sel >= 0 && m.sel < n:
		m.hosts.Forget(m.hosts.Entries[m.sel].Host)
		m.save()
		if m.sel >= len(m.hosts.Entries) {
			m.sel = len(m.hosts.Entries) - 1
		}
		if m.sel >= 0 {
			m.pick()
		} else {
			m.host.SetValue("")
		}
		return m, nil
	}
	var cmd tea.Cmd
	before := m.host.Value()
	m.host, cmd = m.host.Update(msg)
	if m.host.Value() != before {
		m.sel, m.errText = -1, ""
	}
	return m, cmd
}

func (m *connectModel) pick() {
	m.host.SetValue(m.hosts.Entries[m.sel].Host)
	m.host.CursorEnd()
	m.errText = ""
}

// enterAuth switches to the sign-in form. A saved username is filled
// in; a stale saved credential is reported.
func (m connectModel) enterAuth(info moonraker.AccessInfo) connectModel {
	tried := m.credentialsFor(m.target)
	m.stage = stageAuth
	m.sources = info.AvailableSources
	m.source = 0
	for i, s := range m.sources {
		if s == info.DefaultSource {
			m.source = i
		}
	}
	switch {
	case tried.APIKey != "":
		m.errText = "the API key was refused"
		m.method = authAPIKey
	case tried.RefreshToken != "":
		m.errText = "your saved login has expired; sign in again"
		m.method = authLogin
	default:
		m.errText = ""
		m.method = authLogin
	}
	if e, ok := m.hosts.Lookup(m.target); ok && e.Username != "" && m.user.Value() == "" {
		m.user.SetValue(e.Username)
	}
	m.row = m.firstField()
	m.focusRow()
	return m
}

func (m connectModel) rows() []int {
	if m.method == authAPIKey {
		return []int{authRowMethod, authRowKey}
	}
	r := []int{authRowMethod, authRowUser, authRowPass}
	if len(m.sources) > 1 {
		r = append(r, authRowSource)
	}
	return r
}

func (m connectModel) firstField() int {
	if m.method == authAPIKey {
		return authRowKey
	}
	if m.user.Value() != "" {
		return authRowPass
	}
	return authRowUser
}

func (m *connectModel) focusRow() {
	m.user.Blur()
	m.pass.Blur()
	m.key.Blur()
	switch m.row {
	case authRowUser:
		m.user.Focus()
	case authRowPass:
		m.pass.Focus()
	case authRowKey:
		m.key.Focus()
	}
}

func (m connectModel) updateAuth(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.rows()
	at := 0
	for i, r := range rows {
		if r == m.row {
			at = i
		}
	}
	switch {
	case key.Matches(msg, connKeys.Back):
		m.stage, m.errText = stageHost, ""
		m.pass.SetValue("")
		m.host.Focus()
		return m, nil
	case key.Matches(msg, connKeys.Connect):
		return m.submitAuth()
	case key.Matches(msg, connKeys.Next):
		m.row = rows[(at+1)%len(rows)]
		m.focusRow()
		return m, nil
	case key.Matches(msg, connKeys.Prev):
		m.row = rows[(at-1+len(rows))%len(rows)]
		m.focusRow()
		return m, nil
	case (m.row == authRowMethod || m.row == authRowSource) && key.Matches(msg, connKeys.Toggle):
		if m.row == authRowMethod {
			m.method = 1 - m.method
			m.errText = ""
		} else if len(m.sources) > 0 {
			m.source = (m.source + 1) % len(m.sources)
		}
		return m, nil
	}
	var cmd tea.Cmd
	switch m.row {
	case authRowUser:
		m.user, cmd = m.user.Update(msg)
	case authRowPass:
		m.pass, cmd = m.pass.Update(msg)
	case authRowKey:
		m.key, cmd = m.key.Update(msg)
	}
	return m, cmd
}

func (m connectModel) submitAuth() (tea.Model, tea.Cmd) {
	if m.method == authAPIKey {
		k := strings.TrimSpace(m.key.Value())
		if k == "" {
			m.errText = "paste the printer's Moonraker API key"
			return m, nil
		}
		m.errText, m.busy = "", "connecting…"
		return m, connectCmd(m.target, moonraker.Credentials{APIKey: k})
	}
	u := strings.TrimSpace(m.user.Value())
	if u == "" || m.pass.Value() == "" {
		m.errText = "enter a username and password"
		return m, nil
	}
	src := ""
	if m.source < len(m.sources) {
		src = m.sources[m.source]
	}
	m.errText, m.busy = "", "signing in…"
	return m, loginCmd(m.target, u, m.pass.Value(), src)
}

// remember saves the host on top of the history, with credentials worth
// keeping: an API key, or a login's username and refresh token. An API
// key that came from the environment isn't written to disk.
func (m *connectModel) remember(cr moonraker.Credentials, name string) {
	e := config.HostEntry{Host: m.target, Name: name}
	if cr.APIKey != "" && cr.APIKey != m.envAPIKey {
		e.APIKey = cr.APIKey
	}
	if cr.RefreshToken != "" {
		e.Username, e.RefreshToken = cr.Username, cr.RefreshToken
	}
	m.hosts.Touch(e, m.now())
	m.save()
}

func (m *connectModel) save() {
	if m.hostsPath == "" {
		return
	}
	if err := m.hosts.Save(m.hostsPath); err != nil {
		m.errText = "couldn't save the host list: " + err.Error()
	}
}

// friendlyDialError shortens the dialer's wrapped errors.
func friendlyDialError(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"), strings.Contains(s, "actively refused"):
		return "connection refused — is Moonraker running, and is that the right port? (usually 7125)"
	case strings.Contains(s, "no such host"):
		return "no such host"
	case strings.Contains(s, "i/o timeout"), strings.Contains(s, "deadline exceeded"):
		return "timed out — the printer may be off or on another network"
	case strings.Contains(s, "bad handshake"):
		return "that address answered, but not as Moonraker (try its Moonraker port, usually 7125)"
	}
	return s
}

// ago renders a time as "just now", "5 min ago", "3 h ago", "2 days ago".
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case t.IsZero():
		return ""
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

func (m connectModel) View() string { return tintView(m.view()) }

func (m connectModel) view() string {
	if m.client != nil || m.quit {
		return ""
	}
	var b strings.Builder
	title := appTitleStyle.Render(appTitle) + footerText.Render("  ·  ") + footerFocusStyle.Render("Connect")
	if m.inline {
		title = footerFocusStyle.Render(appTitle + " · connect")
	}
	b.WriteString(title + "\n\n")
	pad := bodyStyle
	if m.inline {
		pad = lipgloss.NewStyle()
	}
	var body []string
	if m.stage == stageHost {
		body = m.viewHost()
	} else {
		body = m.viewAuth()
	}
	switch {
	case m.busy != "":
		body = append(body, "", consoleNoticeStyle.Render(m.busy))
	case m.errText != "":
		body = append(body, "", consoleErrStyle.Render(m.errText))
	case m.info != "":
		body = append(body, "", consoleInfoStyle.Render(m.info))
	}
	b.WriteString(pad.Render(strings.Join(body, "\n")))
	b.WriteString("\n\n")
	b.WriteString(footerStyle.Render(m.footer()))
	return b.String()
}

func (m connectModel) viewHost() []string {
	out := []string{
		connLabelStyle.Render("Printer") + m.host.View(),
		footerText.Render("Moonraker's address: host or IP, :port if it isn't 7125, https:// for TLS"),
	}
	if len(m.hosts.Entries) == 0 {
		return out
	}
	out = append(out, "", helpGroupStyle.Render("Recent"))
	now := m.now()
	limit := len(m.hosts.Entries)
	if m.inline && limit > 8 {
		limit = 8
	}
	for i, e := range m.hosts.Entries[:limit] {
		cur := "  "
		hostStyle := lipgloss.NewStyle()
		if i == m.sel {
			cur = setupCursorStyle.Render("▸ ")
			hostStyle = setupCursorStyle
		}
		line := cur + hostStyle.Render(fmt.Sprintf("%-28s", e.Host))
		name := e.Name
		if name == "" {
			name = "—"
		}
		line += footerText.Render(fmt.Sprintf("  %-16s %-12s", name, ago(e.LastUsed, now)))
		switch {
		case e.APIKey != "":
			line += consoleNoticeStyle.Render(" api key")
		case e.RefreshToken != "":
			line += consoleNoticeStyle.Render(" signed in as " + e.Username)
		}
		if m.hosts.IsDefault(e.Host) {
			line += jogStepStyle.Render("  [default]")
		}
		out = append(out, line)
	}
	return out
}

func (m connectModel) viewAuth() []string {
	out := []string{
		footerFocusStyle.Render(m.target) + footerText.Render(" needs you to sign in."),
		"",
	}
	mark := func(row int) string {
		if m.row == row {
			return setupCursorStyle.Render("▸ ")
		}
		return "  "
	}
	opt := func(on bool, s string) string {
		if on {
			return setupCursorStyle.Render("(•) " + s)
		}
		return footerText.Render("( ) " + s)
	}
	out = append(out, mark(authRowMethod)+connLabelStyle.Render("Sign in")+opt(m.method == authLogin, "user login")+"   "+opt(m.method == authAPIKey, "API key"))
	if m.method == authAPIKey {
		out = append(out, mark(authRowKey)+connLabelStyle.Render("API key")+m.key.View())
		out = append(out, "", footerText.Render("The key is saved for this printer (owner-only file). $MOONRAKER_API_KEY works too."))
		return out
	}
	out = append(out,
		mark(authRowUser)+connLabelStyle.Render("User")+m.user.View(),
		mark(authRowPass)+connLabelStyle.Render("Password")+m.pass.View(),
	)
	if len(m.sources) > 1 {
		out = append(out, mark(authRowSource)+connLabelStyle.Render("Source")+jogStepStyle.Render(m.sources[m.source])+footerText.Render("  ←→ change"))
	}
	out = append(out, "", footerText.Render("The password isn't saved; Moontop keeps Moonraker's refresh token, so you stay signed in."))
	return out
}

func (m connectModel) footer() string {
	sep := footerText.Render("  •  ")
	hint := func(b key.Binding) string {
		h := b.Help()
		return footerFocusStyle.Render(h.Key) + footerText.Render(" "+h.Desc)
	}
	var parts []string
	if m.stage == stageHost {
		parts = []string{hint(connKeys.Connect)}
		if len(m.hosts.Entries) > 0 {
			parts = append(parts, hint(connKeys.Up), hint(connKeys.Default), hint(connKeys.Forget))
		}
		parts = append(parts, footerFocusStyle.Render("esc")+footerText.Render(" quit"))
	} else {
		parts = []string{
			footerFocusStyle.Render("enter") + footerText.Render(" sign in"),
			hint(connKeys.Next), hint(connKeys.Toggle), hint(connKeys.Back),
		}
	}
	return strings.Join(parts, sep)
}

// ---------------------------------------------------------------------------
// Entry points
// ---------------------------------------------------------------------------

func loadHosts() (*config.Hosts, string) {
	path, err := config.DefaultHostsPath()
	if err != nil {
		return &config.Hosts{}, ""
	}
	h, err := config.LoadHosts(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moontop: %s: %v (starting a new host list)\n", path, err)
	}
	return h, path
}

// savedCredentials is what to connect to host with when it was given on
// the command line: $MOONRAKER_API_KEY, else what the host list saved.
func savedCredentials(host string) moonraker.Credentials {
	if k := os.Getenv("MOONRAKER_API_KEY"); k != "" {
		return moonraker.Credentials{APIKey: k}
	}
	h, _ := loadHosts()
	if e, ok := h.Lookup(host); ok {
		return moonraker.Credentials{APIKey: e.APIKey, Username: e.Username, RefreshToken: e.RefreshToken}
	}
	return moonraker.Credentials{}
}

// connectClient connects to host (from -host / $MOONRAKER_HOST), or to
// the default printer when useDefault is set, or runs the connection
// manager: when there's neither, when the host wants a sign-in, or when
// the default can't be reached. It returns a nil client when the user
// quit.
func connectClient(host string, inline, useDefault bool) (*moonraker.Client, string, error) {
	viaDefault := false
	if host == "" && useDefault {
		if hosts, _ := loadHosts(); hosts.Default != "" {
			host, viaDefault = hosts.Default, true
		}
	}
	var dialErr error
	if host != "" {
		host = moonraker.NormalizeHost(host)
		c, err := moonraker.NewWithCredentials(host, savedCredentials(host))
		if err == nil {
			hosts, path := loadHosts()
			e, ok := hosts.Lookup(host)
			if !ok {
				e = config.HostEntry{Host: host}
			}
			hosts.Touch(e, time.Now())
			if path != "" {
				_ = hosts.Save(path)
			}
			return c, host, nil
		}
		unauthorized := errors.Is(err, moonraker.ErrUnauthorized)
		if !isTerminal(os.Stdin) || !unauthorized && !viaDefault {
			return nil, host, err
		}
		if !unauthorized {
			dialErr = err
		}
	}
	hosts, path := loadHosts()
	m := newConnectModel(hosts, path, inline)
	opts := []tea.ProgramOption{}
	if !inline {
		opts = append(opts, tea.WithAltScreen())
	}
	switch {
	case dialErr != nil:
		// The default printer is off or away: say so, offer the rest.
		m.errText = host + " (your default): " + friendlyDialError(dialErr)
	case host != "":
		// Straight to the sign-in form for the host that refused us.
		m.host.SetValue(host)
		m.target, m.busy = host, "connecting to "+host+"…"
		p := tea.NewProgram(startWith{m, connectCmd(host, m.credentialsFor(host))}, opts...)
		return finishConnect(p.Run())
	}
	return finishConnect(tea.NewProgram(m, opts...).Run())
}

func finishConnect(final tea.Model, err error) (*moonraker.Client, string, error) {
	if err != nil {
		return nil, "", err
	}
	var m connectModel
	switch f := final.(type) {
	case connectModel:
		m = f
	case startWith:
		m = f.connectModel
	}
	if m.client == nil {
		return nil, "", nil
	}
	return m.client, m.target, nil
}

// startWith runs a connectModel with a first command (a connection
// attempt already under way).
type startWith struct {
	connectModel
	first tea.Cmd
}

func (s startWith) Init() tea.Cmd { return tea.Batch(textinput.Blink, s.first) }

func (s startWith) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := s.connectModel.Update(msg)
	s.connectModel = m.(connectModel)
	return s, cmd
}
