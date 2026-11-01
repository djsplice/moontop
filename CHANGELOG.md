# Changelog

All notable changes to Moontop are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Releases are cut from this file: adding a `## [x.y.z] - YYYY-MM-DD` heading on
`main` tags and publishes that version (see [docs/RELEASING.md](docs/RELEASING.md)).

## [Unreleased]

### Added

- Dashboard with Status, Temperatures, Toolhead, Tuning, Files, Console, AFC,
  Macros, Extruder and Outputs panes, laid out by a configurable layout solver.
- Klippy lifecycle handling: state banner, resubscribe on ready, restart and
  firmware restart (`F5` / `F6`), E-stop, highlighted `!!` errors, console
  backfill.
- `moontop console` (an inline line console) and `moontop gcode` (one-shot
  gcode from arguments or stdin, with exit codes for scripts).
- Keymap registry driving keys, footer and the `?` help overlay; console
  filters for heat-wait and TMC noise.
- Setup screen (`F2` / `,`) with settings in `~/.config/moontop/config.toml`
  and per-printer settings in Moonraker's database.
- Heater targets, cooldown and presets (including Mainsail's) from the
  Temperatures pane, with braille sparkline history backfilled from
  `server.temperature_store`.
- Toolhead: WASD jog, space / `c` for Z, typed absolute moves, homing, motors
  off, endstop query, QGL / Z tilt when the printer has it.
- Tuning and Extruder panes on a shared numeric field widget: speed and flow
  factor, Z-offset babystep, velocity limits, tool select, extrude / retract,
  pressure advance, filament load / unload.
- Outputs pane for fans, output pins and LEDs, built from each object's type.
- AFC pane for lanes from `AFC.lanes`: material, colour, weight, load / unload
  / eject.
- Macros: a browser of every macro (`F3` / `g`), a pinned Macros pane and
  parameter forms.
- Macro prompts (`action:prompt_*`) and exclude object (`O` / `F4`).
- Jobs screen (`J` / `F7`): print history, totals and the job queue.
- Machine screen (`M` / `F8`): host and MCU stats, services, power devices,
  reboot / shutdown.
- Software screen (`U` / `F9`): update manager with streamed output and
  announcements.
- Bed mesh screen (`B` / `F11`): coloured grid of values, profiles, calibrate,
  screws tilt and the manual probe dialog.
- Logs (`L` / `F12`), config files with `$EDITOR` editing (`E`), console tab
  completion with macro parameter hints.
- `moontop version`, release builds for Linux, macOS, Windows and FreeBSD,
  Homebrew cask, Scoop manifest, deb / rpm / apk / Arch packages and an
  install script.
- Project website (GitHub Pages, from `docs/`) with screenshots of every
  screen.

### Changed

- Module path is now `github.com/justinh-rahb/moontop` and the command lives
  in `cmd/moontop`, so `go install github.com/justinh-rahb/moontop/cmd/moontop@latest`
  works.

[Unreleased]: https://github.com/justinh-rahb/moontop/commits/main
