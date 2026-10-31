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
- **Line console and one-shot gcode** — see `moontop console` and
  `moontop gcode` below.
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
go build -o moontop ./cmd/tui
./moontop -host 192.168.1.100:7125
# or
export MOONRAKER_HOST=192.168.1.100:7125
./moontop
```

`host` is the Moonraker websocket endpoint, typically port 7125. The
`ws://<host>/websocket` URL is constructed automatically.

Moontop has three modes:

| Command | What it is |
| ------- | ---------- |
| `moontop` | Full-screen dashboard (everything below). |
| `moontop console` | Interactive gcode console in your normal terminal. |
| `moontop gcode CMD…` | Run gcode, print Klipper's output, exit. |

### `moontop console`

A gcode prompt that stays at the bottom of your terminal while Klipper's
output scrolls above it as ordinary terminal text, so your terminal's
own scrollback, search and copy all work. It works from any machine
that can reach Moonraker — no SSH, no `/tmp/printer` (`klippy.serial`).

- Shows the last 100 console lines on connect, then streams live output.
- `Tab` completes commands and macros (from `printer.gcode.help`);
  `ctrl+n` / `ctrl+p` cycle candidates.
- `↑` / `↓` history, saved across sessions in
  `$XDG_STATE_HOME/moontop/console_history`
  (`~/.local/state/…` by default; `-no-history` to skip).
- The status line shows the connection, Klipper's state, and the
  command currently running (`running G28`).
- `ctrl+x` then `y`: emergency stop. `ctrl+c` clears the line, or quits
  on an empty one; `ctrl+d` or `exit` quits.
- Reconnects and follows Klipper restarts like the dashboard.

### `moontop gcode`

```sh
moontop gcode M115                       # print firmware info
moontop gcode G28 "G1 Z10 F600"          # one command per argument
moontop gcode -echo < calibrate.gcode    # one command per line from stdin
```

Each command runs to completion before the next: Moonraker replies to
`printer.gcode.script` only after Klipper finishes (`G28` after homing,
`M190` once the bed is at temperature), and Klipper's output for it is
printed as it streams. Klipper errors go to stderr and stop the run
(`-k` keeps going). Exit status is 0 when everything succeeded, 1 when a
command failed, 2 for usage or connection errors — so it works in shell
scripts and cron. Output is coloured only when stdout is a terminal.

On stdin, blank lines and whole-line `;` comments are skipped. Sending
a file this way runs it line by line; it isn't a substitute for
starting a print (use the dashboard's Files pane).

Output from other clients (Mainsail, a running print) that arrives
while a command runs is printed too, since Moonraker broadcasts console
output to every connection.

## Keybindings

Press `?` (or `F1`) in the dashboard for the full list. It's generated
from the same keymap the app uses, and shows which keys apply right
now. The footer shows the focused pane's main keys.

Panes are numbered in their titles: `1` Files, `2` Toolhead, `3` Tuning,
`4` Temperatures, `5` Console.

| Key | Action |
| --- | ------ |
| `1`–`5`, `Tab` / `Shift+Tab` | Jump to a pane / next / previous |
| `?` / `F1` | Help: every key, grouped by pane |
| `F2` / `,` | Setup: panes, heaters, console filters, toolhead, safety, presets |
| `Ctrl+X` | Emergency stop (confirms; works everywhere) |
| `q` / `F10` / `Ctrl+C` | Quit |
| `p` / `r` / `c` | Pause / resume / cancel the print (cancel confirms) |
| `K` / `F` | Restart Klipper / firmware restart (confirm) |
| `Ctrl+O` | Show or hide filtered console lines |
| `y` / `n` / `Esc` | Answer a confirmation prompt |

| Pane | Keys |
| ---- | ---- |
| Files | `↑↓` or `j k` select, `/` filter, `Enter` print (confirms) |
| Toolhead | `←→` jog X, `↑↓` jog Y, `PgUp/PgDn` jog Z, `[ ]` step size, `H` home all, `x y z` home one axis, `m` motors off (idle only) |
| Tuning | `↑↓` or `j k` select field, type a value, `Enter` apply changed fields |
| Temperatures | `↑↓` or `j k` select, `Enter` set the target in place (type °C, `Enter` sends, `Esc` cancels), `t` preheat preset, `x` selected heater off, `X` all heaters off |
| Console | type gcode, `Enter` send, `↑↓` history |

While the console, a file filter or a Tuning field is taking text,
printable keys type text (so `q` or `1` in a filename or gcode is just
text); ctrl-keys, F-keys, `Tab` and arrows still work.

### Setup and settings

`F2` (or `,`) opens a setup screen modelled on htop's: categories on the
left, their settings on the right; changes apply and save immediately.

| Category | Settings | Saved to |
| -------- | -------- | -------- |
| Panes | Show or hide each pane; the others take its space | config file |
| Temperatures | Hide sensors, rename heaters (`extruder1` → `T1 ASA`) | printer |
| Console | Heat-wait and TMC filters on/off, your own regex filters | config file |
| Toolhead | Jog step sizes, XY and Z feedrates | config file |
| Safety | Confirm E-stop, confirm restarts when idle | config file |
| Presets | Save the current heater targets as a preset; delete | printer |
| Keys | Every key (read-only) | — |

The config file is `~/.config/moontop/config.toml` (`$XDG_CONFIG_HOME`
respected, `-config` to use another). It only stores what differs from
the defaults, and is safe to edit by hand. If it fails to parse,
Moontop warns, runs on defaults, and won't overwrite it until it's
fixed.

Per-printer settings are kept in the printer's Moonraker database
(namespace `moontop`), so they follow the printer to any computer, the
same way Mainsail's settings do. They take precedence over the same
settings in the file.

### Heaters

Targets are set in place: select a heater in Temperatures, press
`Enter`, type the target and press `Enter` again. The table keeps
showing the printer's confirmed target, so the new value appears once
Klipper reports it. Extruders, the bed and `heater_generic` use
`SET_HEATER_TEMPERATURE`; temperature fans use
`SET_TEMPERATURE_FAN_TARGET`. Klipper rejects targets above a heater's
`max_temp`, and the error shows in the console.

`t` lists your presets: Moontop's own (saved from setup) first, then
any you made in Mainsail, which Moontop reads from the same Moonraker
database. `X` runs Mainsail's cooldown gcode (`TURN_OFF_HEATERS` by
default). Turning heaters off during a print asks first.

### Console filters

Klipper's heat-wait temperature reports (`B:26.3 /0.0 T0:168.1 /140.0`
every second during `M109`/`M190`) and TMC `Run Current:` reports are
hidden by default in the dashboard and `moontop console`; the console
shows how many lines are hidden, and `Ctrl+O` shows them. `moontop gcode`
prints raw output unless given `-filter`. Klipper errors (`!!`) are
never hidden.

## Project layout

```
cmd/tui/                the moontop binary
  cli.go                subcommand dispatch, `moontop gcode`
  keys.go               keymap registry: every key, its scope and help
  heaters.go            heater targets, cooldown, presets
  setup.go              the F2 setup screen
  settings.go           applying and saving settings
  help.go               key routing and the ? help overlay
  repl.go               `moontop console` (inline Bubbletea program)
  cmds.go               client Cmds shared by dashboard and console
  main.go               dashboard model, Update, View
  layout.go             pane dimensions, panel helper, footer
  klippy.go             notification dispatch table, Klipper state,
                        e-stop / restarts, banner, console styling
  theme.go              colors and styles
internal/config/        settings: config.toml and per-printer settings
internal/console/       shared by all three modes: line classification,
                        filters, history, backfill, one-shot gcode runner
internal/moonraker/     JSON-RPC over websocket client
  client.go             connection lifecycle, read loop, reconnect
  api.go                typed RPC methods
  notify.go             notification method names + parsers
  moonrakertest/        fake Moonraker server for tests
cmd/moonraker-test/     ad-hoc client smoke test
docs/ROADMAP.md         Mainsail gap audit and phase plan
docs/UI-PLAN.md         controls and configurability plan (UI-1 to UI-3)
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
