package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/moonraker"
)

// ---------------------------------------------------------------------------
// Phase 22: the Machine screen (M or F8)
//
// Host (board, OS, uptime, CPU, temperature, memory, disk, throttling,
// network), Moonraker's own usage, each MCU (chip, version, load, awake,
// frequency), and a list of services and power devices to act on.
// Stats are pushed by Moonraker (notify_proc_stat_update, about once a
// second) and applied while the screen is open; system info, disk and
// power devices are read when it opens. Every action confirms; host
// reboot and shutdown are styled as dangerous.
// ---------------------------------------------------------------------------

type machineScreen struct {
	open     bool
	loaded   bool
	cursor   int
	sys      map[string]any
	stats    map[string]any
	disk     map[string]any
	power    []moonraker.PowerDevice
	powerErr string
	err      string
}

type machineLoadedMsg struct {
	sys, stats, disk map[string]any
	power            []moonraker.PowerDevice
	err, powerErr    error
}

type machineActionMsg struct {
	what string
	err  error
}

func loadMachineCmd(c *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		var msg machineLoadedMsg
		msg.sys, msg.err = c.SystemInfo()
		msg.stats, _ = c.ProcStats()
		msg.disk, _ = c.DiskUsage()
		msg.power, msg.powerErr = c.PowerDevices()
		return msg
	}
}

func machineAction(what string, f func() error) tea.Cmd {
	return func() tea.Msg { return machineActionMsg{what: what, err: f()} }
}

func (m *model) openMachine() tea.Cmd {
	m.machine.open = true
	return loadMachineCmd(m.client)
}

func (m *model) onMachineLoaded(msg machineLoadedMsg) {
	mc := &m.machine
	mc.loaded = true
	mc.sys, mc.stats, mc.disk, mc.power = msg.sys, msg.stats, msg.disk, msg.power
	mc.err, mc.powerErr = "", ""
	if msg.err != nil {
		mc.err = msg.err.Error()
	}
	if msg.powerErr != nil && !strings.Contains(strings.ToLower(msg.powerErr.Error()), "not found") {
		mc.powerErr = msg.powerErr.Error()
	}
	mc.cursor = min(mc.cursor, max(0, len(m.machineRows())-1))
}

// --- notifications -------------------------------------------------------------

func firstParam(params []json.RawMessage) map[string]any {
	if len(params) == 0 {
		return nil
	}
	var v map[string]any
	_ = json.Unmarshal(params[0], &v)
	return v
}

func (m *model) onProcStats(params []json.RawMessage) tea.Cmd {
	if !m.machine.open {
		return nil
	}
	if m.machine.stats == nil {
		m.machine.stats = map[string]any{}
	}
	for k, v := range firstParam(params) {
		if k == "moonraker_stats" { // an object here, a list in proc_stats
			m.machine.stats[k] = []any{v}
			continue
		}
		m.machine.stats[k] = v
	}
	return nil
}

func (m *model) onServiceChanged(params []json.RawMessage) tea.Cmd {
	if m.machine.sys == nil {
		return nil
	}
	states, _ := m.machine.sys["service_state"].(map[string]any)
	if states == nil {
		states = map[string]any{}
		m.machine.sys["service_state"] = states
	}
	for k, v := range firstParam(params) {
		states[k] = v
	}
	return nil
}

func (m *model) onPowerChanged(params []json.RawMessage) tea.Cmd {
	d := firstParam(params)
	name, _ := d["device"].(string)
	status, _ := d["status"].(string)
	for i := range m.machine.power {
		if m.machine.power[i].Device == name && status != "" {
			m.machine.power[i].Status = status
		}
	}
	return nil
}

// --- reading ---------------------------------------------------------------------

func dig(v any, path ...string) any {
	for _, p := range path {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[p]
	}
	return v
}

func digF(v any, path ...string) (float64, bool) {
	f, ok := dig(v, path...).(float64)
	return f, ok
}

func digS(v any, path ...string) string {
	s, _ := dig(v, path...).(string)
	return s
}

type machineRow struct {
	service string // a systemd service, or
	power   *moonraker.PowerDevice
}

func (m *model) services() []string {
	var out []string
	for _, s := range stringList(dig(m.machine.sys, "available_services")) {
		out = append(out, s)
	}
	return out
}

func (m *model) serviceState(svc string) (active, sub string) {
	return digS(m.machine.sys, "service_state", svc, "active_state"), digS(m.machine.sys, "service_state", svc, "sub_state")
}

func (m *model) machineRows() []machineRow {
	var rows []machineRow
	for _, s := range m.services() {
		rows = append(rows, machineRow{service: s})
	}
	for i := range m.machine.power {
		rows = append(rows, machineRow{power: &m.machine.power[i]})
	}
	return rows
}

func isMCUObject(name string) bool { return name == "mcu" || strings.HasPrefix(name, "mcu ") }

// --- keys -------------------------------------------------------------------------

func (m *model) handleMachineKey(msg tea.KeyMsg) tea.Cmd {
	mc := &m.machine
	rows := m.machineRows()
	c := m.client
	switch msg.String() {
	case "esc", "q":
		mc.open = false
		return nil
	case "up", "k":
		if len(rows) > 0 {
			mc.cursor = (mc.cursor + len(rows) - 1) % len(rows)
		}
		return nil
	case "down", "j":
		if len(rows) > 0 {
			mc.cursor = (mc.cursor + 1) % len(rows)
		}
		return nil
	case "r":
		return loadMachineCmd(c)
	case "R":
		m.confirmation = &pendingConfirmation{prompt: "Reboot the printer's host computer?", danger: true,
			onYes: machineAction("reboot", c.Reboot)}
		return nil
	case "S":
		m.confirmation = &pendingConfirmation{prompt: "Shut down the printer's host computer? You'll need to power it on by hand.", danger: true,
			onYes: machineAction("shut down", c.Shutdown)}
		return nil
	}
	if key.Matches(msg, *keys.Machine) {
		mc.open = false
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	row := rows[min(mc.cursor, len(rows)-1)]
	printing := jobActive(m.job.State)
	switch {
	case row.service != "" && msg.String() == "enter":
		svc := row.service
		m.confirmation = &pendingConfirmation{prompt: "Restart " + svc + "?", danger: printing && svc != "moonraker",
			onYes: machineAction("restart "+svc, func() error { return c.ServiceAction("restart", svc) })}
	case row.service != "" && msg.String() == "x":
		svc := row.service
		action := "stop"
		if active, _ := m.serviceState(svc); active != "active" {
			action = "start"
		}
		m.confirmation = &pendingConfirmation{prompt: strings.ToUpper(action[:1]) + action[1:] + " " + svc + "?", danger: action == "stop" && printing,
			onYes: machineAction(action+" "+svc, func() error { return c.ServiceAction(action, svc) })}
	case row.power != nil && (msg.String() == "enter" || msg.String() == " "):
		d := *row.power
		if d.LockedWhilePrinting && printing {
			return m.refuse(d.Device + " is locked while printing")
		}
		action := "on"
		if d.Status == "on" {
			action = "off"
		}
		m.confirmation = &pendingConfirmation{prompt: fmt.Sprintf("Turn %s %s?", d.Device, action), danger: action == "off" && printing,
			onYes: machineAction("power "+d.Device, func() error { return c.PowerSet(d.Device, action) })}
	}
	return nil
}

// --- rendering --------------------------------------------------------------------

func bar(frac float64, width int) string {
	frac = max(0, min(1, frac))
	n := int(frac*float64(width) + 0.5)
	return "▕" + jogStepStyle.Render(strings.Repeat("█", n)) + consoleInfoStyle.Render(strings.Repeat("·", width-n)) + "▏"
}

func fmtUptime(secs float64) string {
	d := int(secs)
	switch {
	case d >= 86400:
		return fmt.Sprintf("%dd %dh", d/86400, d%86400/3600)
	case d >= 3600:
		return fmt.Sprintf("%dh %dm", d/3600, d%3600/60)
	}
	return fmt.Sprintf("%dm", d/60)
}

func fmtRate(bps float64) string {
	switch {
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", bps/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.1f KB/s", bps/(1<<10))
	}
	return fmt.Sprintf("%.0f B/s", bps)
}

// mcuLine: name, chip, version, load and awake (Mainsail's formulas).
func (m *model) mcuLine(name string) string {
	o := m.raw[name]
	chip := digS(o, "mcu_constants", "MCU")
	ver := digS(o, "mcu_version")
	s := fmt.Sprintf("%-14s %-12s %s", strings.TrimPrefix(name, "mcu "), chip, consoleInfoStyle.Render(ver))
	if avg, ok := digF(o, "last_stats", "mcu_task_avg"); ok {
		sd, _ := digF(o, "last_stats", "mcu_task_stddev")
		awake, _ := digF(o, "last_stats", "mcu_awake")
		s += fmt.Sprintf("  load %3.0f%%  awake %3.0f%%", (avg+3*sd)/0.0025*100, awake/5*100)
	}
	if f, ok := digF(o, "last_stats", "freq"); ok && f > 0 {
		s += fmt.Sprintf("  %.0f MHz", f/1e6)
	}
	return s
}

func (m model) renderMachine() string {
	mc := m.machine
	title := appTitleStyle.Render(appTitle + "  ·  Machine")
	w := m.width - 4
	label := func(s string) string { return helpGroupStyle.Render(fmt.Sprintf("%-10s", s)) }
	var lines []string
	switch {
	case !mc.loaded:
		lines = append(lines, consoleInfoStyle.Render("Loading…"))
	case mc.err != "":
		lines = append(lines, consoleErrStyle.Render("!! "+mc.err))
	}
	if mc.loaded {
		// Host.
		board := digS(mc.sys, "cpu_info", "model")
		if board == "" {
			board = digS(mc.sys, "cpu_info", "cpu_desc")
		}
		host := strings.TrimSpace(board + "  ·  " + digS(mc.sys, "distribution", "name"))
		if up, ok := digF(mc.stats, "system_uptime"); ok {
			host += "  ·  up " + fmtUptime(up)
		}
		lines = append(lines, label("Host")+strings.Trim(host, " ·"))

		cpu := ""
		if u, ok := digF(mc.stats, "system_cpu_usage", "cpu"); ok {
			cpu = fmt.Sprintf("%s %3.0f %%", bar(u/100, 12), u)
		}
		if t, ok := digF(mc.stats, "cpu_temp"); ok {
			cpu += fmt.Sprintf("   %.1f°C", t)
		}
		if n, ok := digF(mc.sys, "cpu_info", "cpu_count"); ok {
			cpu += consoleInfoStyle.Render(fmt.Sprintf("   %.0f cores", n))
		}
		lines = append(lines, label("CPU")+cpu)

		if total, ok := digF(mc.stats, "system_memory", "total"); ok && total > 0 {
			used, _ := digF(mc.stats, "system_memory", "used")
			lines = append(lines, label("Memory")+fmt.Sprintf("%s %3.0f %%   %.0f / %.0f MB", bar(used/total, 12), used/total*100, used/1024, total/1024))
		}
		if total, ok := digF(mc.disk, "total"); ok && total > 0 {
			used, _ := digF(mc.disk, "used")
			lines = append(lines, label("Disk")+fmt.Sprintf("%s %3.0f %%   %s / %s", bar(used/total, 12), used/total*100, formatBytes(int64(used)), formatBytes(int64(total))))
		}
		flags := stringList(dig(mc.stats, "throttled_state", "flags"))
		thr := consoleNoticeStyle.Render("ok")
		if len(flags) > 0 {
			thr = consoleErrStyle.Render(strings.Join(flags, ", "))
		}
		if dig(mc.stats, "throttled_state") != nil {
			lines = append(lines, label("Throttled")+thr)
		}
		if nets, ok := dig(mc.stats, "network").(map[string]any); ok {
			var parts []string
			var names []string
			for n := range nets {
				if n != "lo" {
					names = append(names, n)
				}
			}
			sort.Strings(names)
			for _, n := range names {
				if bw, ok := digF(nets, n, "bandwidth"); ok {
					parts = append(parts, fmt.Sprintf("%s %s", n, fmtRate(bw)))
				}
			}
			if len(parts) > 0 {
				lines = append(lines, label("Network")+strings.Join(parts, "   "))
			}
		}
		if list, ok := dig(mc.stats, "moonraker_stats").([]any); ok && len(list) > 0 {
			last := list[len(list)-1]
			cpu, _ := digF(last, "cpu_usage")
			mem, _ := digF(last, "memory")
			units := digS(last, "mem_units")
			conns := ""
			if n, ok := digF(mc.stats, "websocket_connections"); ok {
				conns = fmt.Sprintf("  ·  %.0f clients", n)
			}
			lines = append(lines, label("Moonraker")+fmt.Sprintf("%.0f %% CPU  ·  %.0f %s%s", cpu, mem, units, conns))
		}

		// MCUs.
		var mcus []string
		for name := range m.raw {
			if isMCUObject(name) {
				mcus = append(mcus, name)
			}
		}
		sort.Strings(mcus)
		if len(mcus) > 0 {
			lines = append(lines, "", helpGroupStyle.Render("MCUs"))
			for _, n := range mcus {
				lines = append(lines, "  "+m.mcuLine(n))
			}
		}

		// Services and power devices.
		rows := m.machineRows()
		if len(rows) > 0 {
			lines = append(lines, "", helpGroupStyle.Render("Services")+consoleInfoStyle.Render("  (enter restarts, x stops / starts)"))
		}
		inPower := false
		for i, r := range rows {
			if r.power != nil && !inPower {
				inPower = true
				lines = append(lines, "", helpGroupStyle.Render("Power devices")+consoleInfoStyle.Render("  (enter switches)"))
			}
			mark, ns := "  ", footerText
			if i == mc.cursor {
				mark, ns = setupCursorStyle.Render("▸ "), jogStepStyle
			}
			if r.service != "" {
				active, sub := m.serviceState(r.service)
				st := consoleNoticeStyle
				if active != "active" {
					st = consoleErrStyle
				}
				state := strings.TrimSpace(active + " (" + sub + ")")
				if active == "" {
					state, st = "unknown", consoleInfoStyle
				}
				lines = append(lines, mark+ns.Render(fmt.Sprintf("%-16s", r.service))+st.Render(state))
				continue
			}
			st := consoleInfoStyle
			if r.power.Status == "on" {
				st = consoleNoticeStyle
			}
			lock := ""
			if r.power.LockedWhilePrinting {
				lock = consoleInfoStyle.Render("  locked while printing")
			}
			lines = append(lines, mark+ns.Render(fmt.Sprintf("%-16s", r.power.Device))+st.Render(r.power.Status)+lock)
		}
		if mc.powerErr != "" {
			lines = append(lines, consoleErrStyle.Render("!! power devices: "+mc.powerErr))
		}
	}
	for i, l := range lines {
		lines[i] = clipWidth(l, w)
	}
	// Keep the cursor visible on short terminals: trim from the top.
	if fit := max(5, m.height-6); len(lines) > fit {
		lines = lines[len(lines)-fit:]
	}
	out := title + "\n\n" + bodyStyle.Render(strings.Join(lines, "\n")) + "\n\n"
	if m.confirmation != nil {
		out += renderPrompt(m.confirmation, m.width) + "\n"
	}
	return out + clipWidth(footerStyle.Render("↑↓ select  •  enter restart / switch  •  x stop / start  •  R reboot host  •  S shut down host  •  r refresh  •  esc close"), m.width) + "\n"
}
