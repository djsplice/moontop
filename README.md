# Moontop

A terminal UI for [Moonraker](https://moonraker.readthedocs.io/) /
[Klipper](https://www.klipper3d.org/) 3D printers, written in Go using
[Bubbletea](https://github.com/charmbracelet/bubbletea). Roughly a
keyboard-driven analog of Mainsail's main panels.

**Status: work in progress.** Functional for day-to-day printer
control but not exhaustively tested, no releases yet, expect rough
edges. Single-printer, single-host, no auth, no persistence.

## What works

- **Temperatures** — auto-discovered heaters and sensors with live
  current/target readouts (`extruder`, `heater_bed`, `heater_generic`,
  `temperature_sensor`, `temperature_fan`).
- **Toolhead jog pad** — keyboard-driven XY/Z jogging with cycleable
  step sizes (0.1 / 1 / 10 / 25 / 50 / 100 mm), live X/Y/Z position.
- **Tuning** — editable `SET_VELOCITY_LIMIT` fields (velocity,
  acceleration, square corner velocity, min cruise ratio).
- **Files** — gcode file list from the `gcodes` root, sorted by mtime,
  filterable, with y/n confirmation to start a print.
- **Console** — send arbitrary gcode, live response stream, up/down
  history recall.
- **Job control** — pause / resume / cancel with print state in the
  footer, a live progress bar + ETA during prints.
- **Reconnection** — automatic websocket reconnect with exponential
  backoff if the printer reboots or the network blips; everything
  resubscribes automatically.
- **Klipper lifecycle** — a banner whenever Klipper is starting, shut
  down, in error or disconnected (with its reason), restart and
  firmware restart keys, automatic resubscribe after `RESTART` /
  `FIRMWARE_RESTART`, and an emergency stop reachable from any pane.
- **Console extras** — Klipper `!!` errors in red, `//` echoes muted,
  and the last 100 lines of console history pulled from Moonraker on
  startup.

## What's missing

- File upload / delete / rename, subdirectory navigation
- Thumbnails / previews
- Print history, smarter ETAs
- Persistent state (command history, last host) across restarts
- Multi-printer / config file
- Authentication (Moonraker API keys / OAuth)
- AFC / multi-material lanes
- Anything camera-related

## Build and run

Requires Go 1.26+ and a reachable Moonraker instance.

```sh
go run ./cmd/tui -host 192.168.1.100:7125
# or
MOONRAKER_HOST=192.168.1.100:7125 go run ./cmd/tui
```

`host` is the Moonraker websocket endpoint, typically port 7125. The
`ws://<host>/websocket` URL is constructed automatically.

## Keybindings

Global:

| Key      | Action                                                  |
| -------- | ------------------------------------------------------- |
| `Tab`    | Cycle focus: Files → Toolhead → Tuning → Temps → Console |
| `Ctrl+C` | Quit                                                    |
| `q`      | Quit (when console is not focused)                      |
| `p`      | Pause active print                                      |
| `r`      | Resume paused print                                     |
| `c`      | Cancel active print (confirms)                          |
| `Ctrl+X` | Emergency stop (confirms; works from every pane)        |
| `K`      | Restart Klipper (confirms; not in console)              |
| `F`      | Firmware restart (confirms; not in console)             |
| `y`/`n`  | Answer a confirmation prompt                            |

Per pane, when focused:

- **Files**: `↑/↓` select, `/` filter, `Enter` start (confirms)
- **Toolhead**: `←/→` jog X, `↑/↓` jog Y, `PgUp/PgDn` jog Z,
  `[`/`]` step −/+, `H` home all
- **Tuning**: `↑/↓` move between fields, type a value,
  `Enter` apply changed fields
- **Console**: type gcode, `Enter` send, `↑/↓` history recall

## Project layout

```
cmd/tui/                Bubbletea app
  main.go               model, Update, View, send Cmds
  layout.go             pane dimensions, panel helper, footer
  klippy.go             notification dispatch table, Klipper state,
                        e-stop / restarts, banner, console styling
  theme.go              colors and styles
internal/moonraker/     JSON-RPC over websocket client
  client.go             connection lifecycle, read loop, reconnect
  api.go                typed RPC methods
  notify.go             notification method names + parsers
  moonrakertest/        fake Moonraker server for tests
cmd/moonraker-test/     ad-hoc client smoke test
docs/ROADMAP.md         Mainsail gap audit and phase plan
```

The client owns the websocket and forwards every server notification
on one channel; the TUI routes them through a dispatch table in
`klippy.go`. Subscribing happens on one path, `initialize`, which runs
at startup, after a websocket reconnect, and on `notify_klippy_ready`:
it identifies the connection, checks Klipper's state with
`server.info`, and only then lists objects and sends one complete
subscribe map. The TUI model holds one `layout` value computed on every
`WindowSizeMsg`; all panes render through a shared `renderPanel` helper.

Run the tests with `go test ./...`.

## License

[MIT](LICENSE).
