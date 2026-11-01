package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/moonraker/moonrakertest"
)

func machinePrinter(t *testing.T, withPower bool) *moonrakertest.Server {
	t.Helper()
	srv := fakePrinter(t)
	srv.Handle("machine.system_info", moonrakertest.Result(map[string]any{"system_info": map[string]any{
		"cpu_info":           map[string]any{"model": "Raspberry Pi 4 Model B", "cpu_count": 4.0},
		"distribution":       map[string]any{"name": "Debian GNU/Linux 12 (bookworm)"},
		"available_services": []any{"klipper", "moonraker", "crowsnest"},
		"service_state": map[string]any{
			"klipper":   map[string]any{"active_state": "active", "sub_state": "running"},
			"moonraker": map[string]any{"active_state": "active", "sub_state": "running"},
			"crowsnest": map[string]any{"active_state": "inactive", "sub_state": "dead"},
		},
	}}))
	srv.Handle("machine.proc_stats", moonrakertest.Result(map[string]any{
		"system_cpu_usage": map[string]any{"cpu": 23.0},
		"cpu_temp":         52.1,
		"system_memory":    map[string]any{"total": 4000000.0, "used": 1000000.0},
		"system_uptime":    3.0*86400 + 4*3600,
		"throttled_state":  map[string]any{"bits": 0.0, "flags": []any{}},
		"moonraker_stats":  []any{map[string]any{"cpu_usage": 3.0, "memory": 45.0, "mem_units": "MB"}},
		"network":          map[string]any{"wlan0": map[string]any{"bandwidth": 2048.0}, "lo": map[string]any{"bandwidth": 9.0}},
	}))
	srv.Handle("server.files.get_directory", moonrakertest.Result(map[string]any{"disk_usage": map[string]any{"total": 32e9, "used": 8e9, "free": 24e9}}))
	if withPower {
		srv.Handle("machine.device_power.devices", moonrakertest.Result(map[string]any{"devices": []any{
			map[string]any{"device": "printer", "status": "on", "locked_while_printing": true},
			map[string]any{"device": "lights", "status": "off"},
		}}))
	} else {
		srv.Handle("machine.device_power.devices", func(json.RawMessage) (any, error) { return nil, errors.New("Method not found") })
	}
	return srv
}

func openMachine(t *testing.T, m model) model {
	t.Helper()
	m, cmd := updateCmd(t, m, keyRunes("M"))
	if !m.machine.open || cmd == nil {
		t.Fatal("M didn't open Machine")
	}
	return update(t, m, cmd())
}

func TestMachineScreen(t *testing.T) {
	srv := machinePrinter(t, true)
	m := readyModel(t, srv)
	m.raw.merge("mcu", map[string]any{"mcu_version": "v0.12.0-200", "mcu_constants": map[string]any{"MCU": "stm32f446xx"},
		"last_stats": map[string]any{"mcu_task_avg": 0.0001, "mcu_task_stddev": 0.0001, "mcu_awake": 0.05, "freq": 180e6}})
	m = openMachine(t, m)
	v := m.View()
	for _, s := range []string{"Machine", "Raspberry Pi 4", "Debian GNU/Linux 12", "up 3d 4h", "23 %", "52.1°C", "4 cores",
		"25 %", "Throttled", "ok", "wlan0 2.0 KB/s", "3 % CPU", "45 MB", "MCUs", "stm32f446xx", "v0.12.0-200", "load  16%", "awake   1%", "180 MHz",
		"klipper", "active (running)", "crowsnest", "inactive (dead)", "Power devices", "printer", "locked while printing", "lights"} {
		if !strings.Contains(v, s) {
			t.Errorf("machine view lacks %q", s)
		}
	}
	if strings.Contains(v, "lo ") {
		t.Error("loopback shown")
	}
	// A pushed stat update applies while open.
	m.onProcStats(rawParams(t, map[string]any{"system_cpu_usage": map[string]any{"cpu": 77.0}, "moonraker_stats": map[string]any{"cpu_usage": 9.0, "memory": 50.0, "mem_units": "MB"}}))
	if v := m.View(); !strings.Contains(v, "77 %") || !strings.Contains(v, "9 % CPU") {
		t.Fatal("proc stat update not applied")
	}

	// enter on a service restarts it, after a confirmation.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirmation == nil || m.confirmation.prompt != "Restart klipper?" {
		t.Fatalf("confirmation %+v", m.confirmation)
	}
	m, cmd := updateCmd(t, m, keyRunes("y"))
	m = update(t, m, cmd())
	srv.WaitForCall("machine.services.restart", 1, wait)
	// x on an inactive service starts it.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown}, keyRunes("x"))
	if m.confirmation == nil || m.confirmation.prompt != "Start crowsnest?" {
		t.Fatalf("x on crowsnest: %+v", m.confirmation)
	}
	m = update(t, m, keyRunes("n"))
	// Power: locked while printing is refused; lights toggles on.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m.job.State = "printing"
	if m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}); m.confirmation != nil || !logContains(m, "locked while printing") {
		t.Fatal("locked device switched mid-print")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirmation == nil || m.confirmation.prompt != "Turn lights on?" {
		t.Fatalf("lights: %+v", m.confirmation)
	}
	m, cmd = updateCmd(t, m, keyRunes("y"))
	m = update(t, m, cmd())
	srv.WaitForCall("machine.device_power.post_device", 1, wait)
	m.onPowerChanged(rawParams(t, map[string]any{"device": "lights", "status": "on"}))
	if m.machine.power[1].Status != "on" {
		t.Fatal("notify_power_changed not applied")
	}
	// Host reboot: a dangerous confirmation.
	m = update(t, m, keyRunes("R"))
	if m.confirmation == nil || !m.confirmation.danger || !strings.Contains(m.View(), "Reboot the printer's host") {
		t.Fatal("R didn't arm a reboot confirmation")
	}
	m = update(t, m, keyRunes("n"), tea.KeyMsg{Type: tea.KeyEsc})
	if m.machine.open {
		t.Fatal("esc didn't close")
	}
}

func TestMachineWithoutPowerComponent(t *testing.T) {
	m := openMachine(t, readyModel(t, machinePrinter(t, false)))
	v := m.View()
	if strings.Contains(v, "Power devices") || strings.Contains(v, "!!") || !strings.Contains(v, "klipper") {
		t.Fatalf("view:\n%s", v)
	}
}
