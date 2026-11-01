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
- **Toolhead** — WASD jogging (space jumps Z up, `c` crouches Z
  down), step sizes (0.1 / 1 / 10 / 25 / 50 / 100 mm, or type your
  own), and X/Y/Z fields you can type an absolute position into.
- **Tuning** — live print adjustments and limits, applied at once
  (during a print too): speed factor (`M220`), flow (`M221`), Z offset
  babystep (`SET_GCODE_OFFSET Z_ADJUST`), velocity, acceleration,
  square corner velocity, minimum cruise ratio.
- **Extruder** — tool select (`T0`–`T3`, from the printer's own
  commands), extrude / retract with length and speed, filament load /
  unload, pressure advance and smooth time for the active extruder.
  There's no standard load or unload command, so Moontop uses the first
  the printer has of `LOAD_FILAMENT` / `FILAMENT_LOAD` / `M701` (and
  `UNLOAD_FILAMENT` / `FILAMENT_UNLOAD` / `M702` / `M603`), then any
  macro named like one (`QIDI_LOAD_FILAMENT`); setup → Extruder sets
  them per printer.
- **Outputs** — every fan, output pin and LED the printer has, built from
  the object type: part fan (`M106`/`M107`), `fan_generic`
  (`SET_FAN_SPEED`), heater and controller fans (read-only, with rpm),
  output pins (`SET_PIN`; a percentage when PWM, on/off when digital, in
  the pin's own units when scaled) and LEDs (`SET_LED`; brightness, and
  a hex colour). Objects named `_something` are hidden, as in Mainsail.
- **Macro prompts** — dialogs macros open with `RESPOND
  MSG="action:prompt_…"` (the protocol Mainsail and Fluidd draw) take
  the screen until the printer ends them: `←→`/`Tab` choose a button,
  `Enter` presses it, `Esc` closes it (sends `prompt_end`, like
  Mainsail's ✕). The protocol lines stay out of the console; an open
  prompt in the console history shows when Moontop connects.
- **Exclude object** — during a print with labelled objects, `O` (or
  `F4`) lists them with the one printing now and those excluded;
  `Enter` excludes one after a confirmation.
- **Jobs** — `J` (or `F7`) opens print history and the job queue:
  recent prints with result, date, print time and filament, lifetime
  totals; `Enter` prints one again (confirms), `a` queues it, `x`
  deletes its record. The Queue tab shows the queued files and the
  queue state (`s` starts or pauses it, `x` removes one); from Files,
  `a` queues the selected file. Both are optional Moonraker components
  (`[history]`, `[job_queue]`), and the screen says when one is off.
- **Machine** — `M` (or `F8`): the host (board, OS, uptime, CPU load
  and temperature, memory, disk, throttling, network), Moonraker's own
  usage, every MCU (chip, firmware version, load, awake time,
  frequency), and the services and power devices: `Enter` restarts a
  service or switches a power device, `x` stops or starts a service,
  `R` reboots and `S` shuts down the host. Everything confirms; power
  devices locked while printing stay locked. Live while open.
- **Macros** — every `gcode_macro` the printer has, with its description
  and the parameters its template reads (`params.X|default(…)`, found
  the way Mainsail finds them). `F3` (or `g`) opens a browser of all of
  them: type to filter, `Enter` runs, `space` pins it to the Macros
  pane, which shows only pinned macros (saved per printer in Moonraker's
  database). A macro with parameters opens a form first; only the
  fields you fill in are sent, so its own defaults apply to the rest.
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
| `moontop dump [-prefix AFC]` | Print printer object status as JSON (bug reports, test fixtures). |

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
`4` Temperatures, `5` Console, `6` AFC (only on printers with AFC),
`7` Extruder, `8` Outputs (only on printers with fans, pins or LEDs),
`9` Macros (only on printers with macros).

| Key | Action |
| --- | ------ |
| `1`–`6`, `Tab` / `Shift+Tab` | Jump to a pane / next / previous |
| `?` / `F1` | Help: every key, grouped by pane |
| `F2` / `,` | Setup: panes, heaters, console filters, toolhead, safety, presets |
| `Ctrl+X` | Emergency stop (confirms; works everywhere) |
| `q` / `F10` / `Ctrl+C` | Quit |
| `p` / `r` / `c` | Pause / resume / cancel the print (cancel confirms) |
| `F5` / `F6` (or `K` / `F` outside the console) | Restart Klipper / firmware restart (confirm). F5 and F6 work from every pane, including while the console has focus |
| `Ctrl+O` | Show or hide filtered console lines |
| `y` / `n` / `Esc` | Answer a confirmation prompt |

| Pane | Keys |
| ---- | ---- |
| Files | `↑↓` or `j k` select, `/` filter, `Enter` print (confirms) |
| Toolhead | `w a s d` jog Y+ X− Y− X+, `space` jog Z up, `c` jog Z down (`PgUp/PgDn` too), `↑↓` select X / Y / Z / step, type a number (or `Enter`) to edit it in place — `Enter` moves there (absolute) or uses that step, `Esc` cancels; `← →` or `[ ]` step size, `H` home all, `x y z` home one axis, `m` motors off (idle only), `l` level the gantry (`QUAD_GANTRY_LEVEL` or `Z_TILT_ADJUST`, only on printers with `quad_gantry_level` / `z_tilt`; the pane shows whether it's applied) |
| Tuning | `↑↓` or `j k` select a row; `← →` adjust it at once by its step (speed 5 %, flow 1 %, Z offset 0.01 mm, velocity 10, accel 500; `shift` ×10); type a number (or `Enter`) to edit it in place, `Enter` sets it, `Esc` cancels |
| `F3` / `g` | Macro browser: type to filter, `↑↓` select, `Enter` run, `space` pin / unpin, `Esc` clears the filter, then closes |
| Macros | `↑↓` select a pinned macro, `Enter` run (a parameter form first when it takes parameters: `↑↓` / `Tab` field, `Enter` run, `Esc` cancel), `x` unpin |
| Outputs | `↑↓` select, `← →` adjust (5 %; LEDs 10 % brightness; digital pins on/off), type a value, `x` off, `o` LED colour (hex) |
| Extruder | `+` extrude, `-` retract (length at speed), `l` load, `u` unload (a form first when the macro takes parameters), `↑↓` select a row, `← →` adjust (length and speed step through presets; pressure advance by 0.005), type to set; on the Tool row type the tool number and `Enter` to change tools |
| Temperatures | `↑↓` or `j k` select, `Enter` set the target in place (type °C, `Enter` sends, `Esc` cancels), `t` preheat preset, `x` selected heater off, `X` all heaters off |
| Console | type gcode, `Enter` send, `↑↓` history |
| AFC | `↑↓` or `j k` select lane, `Enter` load into its tool, `u` unload, `e` eject (all confirm), `t` tool, `m` material, `w` weight, `o` colour, `s` spool ID, `x` clear message |

While the console, a file filter or a Tuning field is taking text,
printable keys type text (so `q` or `1` in a filename or gcode is just
text); ctrl-keys, F-keys, `Tab` and arrows still work. In Toolhead,
Tuning, Extruder and Outputs, digits type a value, so use `Tab` to leave them.

Tuning values are what the printer reports: an adjustment shows once
Klipper confirms it. Typed Z offsets are limited to ±2 mm (a slip of
`-1` for `-0.1` mid-print would drive the nozzle into the bed); use the
console for more. Extruding and tool changes are refused while printing
(pause first) and, for extruding, while the nozzle is below its minimum
extrude temperature.

In the Toolhead pane `c` jogs Z down rather than cancelling the print;
cancel from any other pane. Toolhead moves are refused while printing
(pause first), on an axis Klipper hasn't homed (shown as `?` or
`unhomed`), and outside the axis limits. Positions are gcode
coordinates, so what you type is what `G1` moves to.

### Setup and settings

`F2` (or `,`) opens a setup screen modelled on htop's: categories on the
left, their settings on the right; changes apply and save immediately.
In setup, `space` toggles, `enter` edits, `[` `]` reorder, `d d` deletes,
`esc` goes back.

Layout: the top row holds Files, Toolhead, Tuning and Temperatures in
your order; panes with a width share split the spare width (Files 1,
Temperatures 2 by default), the others keep their natural width. The
console runs along the bottom, either taking what the top row doesn't
need or a fixed percentage, with a column of AFC over Macros and a
column of Extruder over Outputs to its left. Tab follows the on-screen order; the pane numbers stay with the
pane. Below about 98 columns, when the panes' minimum widths don't fit,
or when the console would be narrower than 40 columns, everything stacks
in the same order.

| Category | Settings | Saved to |
| -------- | -------- | -------- |
| Panes | Show or hide each pane; reorder the top row (`[` `]`); width share per pane (0 = natural width); console height | config file |
| Columns | Temperatures: State, Current, Target, Power; Files: size, modified; files sorted newest first or by name | config file |
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

### AFC lanes

On printers running Armored Turtle's AFC add-on, or AFC-Lite like the
Snapmaker U1, an AFC pane appears beside the console. It shows each
unit's lanes: colour, lane name (`*` marks the active one), tool,
material, weight when AFC reports one, and state (In tool, Ready, Prep,
Empty, or what AFC is busy doing).

The model follows Fluidd's AFC implementation, with the rules from
[retrograde](https://github.com/justinh-rahb/retrograde) for AFC-Lite:
lane state comes from the sensors (AFC-Lite reports every lane as
"empty"), and a command is only offered when the printer advertises
it. Nothing runs during a print, while AFC is busy, or while it reports
an error (clearing the message is still allowed). Load, unload and
eject confirm first; eject waits until the lane is out of its tool.
Edits validate their input, and values that could smuggle in another
gcode command are refused.

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
  afcpane.go            the AFC pane, its actions, the one-line edit prompt
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
internal/afc/           AFC status model and command builder
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
testdata/               printer status fixtures (afc-lite-u1.json: a U1)
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
