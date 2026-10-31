// Command tui is Moontop, a terminal UI for Moonraker / Klipper.
//
// Usage:
//
//	moontop  [-host ip:port]                 full-screen dashboard
//	moontop console [-host ip:port]          line-based gcode console
//	moontop gcode [-host ip:port] [cmd ...]  run gcode and exit
//
// -host defaults to $MOONRAKER_HOST. Build with: go build -o moontop ./cmd/tui
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moonraker-tui/internal/config"
	"github.com/justinh-rahb/moonraker-tui/internal/console"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
)

// ---------------------------------------------------------------------------
// Sensor-object prefixes we care about
// ---------------------------------------------------------------------------

var sensorPrefixes = []string{
	"extruder",
	"heater_bed",
	"heater_generic",
	"temperature_sensor",
	"temperature_fan",
}

// extruderHeater matches Klipper's extruder heaters: "extruder",
// "extruder1", "extruder2"… (one per toolhead on a toolchanger like the
// U1). It must not match "extruder_stepper …", which has no heater.
var extruderHeater = regexp.MustCompile(`^extruder\d*$`)

func isSensorObject(name string) bool {
	if extruderHeater.MatchString(name) {
		return true
	}
	for _, prefix := range sensorPrefixes {
		if name == prefix || strings.HasPrefix(name, prefix+" ") {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Message types
// ---------------------------------------------------------------------------

// initMsg carries the snapshot from a successful subscribe. Klippy is
// ready by construction (initialize only subscribes when it is).
type initMsg struct {
	sensors  map[string]*sensorState
	position [3]float64
	job      printJob
	limits   limits
	klippy   klippyStatus
	raw      rawObjects
	err      error
}

// rawObjects is the generic object store (Phase 13): every field of
// objects subscribed whole (the AFC family), merged as reported, with
// no per-field code. Views parse what they need from it on render.
type rawObjects map[string]map[string]any

// limits mirrors the kinematic-tuning fields shown in the tuning pane.
// Populated from toolhead status updates (subset that Klipper exposes)
// and edited by the user via SET_VELOCITY_LIMIT.
type limits struct {
	Velocity             float64
	Accel                float64
	SquareCornerVelocity float64
	MinCruiseRatio       float64
	// MinCruiseRatioKnown indicates whether the live status reported a
	// MinCruiseRatio value. Older Klippers don't expose it via toolhead;
	// when false we treat the field as write-only.
	MinCruiseRatioKnown bool
}

// tuningField identifies one of the four limit inputs.
type tuningField int

const (
	tuningVelocity tuningField = iota
	tuningAccel
	tuningSqCornerV
	tuningMinCruise
	tuningFieldCount
)

func (f tuningField) label() string {
	switch f {
	case tuningVelocity:
		return "Velocity"
	case tuningAccel:
		return "Acceleration"
	case tuningSqCornerV:
		return "Sq. Corner Vel"
	case tuningMinCruise:
		return "Min Cruise Ratio"
	}
	return "?"
}

// filesLoadedMsg arrives once ListFiles returns. Files is sorted with
// most-recently-modified first.
type filesLoadedMsg struct {
	files []moonraker.FileInfo
	err   error
}

// printStartedMsg arrives after a StartPrint Cmd completes. The footer
// will still wait for print_stats to confirm before showing "Printing"
// — this message just surfaces send failures.
type printStartedMsg struct {
	filename string
	err      error
}

// printJob mirrors the subset of print_stats / virtual_sdcard we care
// about for the footer / progress display.
type printJob struct {
	State    string // "standby" | "printing" | "paused" | "complete" | "error" | "cancelled"
	Filename string
	Duration float64 // seconds of active print time
	Progress float64 // 0.0 - 1.0 (from virtual_sdcard)
}

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

// ---------------------------------------------------------------------------
// Reconnection
// ---------------------------------------------------------------------------

type connState int

const (
	stateConnected connState = iota
	stateReconnecting
	stateFailed // shown after reconnectFailedThreshold attempts; we keep retrying
)

// reconnectFailedThreshold = number of consecutive failed attempts
// after which the UI switches from "reconnecting…" to a louder
// "disconnected" banner. We never actually stop retrying.
const reconnectFailedThreshold = 5

// disconnectedMsg fires once per drop of the underlying websocket.
type disconnectedMsg struct{ err error }

// reconnectResultMsg fires after each Reconnect attempt finishes.
type reconnectResultMsg struct {
	attempt int
	err     error
}

// backoffDelay returns the wait before the Nth reconnect attempt
// (1-indexed): 1s, 2s, 4s, 8s, 16s, 30s cap.
func backoffDelay(attempt int) time.Duration {
	d := 1 << (attempt - 1)
	if d > 30 {
		d = 30
	}
	return time.Duration(d) * time.Second
}

// gcodeSentMsg is emitted after a GcodeScript call completes. If err is
// non-nil, the send failed; otherwise no UI state change is needed (the
// echo and response stream handle the visible feedback).
type gcodeSentMsg struct {
	cmd string
	err error
}

// ---------------------------------------------------------------------------
// Sensor state
// ---------------------------------------------------------------------------

type sensorState struct {
	Current  float64
	Target   float64
	Power    float64 // 0–1, heaters only
	HasPower bool
}

func (s *sensorState) stateLabel() string {
	if s.Target > 0 {
		return "heating"
	}
	return "standby"
}

// ---------------------------------------------------------------------------
// Focus
// ---------------------------------------------------------------------------

type focusArea int

const (
	focusTable focusArea = iota
	focusConsole
	focusJog
	focusFiles
	focusTuning
	focusAFC
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type model struct {
	client *moonraker.Client

	sensors     map[string]*sensorState
	sensorNames []string

	// Toolhead position (X, Y, Z) as reported by the printer.
	position [3]float64

	// Jog step sizes (from settings) and the index of the active one.
	steps []float64
	// Index into steps — easier to cycle than a free float.
	stepIndex int

	// Current print job state (from print_stats).
	job printJob

	// Kinematic limits (from toolhead status, edited via tuning pane).
	limits limits

	table    table.Model
	viewport viewport.Model
	input    textinput.Model
	files    list.Model
	progress progress.Model

	// Tuning pane: one textinput per editable field, plus inner-focus
	// index and per-field dirty bits.
	tuningInputs [tuningFieldCount]textinput.Model
	tuningFocus  tuningField
	tuningDirty  [tuningFieldCount]bool
	tuningErr    string

	// Console log lines; viewport content is rebuilt from this on append.
	logLines []string

	// Console command history (in-memory only). historyIdx points to
	// the entry currently shown in the textinput while recalling;
	// historyIdx == len(history) means "not recalling, live draft."
	// historyDraft preserves whatever the user typed before pressing
	// Up, so Down past the newest entry restores it.
	history      []string
	historyIdx   int
	historyDraft string

	// Klippy state (from server.info, printer.info, the webhooks
	// object and klippy notifications). lastKlippy is what the UI last
	// reacted to — see klippyChanged.
	klippy     klippyStatus
	lastKlippy klippyStatus

	// Last initialize failure, shown on the not-ready screen. Never
	// fatal: the next klippy_ready / reconnect retries.
	initErr error

	// When non-nil, a y/n confirmation prompt is pending. Intercepts
	// global key routing — see Update(). This same slot serves any
	// confirm-gated action (start print, cancel print, future AFC
	// eject, ...) so we don't grow a parallel flag per action.
	confirmation *pendingConfirmation

	focus focusArea

	// Settings (UI-2): the file, its path ("" = don't save, e.g. it
	// failed to parse), the printer's database entry, and what's
	// derived from them by applySettings.
	cfg             config.Config
	cfgPath         string
	printerCfg      config.Printer
	mainsailPresets []preset
	shownSensors    []string // sensorNames minus hidden ones, table order
	setup           setupState

	// Generic object store (AFC), the AFC pane's cursor, and an open
	// one-line edit (lane tool, material, …), if any.
	raw       rawObjects
	afcCursor int
	lineEdit  *lineEdit

	// Commands the printer advertises (printer.gcode.help), uppercase;
	// nil until loaded. AFC-Lite lacks some AFC commands.
	gcodeCommands map[string]bool

	// Temperatures columns actually shown: the configured ones that fit.
	tempCols []string

	// Gcode files as loaded, so a sort or column change can rebuild
	// the list without asking Moonraker again.
	fileInfos []moonraker.FileInfo

	// Heater control (Phase 15): the object whose target is being
	// edited in place ("" when not editing), its input, and presets
	// read from Mainsail's settings.
	tempEdit      string
	tempInput     textinput.Model
	presets       []preset
	cooldownGcode string

	// Help overlay (UI-1): open flag and scroll offset.
	showHelp   bool
	helpScroll int

	// Console filters. showFiltered reveals hidden lines (ctrl+o);
	// hiddenCount is how many the current render hid.
	filters      console.FilterSet
	showFiltered bool
	hiddenCount  int

	width  int
	height int

	// Computed once per resize (see resizeLayout), consumed by View.
	layout layout

	// Connection lifecycle.
	connState        connState
	reconnectAttempt int
	nextRetryIn      time.Duration // for footer display while reconnecting

	ready bool
	err   error
}

// fileItem adapts a moonraker.FileInfo for bubbles/list.
type fileItem struct {
	info moonraker.FileInfo
	cols []string // "size", "modified"
}

func (f fileItem) Title() string { return f.info.Path }
func (f fileItem) Description() string {
	var parts []string
	for _, c := range f.cols {
		switch c {
		case "size":
			parts = append(parts, formatBytes(f.info.Size))
		case "modified":
			parts = append(parts, time.Unix(int64(f.info.ModifiedTime), 0).Format("2006-01-02 15:04"))
		}
	}
	return strings.Join(parts, "  ·  ")
}

// rebuildFiles fills the Files list from fileInfos with the configured
// sort and columns, keeping the selected file selected.
func (m *model) rebuildFiles() {
	files := append([]moonraker.FileInfo(nil), m.fileInfos...)
	if m.cfg.Columns.SortByName() {
		sort.SliceStable(files, func(i, j int) bool { return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path) })
	} else {
		sort.SliceStable(files, func(i, j int) bool { return files[i].ModifiedTime > files[j].ModifiedTime })
	}
	var selected string
	if it, ok := m.files.SelectedItem().(fileItem); ok {
		selected = it.info.Path
	}
	cols := m.cfg.Columns.FileCols()
	items := make([]list.Item, len(files))
	idx := 0
	for i, f := range files {
		items[i] = fileItem{info: f, cols: cols}
		if f.Path == selected {
			idx = i
		}
	}
	m.files.SetItems(items)
	m.files.Select(idx)
}
func (f fileItem) FilterValue() string { return f.info.Path }

// formatBytes renders a byte count in a human-friendly unit.
func formatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func initialModel(client *moonraker.Client) model {
	ti := textinput.New()
	ti.Placeholder = "Type a gcode command (e.g. M115, G28) and press Enter"
	ti.Prompt = "> "
	ti.CharLimit = 256

	vp := viewport.New(40, 10)

	files := list.New(nil, list.NewDefaultDelegate(), 40, 10)
	files.Title = "" // we render our own pane title
	files.SetShowTitle(false)
	files.SetShowStatusBar(false)
	files.SetShowHelp(false)
	files.SetFilteringEnabled(true)

	prog := progress.New(progress.WithDefaultGradient())
	prog.ShowPercentage = false // we render percentage ourselves alongside ETA
	prog.Width = 24

	var tuningInputs [tuningFieldCount]textinput.Model
	for i := range tuningInputs {
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 12
		ti.Width = 10
		tuningInputs[i] = ti
	}

	m := model{
		client:       client,
		sensors:      make(map[string]*sensorState),
		viewport:     vp,
		input:        ti,
		files:        files,
		progress:     prog,
		tuningInputs: tuningInputs,
		focus:        focusFiles,
		filters:      console.DefaultFilters,
		tempInput:    newTempInput(),
		stepIndex:    -1, // applySettings picks the step nearest 10 mm
		job:          printJob{State: "standby"},
		connState:    stateConnected,
	}
	m.applySettings()
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.initialize(),
		m.listenNotifications(),
		m.listenDisconnect(),
		m.loadFiles(),
		m.loadGcodeStore(),
	)
}

// listenDisconnect blocks on one disconnect signal, then is re-issued
// after a successful Reconnect. The Client's disconnected channel
// survives reconnects (it's created once in New), so the next drop
// re-arms naturally.
func (m model) listenDisconnect() tea.Cmd {
	return listenDisconnectCmd(m.client)
}

// attemptReconnect schedules one Reconnect attempt after `delay`. The
// caller increments `attempt` for each retry. The TUI keeps calling
// this with longer delays via backoffDelay until it succeeds — there
// is no permanent give-up state, only a visible "failed" banner once
// reconnectFailedThreshold is crossed.
func (m model) attemptReconnect(attempt int, delay time.Duration) tea.Cmd {
	return attemptReconnectCmd(m.client, attempt, delay)
}

// loadFiles fetches the gcode file list once at startup.
func (m model) loadFiles() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		files, err := client.ListFiles()
		if err != nil {
			return filesLoadedMsg{err: err}
		}
		// Most recently modified first — matches Mainsail's default.
		sort.Slice(files, func(i, j int) bool {
			return files[i].ModifiedTime > files[j].ModifiedTime
		})
		return filesLoadedMsg{files: files}
	}
}

// startPrint asks Moonraker to begin printing the named file. The job
// state will still go through print_stats, not optimistic updates.
func (m model) startPrint(filename string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		return printStartedMsg{filename: filename, err: client.StartPrint(filename)}
	}
}

// pendingConfirmation represents an action waiting for y/n approval.
// One slot on the model, reused for every confirm-gated action.
type pendingConfirmation struct {
	prompt string  // human-readable, rendered above the footer
	onYes  tea.Cmd // executed if the user presses y
	danger bool    // render as a red block (E-stop, restart mid-print)

	// options turns the y/n prompt into a numbered choice (the preset
	// picker): pressing an option's key runs it; esc cancels. This is
	// the first step toward Phase 19's modal layer.
	options []promptOption
}

// promptOption is one numbered choice on the prompt line.
type promptOption struct {
	key   string
	label string
	run   func(m *model) tea.Cmd
}

// jobControlMsg is emitted after a pause/resume/cancel call completes.
// Like printStartedMsg, it only surfaces send failures — the visible
// state change waits for the next print_stats update.
type jobControlMsg struct {
	action string
	err    error
}

func (m model) pausePrint() tea.Cmd {
	client := m.client
	return func() tea.Msg { return jobControlMsg{"pause", client.PausePrint()} }
}

func (m model) resumePrint() tea.Cmd {
	client := m.client
	return func() tea.Msg { return jobControlMsg{"resume", client.ResumePrint()} }
}

func (m model) cancelPrint() tea.Cmd {
	client := m.client
	return func() tea.Msg { return jobControlMsg{"cancel", client.CancelPrint()} }
}

// jobActive returns true while a print is either running or paused —
// i.e. while pause/resume/cancel are meaningful actions.
func jobActive(state string) bool {
	return state == "printing" || state == "paused"
}

// connected reports whether the websocket is currently up.
func (m *model) connected() bool { return m.connState == stateConnected }

// requireConnected gates a send. Returns the Cmd to run if we're
// online; otherwise logs an inline note explaining what was dropped
// and returns nil. Centralizing this keeps the "disconnected →
// drop send + warn" behavior consistent across every send site.
func (m *model) requireConnected(what string, cmd tea.Cmd) tea.Cmd {
	if m.connected() {
		return cmd
	}
	m.appendLog("!! disconnected — " + what + " not sent")
	return nil
}

// initialize is the single (re)subscribe path, run at startup, after a
// websocket reconnect, and on notify_klippy_ready:
//
//  1. identify this connection (best effort),
//  2. ask server.info whether Klippy is ready — if not, report its state
//     and stop; notify_klippy_ready will run this again,
//  3. list objects and send ONE complete subscribe map (Moonraker
//     replaces, not merges, a connection's subscription).
func (m model) initialize() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ks := identifyAndCheckKlippy(client)
		if ks.err != nil {
			return initMsg{err: ks.err}
		}
		if ks.state != "ready" {
			return ks
		}

		objects, err := client.ListObjects()
		if err != nil {
			return initMsg{err: fmt.Errorf("list objects: %w", err)}
		}

		subMap := make(map[string][]string)
		for _, name := range objects {
			if isSensorObject(name) {
				// power only exists on heaters; Moonraker simply omits
				// fields an object doesn't have.
				subMap[name] = []string{"temperature", "target", "power"}
			}
		}

		if len(subMap) == 0 {
			return initMsg{err: fmt.Errorf("no heater/sensor objects found")}
		}

		subMap["toolhead"] = []string{
			"position",
			"max_velocity", "max_accel",
			"square_corner_velocity", "minimum_cruise_ratio",
		}
		subMap["print_stats"] = []string{"state", "filename", "print_duration"}
		subMap["virtual_sdcard"] = []string{"progress"}
		// webhooks carries Klippy's own state + message, so a
		// shutdown (E-stop, MCU error) shows up in the same diff
		// stream as everything else.
		subMap["webhooks"] = []string{"state", "state_message"}
		// AFC (Armored Turtle's AFC-Klipper-Add-On): every object, every
		// field — the set varies by unit type and version, and the AFC
		// pane reads it the way Fluidd does.
		for _, name := range objects {
			if isAFCObject(name) {
				subMap[name] = nil
			}
		}

		initial, err := client.Subscribe(subMap)
		if err != nil {
			return initMsg{err: fmt.Errorf("subscribe: %w", err)}
		}

		sensors := make(map[string]*sensorState, len(subMap))
		for name := range subMap {
			if isSensorObject(name) {
				sensors[name] = &sensorState{}
			}
		}
		var position [3]float64
		job := printJob{State: "standby"}
		var lim limits
		klippy := klippyStatus{State: "ready"}
		raw := rawObjects{}
		mergeStatus(sensors, &position, &job, &lim, &klippy, raw, initial.Objects)

		return initMsg{sensors: sensors, position: position, job: job, limits: lim, klippy: klippy, raw: raw}
	}
}

// sendGcode wraps a Client.GcodeScript call as a Cmd so the Update loop
// doesn't block on the round-trip.
func (m model) sendGcode(cmd string) tea.Cmd {
	return sendGcodeCmd(m.client, cmd)
}

// ---------------------------------------------------------------------------
// Delta merge
// ---------------------------------------------------------------------------

// mergeStatus applies a Moonraker status diff. One pass over the diff
// dispatches each object to a per-type handler — sensors map updates,
// toolhead position array, and (later) whatever other objects we add.
// New object types are a single new case here, not a separate merge fn.
func mergeStatus(
	sensors map[string]*sensorState,
	position *[3]float64,
	job *printJob,
	lim *limits,
	klippy *klippyStatus,
	raw rawObjects,
	objects map[string]map[string]any,
) {
	for name, fields := range objects {
		switch {
		case isAFCObject(name):
			// Generic: store every reported field as-is.
			obj := raw[name]
			if obj == nil {
				obj = map[string]any{}
				raw[name] = obj
			}
			for k, v := range fields {
				obj[k] = v
			}
		case name == "webhooks":
			if v, ok := fields["state"]; ok {
				if s, ok := v.(string); ok {
					klippy.State = s
				}
			}
			if v, ok := fields["state_message"]; ok {
				if s, ok := v.(string); ok {
					klippy.Message = s
				}
			}
		case name == "toolhead":
			if v, ok := fields["position"]; ok {
				if arr, ok := v.([]any); ok {
					for i := 0; i < 3 && i < len(arr); i++ {
						if f, ok := arr[i].(float64); ok {
							position[i] = f
						}
					}
				}
			}
			if v, ok := fields["max_velocity"]; ok {
				if f, ok := v.(float64); ok {
					lim.Velocity = f
				}
			}
			if v, ok := fields["max_accel"]; ok {
				if f, ok := v.(float64); ok {
					lim.Accel = f
				}
			}
			if v, ok := fields["square_corner_velocity"]; ok {
				if f, ok := v.(float64); ok {
					lim.SquareCornerVelocity = f
				}
			}
			if v, ok := fields["minimum_cruise_ratio"]; ok {
				if f, ok := v.(float64); ok {
					lim.MinCruiseRatio = f
					lim.MinCruiseRatioKnown = true
				}
			}
		case name == "print_stats":
			if v, ok := fields["state"]; ok {
				if s, ok := v.(string); ok {
					job.State = s
				}
			}
			if v, ok := fields["filename"]; ok {
				if s, ok := v.(string); ok {
					job.Filename = s
				}
			}
			if v, ok := fields["print_duration"]; ok {
				if f, ok := v.(float64); ok {
					job.Duration = f
				}
			}
		case name == "virtual_sdcard":
			if v, ok := fields["progress"]; ok {
				if f, ok := v.(float64); ok {
					job.Progress = f
				}
			}
		default:
			s, ok := sensors[name]
			if !ok {
				continue
			}
			if v, ok := fields["temperature"]; ok {
				if f, ok := v.(float64); ok {
					s.Current = f
				}
			}
			if v, ok := fields["target"]; ok {
				if f, ok := v.(float64); ok {
					s.Target = f
				}
			}
			if v, ok := fields["power"]; ok {
				if f, ok := v.(float64); ok {
					s.Power, s.HasPower = f, true
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.ready {
			m.resizeLayout()
		}

	case initMsg:
		if msg.err != nil {
			m.initErr = msg.err
			m.appendLog(fmt.Sprintf("!! init: %v", msg.err))
			return m, nil
		}
		m.initErr = nil
		m.klippy = msg.klippy
		m.sensors = msg.sensors
		m.sensorNames = sortedKeys(msg.sensors)
		m.raw = msg.raw
		m.applySettings()
		m.position = msg.position
		m.job = msg.job
		m.limits = msg.limits
		m.ready = true
		m.buildTable()
		m.setFocus(m.focus) // the rebuilt table starts unfocused
		m.resizeLayout()
		m.rebuildRows()
		m.refreshTuningInputs()
		m.klippyChanged()
		// Presets, per-printer settings and the command list may have
		// changed with the config; re-read them on every init.
		return m, tea.Batch(loadPresetsCmd(m.client), loadPrinterCfgCmd(m.client), loadGcodeHelpCmd(m.client))

	case gcodeHelpMsg:
		if msg.err == nil {
			m.gcodeCommands = map[string]bool{}
			for _, c := range msg.commands {
				m.gcodeCommands[c] = true
			}
		}
		return m, nil

	case presetsMsg:
		if msg.err == nil {
			m.mainsailPresets = msg.presets
			m.cooldownGcode = msg.cooldown
			m.applySettings()
		}
		return m, nil

	case printerCfgMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("!! printer settings: %v", msg.err))
			return m, nil
		}
		m.printerCfg = msg.cfg
		m.applySettings()
		m.rebuildRows()
		return m, nil

	case settingsSavedMsg:
		if msg.err != nil {
			m.setup.status = "!! " + msg.err.Error()
			m.appendLog("!! saving settings: " + msg.err.Error())
		} else {
			m.setup.status = "saved to " + msg.where
		}
		return m, nil

	case klippyStateMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("!! printer info: %v", msg.err))
			return m, nil
		}
		m.setKlippy(msg.state, msg.message)
		return m, nil

	case notificationMsg:
		return m, m.handleNotification(msg)

	case klippyActionMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("!! %s: %v", msg.action, msg.err))
		}
		return m, nil

	case gcodeStoreMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("!! console history: %v", msg.err))
			return m, nil
		}
		// Prepend: live lines may already have arrived while the
		// request was in flight.
		lines := console.BackfillLines(msg.entries)
		if len(lines) > 0 {
			lines = append(lines, "** ── live ──")
			m.logLines = append(lines, m.logLines...)
			m.refreshConsole()
		}
		return m, nil

	case progress.FrameMsg:
		pm, cmd := m.progress.Update(msg)
		m.progress = pm.(progress.Model)
		return m, cmd

	case gcodeSentMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("!! send failed: %v", msg.err))
		}
		return m, nil

	case filesLoadedMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("!! load files: %v", msg.err))
			return m, nil
		}
		m.fileInfos = msg.files
		m.rebuildFiles()
		return m, nil

	case printStartedMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("!! start print %q: %v", msg.filename, msg.err))
		}
		return m, nil

	case jobControlMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("!! %s: %v", msg.action, msg.err))
		}
		return m, nil

	case tuningAppliedMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("!! set velocity limit: %v", msg.err))
		}
		return m, nil

	case disconnectedMsg:
		m.connState = stateReconnecting
		m.reconnectAttempt = 1
		m.nextRetryIn = backoffDelay(1)
		m.appendLog(fmt.Sprintf("!! disconnected: %v", msg.err))
		return m, m.attemptReconnect(1, m.nextRetryIn)

	case reconnectResultMsg:
		if msg.err == nil {
			m.connState = stateConnected
			m.reconnectAttempt = 0
			m.nextRetryIn = 0
			m.appendLog(fmt.Sprintf("** reconnected (attempt %d)", msg.attempt))
			// Klippy state is unknown until initialize asks again.
			m.klippy = klippyStatus{}
			m.klippyChanged()
			// Re-arm the disconnect watcher for the next drop, and
			// re-run initialize: the old connection's subscription
			// died with it. The notification listener is NOT
			// re-issued — its channel survives reconnect (see Client
			// doc comment) and the goroutine blocked on it simply
			// resumes once the new readLoop pushes data.
			return m, tea.Batch(m.listenDisconnect(), m.initialize())
		}
		next := msg.attempt + 1
		if next > reconnectFailedThreshold {
			m.connState = stateFailed
		}
		m.reconnectAttempt = next
		m.nextRetryIn = backoffDelay(next)
		return m, m.attemptReconnect(next, m.nextRetryIn)

	case errMsg:
		m.err = msg.err
		return m, nil
	}

	return m, nil
}

// paneOrder is the visual and Tab order; pane numbers (1–5) index it.
var paneOrder = []focusArea{focusFiles, focusJog, focusTuning, focusTable, focusConsole, focusAFC}

func paneNumber(f focusArea) int {
	for i, p := range paneOrder {
		if p == f {
			return i + 1
		}
	}
	return 0
}

// cycleFocus moves focus by delta panes (+1 = Tab, -1 = shift+Tab).
// Tab follows the on-screen order (top panes as configured, then the
// console); pane numbers stay attached to the pane, not the position.
func (m *model) cycleFocus(delta int) {
	var order []focusArea
	for _, name := range m.cfg.Panes.Ordered() {
		if f, ok := paneByName(name); ok {
			order = append(order, f)
		}
	}
	order = append(order, focusAFC, focusConsole) // bottom row: AFC, then console
	n := len(order)
	idx := 0
	for i, f := range order {
		if f == m.focus {
			idx = i
		}
	}
	hidden := m.hiddenPanes()
	for i := 1; i <= n; i++ {
		f := order[((idx+delta*i)%n+n)%n]
		if !hidden[f] {
			m.setFocus(f)
			return
		}
	}
}

// setFocus is the one place focus changes, so every widget's own
// focus state (cursor drawn, keys accepted) follows the pane focus.
// Confirmations survive focus changes intentionally.
func (m *model) setFocus(f focusArea) {
	m.focus = f
	if f == focusConsole {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
	// The table only draws its cursor row and accepts keys while
	// focused — it was never focused before UI-1.
	if f == focusTable {
		m.table.Focus()
	} else {
		m.table.Blur()
	}
	if f == focusTuning {
		m.setInnerFocus(m.tuningFocus)
	} else {
		for i := range m.tuningInputs {
			m.tuningInputs[i].Blur()
		}
	}
}

// handleFilesKey implements the files pane's key behavior. The y/n
// confirmation sub-state used to live here (Phase 7); it now lives at
// the top of Update() so any global action can reuse it. Enter just
// arms a confirmation and returns — the actual y/n handling happens
// globally.
func (m *model) handleFilesKey(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "enter" {
		if it, ok := m.files.SelectedItem().(fileItem); ok {
			name := it.info.Path
			m.confirmation = &pendingConfirmation{
				prompt: "Start print: " + name + "?",
				onYes:  m.startPrint(name),
			}
		}
		return nil
	}

	var cmd tea.Cmd
	m.files, cmd = m.files.Update(msg)
	return cmd
}

// ---------------------------------------------------------------------------
// Tuning pane
// ---------------------------------------------------------------------------

// refreshTuningInputs copies the live limit values into each textinput
// EXCEPT for fields the user is currently editing — dirty bits (touched
// since the last successful apply) and the inner-focused field are
// both protected, so live status updates never stomp on in-flight typing.
func (m *model) refreshTuningInputs() {
	vals := [tuningFieldCount]float64{
		m.limits.Velocity,
		m.limits.Accel,
		m.limits.SquareCornerVelocity,
		m.limits.MinCruiseRatio,
	}
	known := [tuningFieldCount]bool{true, true, true, m.limits.MinCruiseRatioKnown}
	for i := range m.tuningInputs {
		if m.tuningDirty[i] {
			continue
		}
		if m.focus == focusTuning && tuningField(i) == m.tuningFocus {
			continue
		}
		if !known[i] {
			continue
		}
		m.tuningInputs[i].SetValue(formatLimit(vals[i]))
	}
}

// formatLimit formats a float for display in the tuning inputs. Trims
// trailing zeros so 1500 doesn't show as "1500.000000".
func formatLimit(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// setInnerFocus moves inner focus inside the tuning pane to field idx.
// Tracks textinput Focus/Blur so the cursor blinks on exactly one field.
func (m *model) setInnerFocus(idx tuningField) {
	if idx < 0 || int(idx) >= int(tuningFieldCount) {
		return
	}
	for i := range m.tuningInputs {
		if tuningField(i) == idx {
			m.tuningInputs[i].Focus()
		} else {
			m.tuningInputs[i].Blur()
		}
	}
	m.tuningFocus = idx
}

// handleTuningKey routes keys while the tuning pane is outer-focused.
// up/down move inner focus between the four fields; Enter applies any
// dirty fields via SET_VELOCITY_LIMIT; everything else feeds the
// currently focused textinput. Outer Tab is handled upstream — it
// never reaches this function — so inner navigation can't collide with
// pane cycling.
func (m *model) handleTuningKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		next := m.tuningFocus - 1
		if next < 0 {
			next = tuningFieldCount - 1
		}
		m.setInnerFocus(next)
		return nil
	case "down", "j":
		next := m.tuningFocus + 1
		if int(next) >= int(tuningFieldCount) {
			next = 0
		}
		m.setInnerFocus(next)
		return nil
	case "enter":
		return m.applyTuning()
	}

	var cmd tea.Cmd
	m.tuningInputs[m.tuningFocus], cmd = m.tuningInputs[m.tuningFocus].Update(msg)
	// Any keystroke other than navigation marks the field dirty so
	// the next status update won't overwrite it.
	m.tuningDirty[m.tuningFocus] = true
	// Clear stale error as soon as the user starts typing again.
	m.tuningErr = ""
	return cmd
}

// applyTuning parses every dirty field, validates, and (if all parse
// cleanly) sends a single SET_VELOCITY_LIMIT with only those fields.
// Validation errors surface inline in m.tuningErr — no send happens.
func (m *model) applyTuning() tea.Cmd {
	var parsed [tuningFieldCount]*float64
	for i := range m.tuningInputs {
		if !m.tuningDirty[i] {
			continue
		}
		raw := strings.TrimSpace(m.tuningInputs[i].Value())
		if raw == "" {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			m.tuningErr = fmt.Sprintf("%s: not a number", tuningField(i).label())
			return nil
		}
		if v < 0 {
			m.tuningErr = fmt.Sprintf("%s: must be ≥ 0", tuningField(i).label())
			return nil
		}
		copied := v
		parsed[i] = &copied
	}
	// If nothing was dirty there's nothing to send.
	if parsed == ([tuningFieldCount]*float64{}) {
		m.tuningErr = "no changes to apply"
		return nil
	}
	m.tuningErr = ""
	for i := range m.tuningDirty {
		m.tuningDirty[i] = false
	}

	client := m.client
	v, a, s, c := parsed[tuningVelocity], parsed[tuningAccel], parsed[tuningSqCornerV], parsed[tuningMinCruise]
	return m.requireConnected("apply limits", func() tea.Msg {
		return tuningAppliedMsg{err: client.SetVelocityLimit(v, a, s, c)}
	})
}

// tuningAppliedMsg surfaces send errors from SET_VELOCITY_LIMIT into
// the console log; success is silent (the next toolhead status update
// will reflect the new values).
type tuningAppliedMsg struct{ err error }

// handleJobControl dispatches p/r/c global keypresses based on the
// current job state. Returns (cmd, true) when the key was a recognized
// job-control action — even if it was a no-op for the current state
// (so the key gets swallowed rather than falling through to pane
// routing). Returns (_, false) when the key isn't a job-control key
// at all.
func (m *model) handleJobControl(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, *keys.Pause):
		if m.job.State == "printing" {
			return m.requireConnected("pause", m.pausePrint()), true
		}
		return nil, true
	case key.Matches(msg, *keys.Resume):
		if m.job.State == "paused" {
			return m.requireConnected("resume", m.resumePrint()), true
		}
		return nil, true
	case key.Matches(msg, *keys.Cancel):
		if jobActive(m.job.State) {
			m.confirmation = &pendingConfirmation{
				prompt: "Cancel current print?",
				onYes:  m.cancelPrint(),
			}
		}
		return nil, true
	}
	return nil, false
}

// handleJogKey reacts to a keypress while the jog pane is focused.
// Returns a Cmd that performs the (non-blocking) GcodeScript send, or
// nil for keys that just adjust local UI state (step cycling).
func (m *model) handleJogKey(msg tea.KeyMsg) tea.Cmd {
	step := m.steps[m.stepIndex]
	switch {
	case key.Matches(msg, *keys.JogX):
		if msg.String() == "left" {
			return m.sendJog("X", -step)
		}
		return m.sendJog("X", step)
	case key.Matches(msg, *keys.JogY):
		if msg.String() == "down" {
			return m.sendJog("Y", -step)
		}
		return m.sendJog("Y", step)
	case key.Matches(msg, *keys.JogZ):
		if msg.String() == "pgdown" {
			return m.sendJog("Z", -step)
		}
		return m.sendJog("Z", step)
	case key.Matches(msg, *keys.StepDown):
		if m.stepIndex > 0 {
			m.stepIndex--
		}
	case key.Matches(msg, *keys.StepUp):
		if m.stepIndex < len(m.steps)-1 {
			m.stepIndex++
		}
	case key.Matches(msg, *keys.HomeAll):
		return m.requireConnected("home", m.sendGcode("G28"))
	case key.Matches(msg, *keys.HomeX):
		return m.requireConnected("home X", m.sendGcode("G28 X"))
	case key.Matches(msg, *keys.HomeY):
		return m.requireConnected("home Y", m.sendGcode("G28 Y"))
	case key.Matches(msg, *keys.HomeZ):
		return m.requireConnected("home Z", m.sendGcode("G28 Z"))
	case key.Matches(msg, *keys.MotorsOff):
		if ok, why := whileIdle(m); !ok {
			m.appendLog("!! motors off only works " + why)
			return nil
		}
		return m.requireConnected("motors off", m.sendGcode("M84"))
	}
	return nil
}

// sendJog emits a relative move on a single axis, wrapping it in
// SAVE/RESTORE_GCODE_STATE so we leave the printer in absolute mode
// regardless of what it was in before.
func (m *model) sendJog(axis string, delta float64) tea.Cmd {
	feed := m.cfg.Toolhead.XY()
	if axis == "Z" {
		feed = m.cfg.Toolhead.Z()
	}
	script := strings.Join([]string{
		"SAVE_GCODE_STATE NAME=_ui_movement",
		"G91",
		fmt.Sprintf("G1 %s%g F%d", axis, delta, feed),
		"RESTORE_GCODE_STATE NAME=_ui_movement",
	}, "\n")
	return m.requireConnected("jog", m.sendGcode(script))
}

func (m *model) submitCommand() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	// Reset recall state for the next prompt.
	m.historyIdx = len(m.history)
	m.historyDraft = ""
	if text == "" {
		return nil
	}
	// Append to history unless it duplicates the immediately preceding
	// entry. Skip dedup of older matches — that's a different feature.
	if len(m.history) == 0 || m.history[len(m.history)-1] != text {
		m.history = append(m.history, text)
	}
	m.historyIdx = len(m.history)
	m.appendLog(">>> " + text)
	return m.requireConnected("command", m.sendGcode(text))
}

// handleConsoleKey routes a key while the console pane has focus.
// Up/Down navigate command history; Enter submits; everything else is
// passed through to the textinput (which handles the actual editing).
//
// Recall-state convention:
//   - historyIdx == len(history)  → not recalling; textinput holds the
//     user's live draft.
//   - 0 <= historyIdx < len(history) → recalling; textinput holds
//     history[historyIdx]. historyDraft preserves the pre-recall draft.
func (m *model) handleConsoleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		return m.submitCommand()

	case "up":
		if len(m.history) == 0 {
			return nil
		}
		if m.historyIdx == len(m.history) {
			// First Up of a fresh recall — snapshot the live draft
			// so Down can later restore it.
			m.historyDraft = m.input.Value()
		}
		if m.historyIdx > 0 {
			m.historyIdx--
			m.input.SetValue(m.history[m.historyIdx])
			m.input.CursorEnd()
		}
		return nil

	case "down":
		if m.historyIdx == len(m.history) {
			// Not recalling; nothing to step toward.
			return nil
		}
		m.historyIdx++
		if m.historyIdx == len(m.history) {
			m.input.SetValue(m.historyDraft)
		} else {
			m.input.SetValue(m.history[m.historyIdx])
		}
		m.input.CursorEnd()
		return nil
	}

	// Any non-navigation key: feed it to the textinput. If we were
	// recalling, the user is now editing the recalled text — promote
	// it to a fresh draft so subsequent Up snapshots the edited value
	// instead of overwriting it with the old draft.
	if m.historyIdx != len(m.history) {
		m.historyIdx = len(m.history)
		m.historyDraft = ""
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *model) appendLog(line string) {
	// Some gcode responses come as multi-line blobs; split so the viewport
	// wraps each line cleanly.
	m.logLines = append(m.logLines, strings.Split(line, "\n")...)
	m.refreshConsole()
}

// refreshConsole re-renders the viewport from logLines, styling each
// line by kind (errors red, echoes muted, ...). logLines stay unstyled
// so classification never has to see through ANSI codes.
func (m *model) refreshConsole() {
	styled := make([]string, 0, len(m.logLines))
	m.hiddenCount = 0
	for _, l := range m.logLines {
		if !m.showFiltered && m.filters.Hides(l) {
			m.hiddenCount++
			continue
		}
		styled = append(styled, styleConsoleLine(l))
	}
	m.viewport.SetContent(strings.Join(styled, "\n"))
	m.viewport.GotoBottom()
}

// ---------------------------------------------------------------------------
// Table construction and resizing
// ---------------------------------------------------------------------------

func (m *model) buildTable() {
	columns := []table.Column{
		{Title: "Name", Width: 24},
		{Title: "State", Width: 10},
		{Title: "Current", Width: 10},
		{Title: "Target", Width: 10},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(false),
	)
	// Matches the four columns above until setTableColumns resizes;
	// rows must always have as many cells as there are columns.
	m.tempCols = []string{"state", "current", "target"}

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("236")).
		Bold(false)
	t.SetStyles(s)

	m.table = t
}

// resizeLayout recomputes pane dimensions from the current window size
// and applies them to the sub-models that need explicit sizing
// (bubbles/table columns + height, viewport, textinput). Pure layout
// math lives in computeLayout (layout.go); this is the imperative
// "tell each widget how big it is" step.
func (m *model) resizeLayout() {
	reserved := 0
	if m.klippy.bannerVisible() {
		reserved = 1
	}
	m.layout = computeLayout(m.layoutInput(reserved))
	if m.layout.mode == layoutMinimal {
		return
	}

	m.setTableColumns()

	// Data rows inside the table widget: outer pane height - title(1)
	// - table-header(1) - borders(2).
	rowH := m.layout.h(focusTable) - 4
	if rowH < 1 {
		rowH = 1
	}
	m.table.SetHeight(rowH)

	m.viewport.Width = m.layout.w(focusConsole) - panelInnerXReserve()
	m.viewport.Height = m.layout.consoleViewportH
	m.refreshConsole()
	m.input.Width = m.layout.inputW

	m.files.SetSize(m.layout.filesContentW, m.layout.filesContentH)
}

// setTableColumns sizes the Temperatures columns to the pane. The State
// column only restates whether Target is above zero, so it's the first
// to go when the pane is narrow (UI-3 makes the column set configurable).
// bubbles/table cells carry 1 column of padding on each side.
func (m *model) setTableColumns() {
	const minNameW = 12
	widths := map[string]int{"state": 9, "current": 9, "target": 9, "power": 6}
	titles := map[string]string{"state": "State", "current": "Current", "target": "Target", "power": "Power"}

	// The configured columns, minus whatever doesn't fit — State first
	// (it restates Target), then Power. bubbles/table pads each cell by
	// one column on each side.
	cols := m.cfg.Columns.Temps()
	need := func(cs []string) int {
		n := minNameW + 2
		for _, c := range cs {
			n += widths[c] + 2
		}
		return n
	}
	w := m.layout.tableContentW
	for _, drop := range []string{"state", "power"} {
		if need(cols) <= w {
			break
		}
		cols = without(cols, drop)
	}
	m.tempCols = cols

	nameW := w - need(cols) + minNameW
	if nameW < minNameW {
		nameW = minNameW
	}
	columns := []table.Column{{Title: "Name", Width: nameW}}
	for _, c := range cols {
		columns = append(columns, table.Column{Title: titles[c], Width: widths[c]})
	}
	// Rows must never have more cells than columns while switching.
	// Emptying the rows resets bubbles/table's cursor to -1, so keep it.
	cursor := m.table.Cursor()
	m.table.SetRows(nil)
	m.table.SetColumns(columns)
	m.rebuildRows()
	m.table.SetCursor(cursor)
	m.clampTableCursor()
}

func without(list []string, s string) []string {
	var out []string
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// clampTableCursor keeps a row selected whenever there are rows.
func (m *model) clampTableCursor() {
	n := len(m.table.Rows())
	switch c := m.table.Cursor(); {
	case n == 0:
	case c < 0:
		m.table.SetCursor(0)
	case c >= n:
		m.table.SetCursor(n - 1)
	}
}

func (m *model) rebuildRows() {
	rows := make([]table.Row, 0, len(m.shownSensors))
	for _, name := range m.shownSensors {
		s := m.sensors[name]
		targetStr := fmt.Sprintf("%.1f°C", s.Target)
		if s.Target == 0 && !hasTarget(name) {
			targetStr = "—"
		}
		if name == m.tempEdit {
			// Plain text with a bar cursor: an embedded textinput's
			// ANSI would throw off the table's column truncation.
			targetStr = "→ " + m.tempInput.Value() + "▏"
		}
		row := table.Row{m.label(name)}
		for _, c := range m.tempCols {
			switch c {
			case "state":
				row = append(row, s.stateLabel())
			case "current":
				row = append(row, fmt.Sprintf("%.1f°C", s.Current))
			case "target":
				row = append(row, targetStr)
			case "power":
				p := "—"
				if s.HasPower {
					p = fmt.Sprintf("%.0f%%", s.Power*100)
				}
				row = append(row, p)
			}
		}
		rows = append(rows, row)
	}
	m.table.SetRows(rows)
	m.clampTableCursor()
}

func hasTarget(name string) bool {
	return strings.HasPrefix(name, "extruder") ||
		name == "heater_bed" ||
		strings.HasPrefix(name, "heater_generic")
}

func friendlyName(name string) string {
	switch {
	case name == "extruder":
		return "Extruder"
	case strings.HasPrefix(name, "extruder"):
		return "Extruder " + strings.TrimPrefix(name, "extruder")
	case name == "heater_bed":
		return "Bed"
	case strings.HasPrefix(name, "heater_generic "):
		return strings.TrimPrefix(name, "heater_generic ")
	case strings.HasPrefix(name, "temperature_sensor "):
		n := strings.TrimPrefix(name, "temperature_sensor ")
		return n + " (sensor)"
	case strings.HasPrefix(name, "temperature_fan "):
		n := strings.TrimPrefix(name, "temperature_fan ")
		return n + " (fan)"
	default:
		return name
	}
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m model) View() string {
	if m.err != nil {
		return errStyle.Render(fmt.Sprintf("\n  Error: %v\n\n", m.err)) +
			footerStyle.Render("  Press ctrl+c to quit.\n")
	}
	if !m.ready {
		return m.renderNotReady()
	}
	if m.setup.open {
		return m.renderSetup()
	}
	if m.showHelp {
		return m.renderHelp()
	}
	if m.layout.mode == layoutMinimal {
		return renderMinimal(m.width, m.height)
	}

	bodies := map[focusArea]func() string{
		focusAFC:     m.renderAFC,
		focusTable:   m.table.View,
		focusJog:     m.renderJog,
		focusFiles:   m.renderFiles,
		focusTuning:  m.renderTuning,
		focusConsole: func() string { return m.viewport.View() + "\n" + m.input.View() },
	}
	pane := func(f focusArea) string {
		return renderPanel(m.paneTitle(f), bodies[f](), m.focus == f, m.layout.w(f), m.layout.h(f))
	}
	// Top panes in the configured order (hidden ones are already left
	// out of layout.order), console last unless hidden.
	var top []string
	for _, f := range m.layout.order {
		top = append(top, pane(f))
	}
	var stacked string
	switch m.layout.mode {
	case layoutWide:
		var rows []string
		if len(top) > 0 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, top...))
		}
		// Bottom row: AFC (when present) beside the console.
		var bottom []string
		for _, f := range []focusArea{focusAFC, focusConsole} {
			if !m.layout.hidden[f] {
				bottom = append(bottom, pane(f))
			}
		}
		if len(bottom) > 0 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, bottom...))
		}
		stacked = lipgloss.JoinVertical(lipgloss.Left, rows...)
	default: // stacked
		for _, f := range []focusArea{focusAFC, focusConsole} {
			if !m.layout.hidden[f] {
				top = append(top, pane(f))
			}
		}
		stacked = lipgloss.JoinVertical(lipgloss.Left, top...)
	}
	body := bodyStyle.Render(stacked)

	title := appTitleStyle.Render(appTitle)
	var barView string
	if m.job.State == "printing" {
		barView = m.progress.View()
	}
	footer := m.fitFooter(barView)

	out := title + "\n"
	if m.klippy.bannerVisible() {
		out += renderKlippyBanner(m.klippy, m.width) + "\n"
	}
	out += body + "\n"
	switch {
	case m.lineEdit != nil:
		out += renderLineEdit(m.lineEdit, m.width) + "\n"
	case m.confirmation != nil:
		out += renderPrompt(m.confirmation, m.width) + "\n"
	}
	out += footer + "\n"
	return out
}

func (m model) renderFiles() string {
	return m.files.View()
}

// renderConnView builds the connection-status chip shown at the left
// of the footer, including the next-retry hint while reconnecting.
func (m model) renderConnView() string {
	switch m.connState {
	case stateConnected:
		return renderConnIndicator("online", true, false)
	case stateReconnecting:
		return renderConnIndicator(
			fmt.Sprintf("reconnecting (#%d, next in %s)",
				m.reconnectAttempt, m.nextRetryIn.Truncate(time.Second)),
			false, true)
	default: // stateFailed — still retrying, just louder
		return renderConnIndicator(
			fmt.Sprintf("disconnected — retrying in %s", m.nextRetryIn.Truncate(time.Second)),
			false, false)
	}
}

func (m model) renderTuning() string {
	lines := make([]string, 0, int(tuningFieldCount)*2+1)
	for i := 0; i < int(tuningFieldCount); i++ {
		f := tuningField(i)
		label := jogHintStyle.Render(f.label())
		if m.focus == focusTuning && f == m.tuningFocus {
			label = jogStepStyle.Render("▸ " + f.label())
		} else {
			label = jogHintStyle.Render("  " + f.label())
		}
		lines = append(lines, label, "  "+m.tuningInputs[i].View())
	}
	if m.tuningErr != "" {
		lines = append(lines, errStyle.Render(m.tuningErr))
	}
	return strings.Join(lines, "\n")
}

// fitFooter renders the footer to fit the terminal: drop the focused
// pane's hints from the end; then go compact (tighter separators, no
// pane name) and drop hints again; then shorten the filename. Help,
// E-stop, job state and job keys always stay; everything dropped is
// still in the ? overlay.
func (m model) fitFooter(barView string) string {
	paneKeys, jobKeys := footerHints(&m)
	job := m.job
	compact := false
	join := func(items []string) string {
		if compact {
			return strings.Join(items, " · ")
		}
		return strings.Join(items, "  •  ")
	}
	for {
		f := renderFooter(paneName(m.focus), join(paneKeys), join(jobKeys), job, barView, m.renderConnView(), compact)
		over := lipgloss.Width(f) - m.width
		if m.width <= 0 || over <= 0 {
			return f
		}
		if len(paneKeys) > 0 {
			paneKeys = paneKeys[:len(paneKeys)-1]
			continue
		}
		if !compact {
			compact = true
			paneKeys, _ = footerHints(&m) // retry with hints, compactly
			continue
		}
		// Last resort: shorten the filename (never below 12 runes).
		name := []rune(job.Filename)
		keep := len(name) - over - 1
		if keep < 12 || len(name) <= 12 {
			return f
		}
		job.Filename = ellipsizeMiddle(job.Filename, keep)
	}
}

// ellipsizeMiddle shortens s to n runes with "…" in the middle, so a
// filename keeps both its start and its extension.
func ellipsizeMiddle(s string, n int) string {
	r := []rune(s)
	if len(r) <= n || n < 3 {
		return s
	}
	tail := (n - 1) / 2
	head := n - 1 - tail
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// paneName is a pane's plain name; paneTitle adds its jump number and,
// for the console, how many lines filters are hiding.
func paneName(f focusArea) string {
	switch f {
	case focusFiles:
		return "Files"
	case focusJog:
		return "Toolhead"
	case focusTuning:
		return "Tuning"
	case focusTable:
		return "Temperatures"
	case focusConsole:
		return "Console"
	case focusAFC:
		return "AFC"
	}
	return "?"
}

func (m model) paneTitle(f focusArea) string {
	t := fmt.Sprintf("%d %s", paneNumber(f), paneName(f))
	if f == focusConsole {
		switch {
		case m.showFiltered:
			t += " · showing filtered lines (ctrl+o hides)"
		case m.hiddenCount > 0:
			t += fmt.Sprintf(" · %d hidden (ctrl+o shows)", m.hiddenCount)
		}
	}
	return t
}

// renderJog produces the contents of the jog pane: an XY directional
// pad, Z controls, the active step size, and the current toolhead
// position.
func (m model) renderJog() string {
	contentW := m.layout.w(focusJog) - panelInnerXReserve()
	if contentW < 10 {
		contentW = 10
	}

	pos := jogPosStyle.Render(fmt.Sprintf(
		"X %8.2f\nY %8.2f\nZ %8.2f",
		m.position[0], m.position[1], m.position[2],
	))

	pad := jogPadStyle.Render(lipgloss.JoinVertical(lipgloss.Center,
		"  ↑  ",
		"← + →",
		"  ↓  ",
	))
	pad = lipgloss.PlaceHorizontal(contentW, lipgloss.Center, pad)

	step := jogStepStyle.Render(fmt.Sprintf("step  %g mm", m.steps[m.stepIndex]))
	step = lipgloss.PlaceHorizontal(contentW, lipgloss.Center, step)

	// Every Toolhead action is on screen with its key — the pane has
	// no hidden controls. Motors-off dims while printing.
	motors := "m motors off"
	if ok, _ := whileIdle(&m); !ok {
		motors = "m motors off (idle)"
	}
	hints := []string{
		"PgUp/Dn Z   [ ] step",
		"H home   x y z axis",
		motors,
	}
	// Narrower panes get the same keys with tighter wording, always
	// 3 lines (jogContentMin depends on it). Full text is in ? help.
	idle, _ := whileIdle(&m)
	switch {
	case contentW >= 21:
	case contentW >= 18:
		motors = "m motors off"
		if !idle {
			motors = "m off (idle)"
		}
		hints = []string{"PgUp/Dn Z [ ] step", "H home xyz axis", motors}
	default:
		motors = "m off  ? keys"
		if !idle {
			motors = "? all keys" // motors off is unavailable while printing
		}
		hints = []string{"PgUp/Dn Z  [ ]", "H home  x y z", motors}
	}
	hint := jogHintStyle.Render(strings.Join(hints, "\n"))

	return lipgloss.JoinVertical(lipgloss.Left,
		pos,
		"",
		pad,
		"",
		step,
		"",
		hint,
	)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sortedKeys(m map[string]*sensorState) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

// runDashboard starts the full-screen dashboard (the default mode).
func runDashboard(args []string) int {
	fs := flag.NewFlagSet("moontop", flag.ContinueOnError)
	host := hostFlag(fs)
	cfgFlag := configFlag(fs)
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
	}
	h, ok := resolveHost(*host)
	if !ok {
		return 2
	}
	cfg, cfgPath := loadConfig(*cfgFlag)

	client, err := moonraker.New(h)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moontop: connect to %s: %v\n", h, err)
		return 2
	}

	m := initialModel(client)
	m.cfg, m.cfgPath = cfg, cfgPath
	m.applySettings()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Printf("TUI error: %v", err)
		return 1
	}
	return 0
}
