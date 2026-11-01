# Changelog

All notable changes to Moontop are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Releases are cut from this file: adding a `## [x.y.z] - YYYY-MM-DD` heading on
`main` tags and publishes that version (see [docs/RELEASING.md](docs/RELEASING.md)).

## [Unreleased]

## [0.1.0] - 2026-11-01

The first release: a keyboard-driven terminal UI for Klipper printers,
through Moonraker, covering what you reach for in Mainsail or Fluidd.

### Added

- **Connecting:** a connection manager with your recent printers (the last
  one preselected), or `-host` / `$MOONRAKER_HOST`. Moonraker logins and API
  keys (`$MOONRAKER_API_KEY` too); refresh tokens and keys are kept per
  printer in an owner-only `hosts.json`, passwords never. `https://` for
  printers behind TLS.
- **Dashboard:** Files, Toolhead, Tuning, Temperatures, Console, AFC,
  Extruder, Outputs and Macros panes, laid out by a solver you can reorder
  and resize. Klippy state banner, restart and firmware restart (`F5` /
  `F6`), E-stop from anywhere (`Ctrl+X`).
- **Temperatures:** set targets in place, cooldown, presets (Mainsail's too),
  braille sparkline history backfilled from `server.temperature_store`.
- **Toolhead:** WASD jog, space / `c` for Z, typed absolute moves, homing,
  motors off, endstops, QGL or Z tilt when the printer has it.
- **Tuning and Extruder:** speed and flow, Z-offset babystep, velocity limits,
  tool select, extrude / retract, pressure advance, and filament load /
  unload detected from each printer's own macros.
- **Outputs:** fans, output pins and LEDs, built from your config.
- **AFC:** lanes from `AFC.lanes` with material, colour, weight, load / unload
  / eject.
- **Macros:** a browser of every macro (`F3` / `g`), a pinned Macros pane,
  parameter forms, and macro prompts (`action:prompt_*`).
- **Printing:** pause / resume / cancel, progress and ETA, exclude object
  (`O` / `F4`).
- **Jobs** (`J` / `F7`): a file manager (folders, slicer details, rename,
  copy, move, delete, upload, upload & print through a local file browser),
  print history and totals, and the job queue. The Files pane and the file
  manager follow changes on the printer as they happen.
- **Machine** (`M` / `F8`), **Software** (`U` / `F9`), **Bed mesh** (`B` /
  `F11`, a coloured grid of values, profiles, screws tilt, manual probe),
  **Logs** (`L` / `F12`) and **Config** (`E`, edit in `$EDITOR`).
- **Console:** tab completion with macro parameter hints, filters for
  heat-wait and TMC noise, history.
- **Setup** (`F2`): panes, columns, heaters, filters, jog steps, safety,
  presets and twelve colour schemes, from all-green `phosphor` to your
  terminal's own palette. Settings in `~/.config/moontop/config.toml`,
  per-printer ones in Moonraker's database.
- **Command line:** `moontop console` (an inline gcode console),
  `moontop gcode` (one-shot gcode with exit codes), `moontop upload` /
  `moontop print`, `moontop dump`, `moontop version`.
- Builds for Linux, macOS, Windows and FreeBSD on amd64 and arm64 (and armv7
  on Linux), an install script, and `go install`.

[Unreleased]: https://github.com/justinh-rahb/moontop/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/justinh-rahb/moontop/releases/tag/v0.1.0
