<div align="center">

# ◐ Moontop

**A keyboard-driven terminal UI for Klipper printers, through Moonraker.**<br>
Everything you reach for in Mainsail or Fluidd — in a terminal, over SSH, on any machine that can see your printer.

[![CI](https://github.com/justinh-rahb/moontop/actions/workflows/ci.yml/badge.svg)](https://github.com/justinh-rahb/moontop/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/justinh-rahb/moontop?sort=semver&color=2ea043)](https://github.com/justinh-rahb/moontop/releases/latest)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Klipper](https://img.shields.io/badge/Klipper-Moonraker-b12f36)](https://moonraker.readthedocs.io/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Buy Me a Coffee](https://img.shields.io/badge/Buy%20me%20a%20coffee-ffdd00?logo=buymeacoffee&logoColor=black)](https://buymeacoffee.com/wildtang3nt)

[Website](https://justinh-rahb.github.io/moontop/) · [Install](#install) · [Quick start](#quick-start) · [The dashboard](#the-dashboard) · [Screens](#full-screen-tools) · [Keys](#keyboard-reference) · [Settings](#settings) · [Printers](#printer-support)

</div>

```text
◐ Moontop
╭──────────────────────────────────╮╭────────────────────────╮╭──────────────────────────╮╭────────────────────────────────────────────────────╮
│ 1 Files                          ││ 2 Toolhead             ││ 3 Tuning                 ││ 4 Temperatures                                     │
│                                  ││   X      10.00         ││   Speed     100 %        ││  Name              Current    Target     History   │
│ │ benchy.gcode                   ││   Y      20.00         ││   Flow      100 %        ││ ────────────────────────────────────────────────── │
│ │ 1.2 MB                         ││   Z       5.00         ││   Z offset  0.000 mm     ││  Extruder          168.1°C    140.0°C    ⣴⣿⣿⣿⣿⣷⣦⣄  │
│                                  ││   step 10 mm           ││   Velocity  500 mm/s     ││  Extruder 1        25.0°C     0.0°C      ⣤⣤⣤⣤⣤⣤⣤⣤  │
│   cube.gcode                     ││                        ││   Accel     20000 mm/s²  ││  Bed               26.3°C     0.0°C      ⣀⣀⣀⣀⣀⣀⣀⣀  │
│   2.3 KB                         ││       w      ␣ Z↑      ││   Corner    —            ││                                                    │
│                                  ││     a + d    c Z↓      ││   Cruise    —            ││                                                    │
│                                  ││       s                ││                          ││                                                    │
│                                  ││                        ││ ←→ adjust  type a value  ││                                                    │
│                                  ││ ↑↓ field  ← → step     ││                          ││                                                    │
│                                  ││ H home   x y z axis    ││                          ││                                                    │
│                                  ││ m motors off           ││                          ││                                                    │
│                                  ││                        ││                          ││                                                    │
╰──────────────────────────────────╯╰────────────────────────╯╰──────────────────────────╯╰────────────────────────────────────────────────────╯
╭────────────────────────────────────────────╮╭────────────────────────────╮╭──────────────────────────────────────────────────────────────────╮
│ 6 AFC                                      ││ 7 Extruder                 ││ 5 Console                                                        │
│ idle · active E1                           ││   Tool          T1         ││ >>> STATUS                                                       │
│   ██ E0    T0    ASA     In tool           ││   Length        10.0 mm    ││ // Klipper state: Ready                                          │
│   ██ E1*   T1    PLA     In tool           ││   Speed         5.0 mm/s   ││ ** ── live ──                                                    │
│   ██ E2    T2    PC      In tool           ││   Pressure adv  —          ││                                                                  │
│   ·· E3    T3    —       Empty             ││   Smooth time   —          ││                                                                  │
╰────────────────────────────────────────────╯│                            ││                                                                  │
╭────────────────────────────────────────────╮│ + extrude  - retract       ││                                                                  │
│ 9 Macros                                   ││ l load  (LOAD_FILAMENT)    ││                                                                  │
│   CLEAN_NOZZLE                             │╰────────────────────────────╯│                                                                  │
│   LOAD_FILAMENT …  Load filament into the  │╭────────────────────────────╮│                                                                  │
│   PRINT_START …  Heat, home, mesh and purg ││ 8 Outputs                  ││                                                                  │
│                                            ││   Part fan    50 %         ││                                                                  │
│                                            ││   aux fan     0 %          ││                                                                  │
│                                            ││   hotend fan  100% 6900rpm ││                                                                  │
│                                            ││   beeper      off          ││                                                                  │
│                                            ││   caselight   40 %         ││                                                                  │
│                                            ││   bar         ██ 100 %     ││                                                                  │
│                                            ││                            ││ > Type a gcode command (e.g. M115, G28) and press Enter; tab …   │
╰────────────────────────────────────────────╯╰────────────────────────────╯╰──────────────────────────────────────────────────────────────────╯
● online  •  Temperatures  ↑↓ j k select  •  enter set  •  t preheat  •  x off  •  X all off  •  Standby  •  ? help  •  F2 setup  •  ctrl+x e-stop
```

<sub>The dashboard at 150 columns, against a test printer carrying a Snapmaker U1's AFC-Lite lanes.</sub>

---

## Contents

- [Highlights](#highlights)
- [Install](#install)
- [Quick start](#quick-start)
- [Four ways to run it](#four-ways-to-run-it)
- [The dashboard](#the-dashboard) — the nine panes
- [Full-screen tools](#full-screen-tools) — macros, jobs, machine, software, bed mesh, logs, config
- [Pop-ups from the printer](#pop-ups-from-the-printer) — macro prompts, manual probe
- [Keyboard reference](#keyboard-reference)
- [Safety](#safety)
- [Settings](#settings)
- [Printer support](#printer-support)
- [Development](#development)
- [Credits](#credits)
- [Support](#support)

---

## Highlights

| | |
| --- | --- |
| 🌡️ **Temperatures** | Every heater and sensor, targets set in place, presets and cooldown, a braille sparkline of the last five minutes |
| 🕹️ **Motion** | WASD jogging, typed absolute moves, homing, motors off, endstops, QGL / Z tilt |
| 🎛️ **Live tuning** | Speed, flow, Z-offset babystepping, velocity limits, pressure advance — applied at once, mid-print too |
| 🧵 **Filament** | Extrude / retract, load / unload (whatever your printer calls it), tool changes, AFC lanes |
| 💡 **Outputs** | Fans, output pins and LEDs, discovered from your config |
| 📜 **Macros** | Every macro with its parameters; pin your favourites; parameter forms; macro prompts |
| 🖨️ **Printing** | Files, pause / resume / cancel, progress and ETA, exclude object, history, job queue |
| 🗺️ **Bed** | The mesh as a coloured grid of numbers, profiles, calibrate, screws tilt, manual probe |
| 🛠️ **Machine** | Host and MCU stats, services, power devices, updates, announcements, logs, config editing in `$EDITOR` |
| 🛡️ **Safety** | E-stop from anywhere, confirmations, refusals that explain themselves, never a value you didn't confirm |

Moontop only ever shows what the printer reports. A target you set appears when Klipper confirms it, not before.

---

## Install

Moontop is one static binary with nothing to install on the printer.

| Platform | Command |
| --- | --- |
| **macOS / Linux** (Homebrew) | `brew install --cask justinh-rahb/tap/moontop` |
| **Windows** (Scoop) | `scoop bucket add justinh-rahb https://github.com/justinh-rahb/scoop-bucket`<br>`scoop install moontop` |
| **Debian / Ubuntu / Raspberry Pi OS** | download the `.deb` from [Releases](https://github.com/justinh-rahb/moontop/releases/latest), then `sudo apt install ./moontop_*.deb` |
| **Fedora / RHEL**, **Alpine**, **Arch** | `.rpm`, `.apk` and `.pkg.tar.zst` on the [Releases](https://github.com/justinh-rahb/moontop/releases/latest) page |
| **Any Linux, macOS, FreeBSD** | `curl -fsSL https://raw.githubusercontent.com/justinh-rahb/moontop/main/install.sh \| sh` |
| **With Go 1.26+** | `go install github.com/justinh-rahb/moontop/cmd/moontop@latest` |

Builds cover amd64 and arm64 everywhere, plus 32-bit ARM (armv7) on Linux, so it also runs on the Pi next to the printer. The install script checks the download against the release checksums and installs to `/usr/local/bin` (or `~/.local/bin`); set `MOONTOP_VERSION` or `MOONTOP_INSTALL_DIR` to change either. `moontop version` shows what you have.

<details>
<summary><b>Build from source</b></summary>

<br>

```sh
git clone https://github.com/justinh-rahb/moontop
cd moontop
go build -o moontop ./cmd/moontop
```

</details>

> macOS binaries aren't notarized yet. The Homebrew cask clears the quarantine flag for you; if you download the tarball by hand, run `xattr -d com.apple.quarantine ./moontop` once.

---

## Quick start

Point it at Moonraker (usually port 7125):

```sh
moontop -host 192.168.1.100:7125

# or set it once
export MOONRAKER_HOST=192.168.1.100:7125
moontop
```

Then press **`?`** for every key, or **`F2`** for settings. A few things worth knowing straight away:

- **`Tab`** moves between panes; **`1`–`9`** jump to one (the numbers are in the pane titles).
- **`Ctrl+X`** is the emergency stop, from anywhere.
- **`q`** quits.
- Moontop needs no SSH and nothing installed on the printer. Moonraker's API keys and login aren't supported yet.

---

## Four ways to run it

| Command | What it is |
| --- | --- |
| `moontop` | The full-screen dashboard. |
| `moontop console` | An interactive gcode console that lives in your normal terminal. |
| `moontop gcode CMD…` | Run gcode, print Klipper's output, exit — for scripts and cron. |
| `moontop dump [-prefix P]` | Print printer object status as JSON (bug reports, test fixtures). |

All of them take `-host`, or read `$MOONRAKER_HOST`.

<details>
<summary><b><code>moontop console</code></b> — a gcode prompt in your terminal</summary>

<br>

A prompt that stays at the bottom of your terminal while Klipper's output scrolls above it as ordinary terminal text, so your terminal's own scrollback, search and copy all work. No SSH, no `/tmp/printer`.

- The last 100 console lines on connect, then live output.
- `Tab` completes commands and macros; `Ctrl+N` / `Ctrl+P` cycle candidates.
- `↑` / `↓` history, kept across sessions in `$XDG_STATE_HOME/moontop/console_history` (`-no-history` to skip).
- A status line with the connection, Klipper's state, and what's running (`running G28`).
- `Ctrl+X` then `y`: emergency stop. `Ctrl+C` clears the line, or quits on an empty one; `Ctrl+D` or `exit` quits.
- Heat-wait temperature reports and TMC current reports are hidden, as in the dashboard.

</details>

<details>
<summary><b><code>moontop gcode</code></b> — one-shot gcode for scripts</summary>

<br>

```sh
moontop gcode M115                       # print firmware info
moontop gcode G28 "G1 Z10 F600"          # one command per argument
moontop gcode -echo < calibrate.gcode    # one command per line from stdin
```

Each command runs to completion before the next: Moonraker answers `printer.gcode.script` only when Klipper finishes (`G28` after homing, `M190` once the bed is hot), and Klipper's output streams as it comes. Errors go to stderr and stop the run.

| Flag | |
| --- | --- |
| `-echo` | Print each command before its output |
| `-k` | Keep going after a failed command |
| `-filter` | Hide lines your console filters match (raw output otherwise) |

Exit status: `0` all succeeded, `1` a command failed, `2` usage or connection error. Output is coloured only on a terminal. On stdin, blank lines and whole-line `;` comments are skipped. This isn't a way to print a file — use Files for that.

</details>

<details>
<summary><b><code>moontop dump</code></b> — printer state as JSON</summary>

<br>

```sh
moontop dump                 # every object
moontop dump -prefix AFC     # just the AFC objects
```

Writes `{"object": {fields…}}`, the format Moontop's test fixtures use. `configfile` is left out unless `-prefix` asks for it — it's large and holds your whole printer config.

</details>

---

## The dashboard

```text
┌──────────┬──────────┬──────────┬──────────────────┐
│ 1 Files  │ 2 Tool-  │ 3 Tuning │ 4 Temperatures   │   top row: your order, your widths
│          │   head   │          │                  │
├──────────┴───┬──────┴─────┬────┴──────────────────┤
│ 6 AFC        │ 7 Extruder │ 5 Console             │   bottom row: columns beside the console
├──────────────┼────────────┤                       │
│ 9 Macros     │ 8 Outputs  │                       │
└──────────────┴────────────┴───────────────────────┘
 status · focused pane's keys · job · progress / ETA · help · e-stop
```

Panes only appear when the printer has something for them: AFC needs AFC, Outputs needs a fan, pin or LED, Macros needs a macro. Rearrange, resize or hide any of them in setup (`F2`). On a narrow terminal everything stacks in the same order.

The footer always shows the focused pane's main keys, what's happening with the print, and the connection. The title shows a badge (`⬆ 2 updates · ✉ 1`) when software updates or announcements are waiting.

### 1 · Files

Your gcode files, newest first (or by name). Type `/` to filter.

| Key | |
| --- | --- |
| `Enter` | Print the selected file (confirms) |
| `a` | Add it to the job queue |
| `/` | Filter |

### 2 · Toolhead

| Key | |
| --- | --- |
| `w` `a` `s` `d` | Jog Y+, X−, Y−, X+ by the step |
| `Space` / `c` | Jog Z up ("jump") / down ("crouch"); `PgUp` / `PgDn` too |
| `↑` `↓` | Select X, Y, Z or the step |
| digits or `Enter` | Type a position: `Enter` moves there (absolute) — or, on the step row, sets a custom step |
| `←` `→` / `[` `]` | Smaller / larger step (0.1 · 1 · 10 · 25 · 50 · 100 mm by default) |
| `H` · `x` `y` `z` | Home all · home one axis |
| `m` | Motors off (`M84`; not while printing) |
| `l` | Level the gantry — `QUAD_GANTRY_LEVEL` or `Z_TILT_ADJUST`, on printers that have one; the pane shows whether it's applied |
| `e` | Query the endstops (result in the console) |

Positions are gcode coordinates, so what you type is what `G1` moves to. An axis Klipper hasn't homed is marked `?` / `unhomed`.

### 3 · Tuning

Live adjustments, applied the moment you press a key — during a print too.

| Row | Arrows step by | Sends |
| --- | --- | --- |
| Speed | 5 % | `M220` |
| Flow | 1 % | `M221` |
| Z offset | 0.01 mm | `SET_GCODE_OFFSET Z_ADJUST=…` (moves the nozzle when homed) |
| Velocity · Accel · Corner · Cruise | 10 · 500 · 1 · 0.05 | `SET_VELOCITY_LIMIT` |

`↑` `↓` pick a row, `←` `→` adjust (hold `Shift` for ×10), or type a value and press `Enter`. Typed Z offsets are limited to ±2 mm.

### 4 · Temperatures

Every heater and sensor, with state, current, target, heater power and a **braille sparkline** of the last five minutes (each character is two samples at four heights, filled from Moonraker's temperature store on connect).

| Key | |
| --- | --- |
| `Enter` | Set the selected heater's target in place — type °C, `Enter` sends |
| `t` | Preheat from a preset (Moontop's own, then Mainsail's) |
| `x` / `X` | Selected heater off / everything off (Mainsail's cooldown gcode) |

Hide sensors, rename heaters (`extruder1` → `T1 ASA`) and choose columns in setup.

### 5 · Console

Type gcode and press `Enter`. Klipper errors are red, echoes muted, and Moontop's own actions are echoed as `>>> …` so you can see exactly what was sent.

- `Tab` completes commands (`Ctrl+N` / `Ctrl+P` for others) — and still moves to the next pane when there's nothing to complete.
- While you type a macro, the pane title shows the parameters you haven't given yet, with their defaults.
- `↑` `↓` history. `Ctrl+O` shows the lines filters hide (heat-wait temperatures and TMC current reports, by default).
- `F5` / `F6` restart Klipper from here, where `K` and `F` are just letters.

### 6 · AFC

For Armored Turtle's AFC add-on — including AFC-Lite on the Snapmaker U1. Each unit's lanes with colour, tool, material, weight and state, the active lane starred.

| Key | |
| --- | --- |
| `Enter` · `u` · `e` | Load into its tool · unload · eject (all confirm) |
| `t` `m` `w` `o` `s` | Set tool, material, weight, colour, Spoolman spool ID |
| `x` | Clear AFC's message |

### 7 · Extruder

| Row / key | |
| --- | --- |
| Tool | The active tool; type a number and `Enter` to change (from the printer's own `T0`, `T1`… commands) |
| Length · Speed | What `+` and `-` use; `←` `→` step through presets |
| Pressure adv · Smooth time | For the active extruder (`SET_PRESSURE_ADVANCE`) |
| `+` / `-` | Extrude / retract |
| `l` / `u` | Load / unload filament — see [Printer support](#printer-support) |

Extruding is refused while the nozzle is too cold; extruding and tool changes are refused mid-print (pause first).

### 8 · Outputs

Every fan, output pin and LED in your config, each row built from what it is.

| Kind | Shown as | Sends |
| --- | --- | --- |
| Part fan | % | `M106` / `M107` |
| `fan_generic` | % | `SET_FAN_SPEED` |
| Heater / controller fans | % and rpm (read-only) | — |
| Output pins | % (PWM), on / off (digital), or the pin's own units (scaled) | `SET_PIN` |
| LEDs | colour swatch and brightness | `SET_LED` |

`←` `→` adjust, type a value, `x` turns it off, `o` sets an LED's colour (hex). Names starting with `_` are hidden, as in Mainsail.

### 9 · Macros

Only the macros you've **pinned** — a printer can have dozens. Pin them from the macro browser (`F3`); pins are saved on the printer.

| Key | |
| --- | --- |
| `Enter` | Run (a parameter form first, when it takes any) |
| `x` | Unpin |

---

## Full-screen tools

Each opens from anywhere and closes with `Esc`.

| Key | Screen | |
| --- | --- | --- |
| `F3` / `g` | **Macros** | Every macro: type to filter, `Enter` runs, `Space` pins to the dashboard |
| `O` / `F4` | **Objects** | During a print with labelled objects: the one printing now, those excluded; `Enter` excludes one (confirms) |
| `J` / `F7` | **Jobs** | Print history and totals, the job queue |
| `M` / `F8` | **Machine** | Host, MCUs, services, power devices |
| `U` / `F9` | **Software** | Updates and announcements |
| `B` / `F11` | **Bed mesh** | Mesh grid, profiles, calibrate, screws tilt |
| `L` / `F12` | **Logs** | `klippy.log`, `moonraker.log`… |
| `E` | **Config** | Read and edit config files |
| `F2` / `,` | **Setup** | All of Moontop's settings |
| `?` / `F1` | **Help** | Every key, grouped, with what applies right now |

<details open>
<summary><b>Macros</b> — <code>F3</code> / <code>g</code></summary>

<br>

Every `gcode_macro` with its description and the parameters its template reads (`params.X|default(…)`, found the way Mainsail finds them). Type to filter, `Enter` to run, `Space` to pin or unpin. Helper macros named `_something` are hidden.

A macro with parameters opens a form showing each default and a preview of the exact command. Only the fields you fill in are sent, so the macro's own defaults apply to the rest.

```text
◐ Moontop  ·  Run PRINT_START
Heat, home, mesh and purge
▸ BED       65    default 60
  EXTRUDER        default 210
  CHAMBER
sends  PRINT_START BED=65
```

</details>

<details>
<summary><b>Jobs</b> — <code>J</code> / <code>F7</code></summary>

<br>

```text
◐ Moontop  ·  Jobs   History   Queue (1)

3 jobs · 1h17m printing · 3.4 m filament · longest print 1h12m

▸ ✓ benchy.gcode      2026-10-30 14:02   1h12m   3.4 m  completed
  ✗ cube.gcode        2026-10-29 09:26      5m          cancelled
  ! gone.gcode        2026-10-28 06:40       —          error
```

**History:** `Enter` prints a file again (confirms), `a` queues it, `x` deletes the record (the file stays).<br>
**Queue:** `s` starts or pauses the queue, `x` removes an entry. From Files, `a` queues a file.

Both are optional Moonraker components (`[history]`, `[job_queue]`); the screen tells you when one is off.

</details>

<details>
<summary><b>Machine</b> — <code>M</code> / <code>F8</code></summary>

<br>

```text
Host      Raspberry Pi 4 Model B  ·  Debian GNU/Linux 12 (bookworm)  ·  up 3d 4h
CPU       ▕███·········▏  23 %   52.1°C   4 cores
Memory    ▕███·········▏  25 %   977 / 3906 MB
Disk      ▕███·········▏  25 %   7.5 GB / 29.8 GB
Throttled ok
Moonraker 3 % CPU  ·  45 MB

MCUs
  mcu            stm32f446xx  v0.12.0-200  load  16%  awake   1%  180 MHz

Services  (enter restarts, x stops / starts)
▸ klipper         active (running)
  crowsnest       inactive (dead)

Power devices  (enter switches)
  printer         on  locked while printing
```

Live while open. `R` reboots and `S` shuts down the host. Every action confirms; power devices locked while printing stay locked.

</details>

<details>
<summary><b>Software</b> — <code>U</code> / <code>F9</code></summary>

<br>

**Updates:** each component's version, the version available, and the commits it's behind (with their subjects). `r` checks, `Enter` updates one, `a` updates everything; output streams in as it runs. Like Mainsail, a git repo has an update when it's commits behind, a release when the remote version is newer — so a beta ahead of the latest release isn't flagged. Refused mid-print.

**Announcements:** Moonraker's news feed, with description and link; `d` dismisses.

</details>

<details>
<summary><b>Bed mesh</b> — <code>B</code> / <code>F11</code></summary>

<br>

The mesh as a grid of numbers, the way Marlin prints `G29` — back of the bed on top — each value in a box coloured by height: green near zero, through blue below and red above.

```text
Profile default  ·  range -0.050 to +0.100 (0.150 mm)  ·  X 10–220  Y 10–220

             0        1        2
     ┌────────┬────────┬────────┐
   2 │ +0.100 │ +0.075 │ +0.060 │
     ├────────┼────────┼────────┤
   1 │ +0.010 │ +0.020 │ +0.030 │
     ├────────┼────────┼────────┤
   0 │ -0.050 │ +0.000 │ +0.025 │
     └────────┴────────┴────────┘
```

| Key | |
| --- | --- |
| `Enter` · `s` · `x` | Load · save as · remove a profile |
| `w` | `SAVE_CONFIG`, so saved profiles survive a restart |
| `c` · `C` | Calibrate a new mesh · clear it |
| `t` | `SCREWS_TILT_CALCULATE`, with each screw's turn shown (needs `[screws_tilt_adjust]`) |

</details>

<details>
<summary><b>Logs</b> — <code>L</code> / <code>F12</code></summary>

<br>

The last half megabyte of any log, errors, warnings and tracebacks coloured. `←` `→` switch log, `f` follows it live, `/` filters, `r` reloads, `R` rolls the log over.

</details>

<details>
<summary><b>Config</b> — <code>E</code></summary>

<br>

Your config files, `printer.cfg` first. `Enter` reads one (sections, keys and comments highlighted). `e` opens it in **`$VISUAL` / `$EDITOR`** (or `vi`) — Moontop steps aside while the editor runs — and if you changed it, it's uploaded and Moontop offers the restart that applies it: Klipper for `.cfg`, Moonraker for `moonraker.conf`.

</details>

---

## Pop-ups from the printer

Some things the printer starts. Moontop shows them as they happen:

- **Macro prompts** — dialogs a macro opens with `RESPOND MSG="action:prompt_…"` (the protocol Mainsail and Fluidd draw). `←` `→` / `Tab` choose a button, `Enter` presses it, `Esc` closes the prompt. An open prompt shows even if it started before Moontop connected.
- **Manual probe** — whenever Klipper waits on a paper test (`PROBE_CALIBRATE`, `Z_ENDSTOP_CALIBRATE`, `MANUAL_PROBE`…): `↑` `↓` raise or lower by the step, `←` `→` change the step (0.005–1 mm), `Enter` accepts, `Esc` aborts.
- **Klipper's state** — a banner whenever Klipper is starting, shut down, in error or disconnected, with its reason and the restart keys. Moontop resubscribes by itself after a restart, and reconnects on its own if the network drops.

---

## Keyboard reference

Press **`?`** in the dashboard for the complete, always-current list — it's generated from the same keymap Moontop uses, and dims keys that don't apply right now.

| Everywhere | |
| --- | --- |
| `Tab` / `Shift+Tab` · `1`–`9` | Next / previous pane · jump to a pane |
| `Ctrl+X` | **Emergency stop** (confirms; works on every screen) |
| `?` `F1` · `F2` `,` | Help · setup |
| `F3` `g` · `J` `F7` · `M` `F8` · `U` `F9` · `B` `F11` · `L` `F12` · `E` | Macros · jobs · machine · software · bed mesh · logs · config |
| `Ctrl+O` | Show / hide filtered console lines |
| `q` `F10` `Ctrl+C` | Quit |

| Print and Klipper | |
| --- | --- |
| `p` · `r` · `c` | Pause · resume · cancel (confirms) |
| `O` `F4` | Exclude an object |
| `F5` `K` · `F6` `F` | Restart Klipper · firmware restart (confirm) |
| `y` · `n` `Esc` | Answer a confirmation |

While the console or a filter takes text, letters are just text — `F`-keys, `Ctrl`-keys, `Tab` and arrows still work. In Toolhead, Tuning, Extruder and Outputs, digits type a value; use `Tab` to leave. In Toolhead, `c` is Z-down rather than cancel; cancel from any other pane.

---

## Safety

Moontop is meant to be hard to hurt a printer with.

- **Emergency stop** is `Ctrl+X` from any screen, any prompt, any edit.
- **Confirmations** before anything that can ruin a print or a machine: starting or cancelling a print, restarts, load / unload / eject, excluding an object, `SAVE_CONFIG`, updates, service and power actions, host reboot. The risky ones mid-print are shown in red.
- **Refusals with a reason**, rather than an error from Klipper: no moves, homing, extruding, tool changes, calibration or updates mid-print (pause first); no moves on an axis that isn't homed or outside its limits; no extruding below the minimum temperature; no Z offset over ±2 mm typed.
- **Nothing smuggled into gcode:** values typed into AFC fields, macro forms and profile names can't contain characters that would add another command.
- **Only confirmed state on screen:** every value shown is what the printer reports.
- **Everything echoed:** what Moontop sends shows in the console as `>>> …`.

---

## Settings

`F2` (or `,`) opens a setup screen modelled on htop's: categories on the left, settings on the right, every change applied and saved at once.

| Category | Settings | Saved to |
| --- | --- | --- |
| **Panes** | Show / hide each pane, top-row order, width shares, console height | config file |
| **Columns** | Temperatures: State, Current, Target, Power, History · Files: size, modified, sort | config file |
| **Temperatures** | Hide sensors, rename heaters | printer |
| **Console** | Heat-wait and TMC filters, your own regex filters | config file |
| **Toolhead** | Jog step sizes, XY and Z feedrates | config file |
| **Extruder** | Load and unload commands (empty = detect) | printer |
| **Safety** | Confirm E-stop, confirm restarts when idle | config file |
| **Presets** | Save the current targets as a preheat preset; delete | printer |
| **Keys** | Every key (read-only; remapping comes later) | — |

**The config file** is `~/.config/moontop/config.toml` (`$XDG_CONFIG_HOME` respected; `-config` picks another). It stores only what differs from the defaults and is safe to edit by hand. If it fails to parse, Moontop says so, runs on defaults, and won't overwrite it until it's fixed.

```toml
[panes]
order = ["Temperatures", "Files", "Toolhead", "Tuning"]
hidden = ["Outputs"]
console_height = 40          # percent

[toolhead]
step_sizes = [0.1, 1, 10, 50]
z_feedrate = 300

[[console.filters]]
name = "Probe chatter"
pattern = "^// probe at"
```

**Per-printer settings** — heater names, presets, pinned macros, load / unload commands — live in the printer's Moonraker database (namespace `moontop`), so they follow the printer to any computer, the way Mainsail's settings do. Moontop also reads Mainsail's presets and cooldown gcode from there.

---

## Printer support

Moontop works with stock Klipper and Moonraker, and discovers what each printer has instead of assuming. It's been used on a **Snapmaker U1** (toolchanger, AFC-Lite) and a **Qidi Q2**.

| | |
| --- | --- |
| **Toolchangers and multiple extruders** | Every `extruderN` is listed; tools come from the printer's own `T0`, `T1`… commands, and tool *N* is `extruder`*N*. |
| **AFC** | The model follows Fluidd's, with the AFC-Lite rules from [retrograde](https://github.com/justinh-rahb/retrograde): lane state from the sensors, only lanes listed in `AFC.lanes` (a ViViD's drive and selector steppers aren't lanes), and a command is offered only if the printer has it. |
| **Load / unload** | There's no standard command, so Moontop uses the first the printer has of `LOAD_FILAMENT`, `FILAMENT_LOAD`, `M701` (and `UNLOAD_FILAMENT`, `FILAMENT_UNLOAD`, `M702`, `M603`), then any macro named like one — `QIDI_LOAD_FILAMENT`, say. Override it per printer in setup → Extruder. |
| **Vendor macros** | Everything a vendor adds shows up in the macro browser with its parameters. |
| **Optional Moonraker components** | History, job queue, power devices, update manager and announcements appear only when enabled, and the screen says which section to add when one isn't. Older Moonraker update endpoints are used when the new one isn't there. |

Not yet: Moonraker authentication (API keys, login), file upload and file management, webcams, thumbnails.

---

## Development

```sh
go build -o moontop ./cmd/moontop
gofmt -l . && go vet ./...
go test -race ./...    # everything, against a fake Moonraker
```

Every change ships with tests and, when it changes what you see, docs and a `CHANGELOG.md` entry. `internal/moonraker/moonrakertest` is a fake Moonraker (websocket JSON-RPC and HTTP file endpoints) the tests drive the real model against. [AGENTS.md](AGENTS.md) covers the architecture rules and conventions (for people and coding agents alike), [CONTRIBUTING.md](.github/CONTRIBUTING.md) the PR process, and [docs/RELEASING.md](docs/RELEASING.md) how releases are cut from the changelog.

<details>
<summary><b>Project layout</b></summary>

<br>

```text
cmd/moontop/             the moontop binary
  main.go                dashboard model, Update, View, initialize (one subscribe path)
  keys.go                keymap registry: every key, its scope, its help
  help.go                key routing and the ? overlay
  layout.go              the layout solver, panels, footer
  klippy.go              notification dispatch, Klipper state, e-stop, banner
  toolhead.go            Toolhead pane          fields.go   Tuning + the field widget
  extruder.go filament.go  Extruder, load/unload, levelling
  outputs.go             Outputs                heaters.go  Temperatures targets, presets
  sparkline.go           temperature history    afcpane.go  AFC
  macros.go              Macros pane, browser, forms
  prompts.go             macro prompts, exclude object
  jobs.go machine.go updates.go bedmesh.go logs.go   the full-screen tools
  setup.go settings.go   setup screen, applying and saving settings
  repl.go cli.go         moontop console, gcode, dump
internal/
  moonraker/             JSON-RPC websocket client, typed API, HTTP files
  config/                config.toml and per-printer settings
  console/               line classification, filters, history (all modes)
  afc/                   AFC model and commands
  macros/                macro parameters from templates
  prompt/                action:prompt_* parser
  spark/                 braille sparklines
docs/                    ROADMAP.md, UI-PLAN.md, RELEASING.md; the website (index.html, shots/)
testdata/                printer status fixtures (a U1's AFC-Lite)
```

</details>

<details>
<summary><b>How it fits together</b></summary>

<br>

- **One notification stream.** The client owns the websocket and forwards every Moonraker notification on one channel; the TUI routes them through a dispatch table.
- **One subscribe path.** `initialize` runs at startup, after a reconnect and on `notify_klippy_ready`: identify the connection, check Klipper's state, list objects, send one complete subscribe map.
- **Keys as data.** Every key is declared once with its scope and help; handling, the footer and the help screen all read the same registry.
- **One layout solver.** Pane sizes are computed in one place from the terminal size and your settings.
- **Confirmed state only,** and every send is a non-blocking command whose result comes back as a message.

</details>

---

## Credits

- [Klipper](https://www.klipper3d.org/) and [Moonraker](https://moonraker.readthedocs.io/), and [Mainsail](https://docs.mainsail.xyz/) and [Fluidd](https://docs.fluidd.xyz/), whose behaviour Moontop follows wherever it can.
- [Charm](https://charm.sh/)'s Bubble Tea, Bubbles and Lip Gloss.
- **djsplice** for the braille temperature sparklines, and **jimmyjon711** (AFC) for the lane rules and pointing at [SocketKeys](https://github.com/N3MI-DG/SocketKeys).
- [retrograde](https://github.com/justinh-rahb/retrograde) for the AFC-Lite rules and the U1 fixture.

## Support

If Moontop saves you a trip to the browser, you can [buy me a coffee](https://buymeacoffee.com/wildtang3nt). Bugs and ideas go in [issues](https://github.com/justinh-rahb/moontop/issues); security reports go through [private reporting](https://github.com/justinh-rahb/moontop/security/advisories/new) ([SECURITY.md](.github/SECURITY.md)).

## License

[MIT](LICENSE) © 2026 Justin Hayes
