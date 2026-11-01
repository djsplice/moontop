package main

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinh-rahb/moontop/internal/moonraker"
)

// Client-only Cmds shared by the dashboard and the line console. The
// dashboard's model methods of the same name delegate here.

func listenNotificationsCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		n, ok := <-client.Notifications()
		if !ok {
			return errMsg{fmt.Errorf("notification channel closed")}
		}
		return notificationMsg(n)
	}
}

func fetchPrinterInfoCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		info, err := client.GetPrinterInfo()
		if err != nil {
			return klippyStateMsg{err: err}
		}
		return klippyStateMsg{state: info.State, message: info.StateMessage}
	}
}

func emergencyStopCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg { return klippyActionMsg{"emergency stop", client.EmergencyStop()} }
}

func restartKlipperCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg { return klippyActionMsg{"restart", client.RestartKlipper()} }
}

func firmwareRestartCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg { return klippyActionMsg{"firmware restart", client.FirmwareRestart()} }
}

func loadGcodeStoreCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		entries, err := client.GcodeStore(gcodeStoreBackfill)
		return gcodeStoreMsg{entries: entries, err: err}
	}
}

func listenDisconnectCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg {
		err := <-client.Disconnected()
		return disconnectedMsg{err: err}
	}
}

func attemptReconnectCmd(client *moonraker.Client, attempt int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		err := client.Reconnect(context.Background())
		return reconnectResultMsg{attempt: attempt, err: err}
	})
}

func sendGcodeCmd(client *moonraker.Client, cmd string) tea.Cmd {
	return func() tea.Msg {
		return gcodeSentMsg{cmd: cmd, err: client.GcodeScript(cmd)}
	}
}

// identifyAndCheckKlippy is the first step of every (re)connect, for
// both frontends: identify the connection (best effort — an old
// Moonraker without server.connection.identify still works), then ask
// server.info whether Klippy is ready. When it isn't, printer.info
// supplies the reason (config error text, shutdown cause) if Klippy is
// connected at all. err is set only when server.info itself fails.
func identifyAndCheckKlippy(client *moonraker.Client) klippyStateMsg {
	_ = client.Identify(clientName, version, "other", clientURL)

	info, err := client.GetServerInfo()
	if err != nil {
		return klippyStateMsg{err: fmt.Errorf("server info: %w", err)}
	}
	if info.KlippyConnected && info.KlippyState == "ready" {
		return klippyStateMsg{state: "ready"}
	}
	msg := klippyStateMsg{state: info.KlippyState}
	if !info.KlippyConnected {
		msg.state = "disconnected"
		return msg
	}
	if pi, err := client.GetPrinterInfo(); err == nil {
		msg.message = pi.StateMessage
	}
	return msg
}

// checkKlippyCmd wraps identifyAndCheckKlippy as a Cmd.
func checkKlippyCmd(client *moonraker.Client) tea.Cmd {
	return func() tea.Msg { return identifyAndCheckKlippy(client) }
}
