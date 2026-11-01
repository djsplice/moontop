# Moontop — Mainsail Feature-Parity Gap Audit & Phase Plan

Oct 30, 2026 · Justin Hayes

## Summary

Moontop covers about a third of what you need to run the U1 day to day; the biggest gaps are that it can't set a temperature, change tools, extrude, or hit E-stop. Mainsail exposes roughly 35 functional areas; Moontop has 9. Thirteen phases, AFC (13) through 25, close every terminal-appropriate gap.

Five structural findings come before any feature work:

1. **Klipper restarts break the app.** Moontop only handles `notify_status_update` and `notify_gcode_response`; every other notification is dropped. Mainsail re-runs its full subscribe on `notify_klippy_ready`. Moontop's reconnect only fires on a websocket drop, so after `FIRMWARE_RESTART` the panels likely freeze until you relaunch. Verify on the U1.
2. **"Subscriptions are additive" needs a precise reading.** Moonraker's docs say a new `printer.objects.subscribe` request overrides the previous one. The code is correct today because it sends one combined map, but that map is hard-coded inside `initialize()`. Every new panel needs an entry in it, so it needs a registry before more objects arrive.
3. **`mergeStatus` takes one pointer per object type** (sensors, position, job, limits). AFC, fans, extruders, exclude\_object and bed\_mesh would push it past ten parameters. It needs a single printer-state struct with per-object handlers.
4. **There are no tests.** The rules require tests every phase, so Phase 13 must also bring a fake Moonraker server to test against.
5. **`EmergencyStop()` exists in the client but nothing calls it.**

I couldn't reach the U1 at 10.0.3.207 from this workspace (its network allowlist blocks your LAN), so U1-specific object names are unverified. Phases that depend on them start with a discovery dump.

## What exists today

The codebase is 2,769 lines across five Go files, at commit `8bc9ce8` ("Moontop" rename after Phase 12).

| File | Lines | Holds |
| --- | --- | --- |
| `cmd/tui/main.go` | 1,520 | Model, Update, View, every pane, every send Cmd |
| `internal/moonraker/client.go` | 437 | Websocket lifecycle, read loop, reconnect, notification dispatch |
| `cmd/tui/layout.go` | 429 | `computeLayout`, `renderPanel`, footer, ETA formatting |
| `internal/moonraker/api.go` | 215 | Typed RPC methods, remembered subscribe map |
| `cmd/moonraker-test/main.go` | 101 | Phase 1 smoke harness |
| `cmd/tui/theme.go` | 67 | Colors and styles |

**RPCs used (10):** `printer.objects.list`, `.subscribe`, `.query`, `printer.gcode.script`, `printer.emergency_stop` (unused), `server.files.list`, `printer.print.start` / `.pause` / `.resume` / `.cancel`. Mainsail uses about 75.

**Notifications handled (2 of 26):** `notify_status_update`, `notify_gcode_response`.

**Objects subscribed:** heaters and temperature sensors (temperature, target only), `toolhead` (position and velocity limits), `print_stats` (state, filename, duration), `virtual_sdcard` (progress).

**Panes:** Files, Toolhead, Tuning, Temperatures (read-only), Console, plus a footer with job state, progress, ETA and connection status.

**Tests:** none.

## Gap table

Forty gaps, sorted by priority. P0 blocks safe daily use, P1 is daily convenience, P2 is weekly, P3 is occasional. Effort: S is under a day, M is a few days, L is a week or more.

| Feature | Mainsail has | Moontop has | Terminal fit | Effort | Priority | Phase |
| --- | --- | --- | --- | --- | --- | --- |
| Resubscribe on `notify_klippy_ready` | Re-runs full init | No | Yes | S | P0 | 14 |
| Klippy state banner (startup, shutdown, error + message) | KlippyStatePanel | No | Yes | S | P0 | 14 |
| Klipper restart / firmware restart | Buttons | No | Yes | S | P0 | 14 |
| Emergency stop | Top bar + optional confirm | API method only | Yes | S | P0 | 14 |
| Set heater targets | Inline target per row | Read-only | Yes | S | P0 | 15 |
| Tool select T0–T3 | Extruder panel tools | No | Yes | S | P0 | 16 |
| Extrude / retract (amount, speed) | Extruder panel | No | Yes | S | P0 | 16 |
| AFC lanes (load, unload, eject, material, weight) | AfcPanel + dialogs | No | Yes | L | P0 | 13 |
| Preheat presets + cooldown | TemperaturePanelPresets | No | Yes | S | P1 | 15 |
| Speed factor / flow (M220 / M221) | Toolhead panel | No | Yes | S | P1 | 16 |
| Z-offset babystep | ZoffsetControl | No | Yes | S | P1 | 16 |
| Fans, LEDs, output pins | MiscellaneousPanel | No | Yes (LED color as RGB fields) | M | P1 | 17 |
| Macros with parameters | MacrosPanel, macro groups | Console only | Yes | M | P1 | 18 |
| Macro prompts (`action:prompt_*`) | TheMacroPrompt | No | Yes | M | P1 | 19 |
| Exclude object | List + map dialog | No | Yes (list; map optional) | M | P1 | 19 |
| Klipper error lines highlighted | Console styling | No | Yes | S | P1 | 14 |
| File metadata (time, filament, slicer) | Table columns | Size only | Yes | S | P1 | 20 |
| File list refresh on change | `notify_filelist_changed` | Loaded once | Yes | S | P1 | 20 |
| Subdirectories | Yes | Flat root | Yes | M | P1 | 20 |
| Delete / rename / move / new folder | Dialogs | No | Yes | M | P1 | 20 |
| Restart services, reboot / shut down host | Top-corner menu | No | Yes | S | P1 | 22 |
| Print history + totals | History page | No | Yes | M | P2 | 21 |
| Job queue | JobqueuePanel | No | Yes | M | P2 | 21 |
| Host stats (CPU, memory, disk, throttling), MCU info | SystemPanel | No | Yes | M | P2 | 22 |
| Power devices | Top-corner menu | No | Yes | S | P2 | 22 |
| Update manager | UpdatePanel | No | Yes | M | P2 | 23 |
| Announcements | Bell menu | No | Yes | S | P2 | 23 |
| Bed mesh profiles (load, save, remove, calibrate) | Heightmap page | No | Yes | M | P2 | 24 |
| Bed mesh view | 3D chart | No | Partial (ASCII heat grid) | M | P2 | 24 |
| Screws tilt / manual probe helpers | Dialogs | No | Yes | M | P2 | 24 |
| Pressure advance, extrusion factor, firmware retraction | Extruder settings | No | Yes | S | P2 | 16 |
| Console backfill (`server.gcode_store`) | On connect | No | Yes | S | P2 | 14 |
| Log viewer (klippy.log, moonraker.log) | LogfilesPanel | No | Yes (tail view) | M | P3 | 25 |
| Config files | Editor | No | Partial (view + `$EDITOR` handoff) | M | P3 | 25 |
| Gcode command autocomplete | `printer.gcode.help` | No | Yes | S | P3 | 25 |
| Endstop query, move-to, per-axis home, motors off | Toolhead / Machine | Home all only | Yes | S | P3 | 25 |
| Live object browser: filter, expand, watch any object update, copy a `printer.<object>.<field>` path for macros (idea from SocketKeys, see below) | No (Fluidd neither) | `moontop dump` only | Yes | M | P2 | 26 |
| Parameter hints for macros in console completion, parsed from `gcode_macro` Jinja (`params.X`), plus a bundled reference for Klipper's built-ins (SocketKeys) | No | Command names only | Yes | M | P3 | 26 |
| Log tail with errors, warnings and tracebacks coloured; trigger a log rollover (SocketKeys) | LogfilesPanel (download) | No | Yes | S | P3 | 25 |
| File upload | Drag-drop | No | Partial (local path) | M | P3 | Later |
| Spoolman | SpoolmanPanel | No | Yes | M | P3 | Later |
| Timelapse status and toggles | Timelapse panels | No | Partial | S | P3 | Later |
| Temperature history graph | Live chart | No | Partial (sparkline) | M | P3 | Later |

Already at parity: job start / pause / resume / cancel, progress and ETA, XYZ jog with step sizes, home all, velocity limits, console with history, reconnect. Not counted as gaps: anything listed under Excluded below.

## Phase plan

Thirteen phases, each one working artifact plus one architectural concept. P0 work lands by Phase 16; after Phase 20 the browser is only needed for P2/P3 tasks.

| Phase | Artifact | Architectural concept | Effort |
| --- | --- | --- | --- |
| 13 | AFC panel: units, lanes (material, color, weight, loaded, mapped tool), active lane, load / unload / eject (eject confirms), AFC message line | **Object registry and printer state.** A registry each pane adds its objects to; one resent subscribe map; `mergeStatus` becomes a `printerState` struct with per-object handlers; dynamic objects (`AFC_lane <name>`, `AFC_hub`, `AFC_extruder`). Also the fake Moonraker test server every later phase uses | L |
| 14 | Klippy lifecycle and safety: state banner, resubscribe on ready, restart / firmware restart, E-stop key, `!!` errors highlighted, console backfill, `server.connection.identify` | **Generic notification routing.** The client exposes one channel of method + params; one listener; a dispatch table in Update instead of a channel per notification type | M |
| 14.5 | Line console and one-shot gcode: `moontop console` (inline prompt, persistent history, completion, Klippy status, E-stop) and `moontop gcode` (args or stdin, exit codes for scripts). Added from user feedback: "interactive access to the printer console from a basic terminal window" | **Inline programs and shared frontends.** `tea.Println` scrollback with `tea.Sequence` for ordering; line classification, history and backfill moved to `internal/console` so all three modes share them | S |
| UI-1 | Discoverability: visible cursor in every pane, `1`–`5` pane jumps, `?` help overlay, footer from the keymap, console filters (heat-wait and TMC current reports hidden by default), console redundancy removed. See [UI-PLAN.md](UI-PLAN.md) | **Keymap as data.** One registry drives key handling, footer and help | M |
| UI-2 | `F2` / `,` setup screen; show/hide panes and objects; renames; custom console filters; settings in `~/.config/moontop/config.toml` and Moonraker's database | **Config as model state** | M |
| UI-3 | Pane order and sizes, configurable table columns, a layout that handles any set of panes | **A generic layout solver** | M |
| 15 | Heater control: set targets from the temperature table, presets, cooldown | **Inline editing inside a table**, plus reading shared settings from the Moonraker database (presets stored by Mainsail, if present) | M |
| 16 | Tool and live-print adjustments: T0–T3 select, extrude / retract, speed factor, flow, Z-offset babystep, pressure advance | **A reusable numeric stepper widget**, shared with Tuning and later panes | M |
| 17 | Fans, LEDs, output pins | **Capability-driven rendering.** Controls built from object type and config (`fan`, `fan_generic`, `led`, `neopixel`, `output_pin` with PWM or not) rather than hard-coded rows | M |
| 18 | Macros panel with parameter forms | **Forms generated from data.** Params parsed from `configfile.settings`; the Tuning form becomes one instance of the same builder | M |
| 19 | Macro prompts and exclude object | **A modal layer.** `pendingConfirmation` grows into a modal stack with N buttons and lists; existing y/n flows move onto it | M |
| 20 | Files v2: subdirectories, metadata columns, delete / rename / move / new folder, live refresh | **Event-driven refresh with a navigation stack**, driven by `notify_filelist_changed` | M |
| 21 | Jobs screen: history list, totals, job queue | **Multiple screens.** A screen router beside the dashboard; the footer and global keys stay shared | M |
| 22 | Machine screen: host and MCU stats, services, reboot / shutdown, power devices | **Push vs poll.** `notify_proc_stat_update` push mixed with `tea.Tick` refresh for one-shot endpoints | M |
| 23 | Updates and announcements | **Long-running operations with streamed progress** (`notify_update_response` into a log view) | M |
| 24 | Bed mesh: profiles, calibrate, ASCII heat grid; screws tilt and manual probe | **Rendering a 2D numeric field** in a terminal | M |
| 25 | Logs, config viewer with `$EDITOR` handoff, gcode autocomplete, endstops, move-to | **Suspending the TUI** for an external process (`tea.ExecProcess`) | M |
| 26 | Debug tools: an object browser pane over every Klipper object (live, filterable, copy the Jinja path) and macro parameter hints in the console. Suggested by jimmyjon711 (AFC) pointing at [SocketKeys](https://github.com/N3MI-DG/SocketKeys), a Tauri debugging client for Moonraker | **A generic tree view over the raw object store** that AFC already uses; subscribing to everything on demand and unsubscribing when the pane closes | M |

Phases 13 and 15–17 depend on U1 object names. Before Phase 13, run this from a machine on your LAN and save the output into the repo as test fixtures:

```
curl -s http://10.0.3.207:7125/printer/objects/list > testdata/objects.json
curl -s "http://10.0.3.207:7125/printer/objects/query?AFC&toolhead&print_stats" > testdata/afc_query.json
```

**Done:** 14, 14.5, UI-1, 15 (heater targets in place, cooldown, Mainsail presets; pulled ahead of UI-2 on 2026-10-31), UI-2 (setup screen, `config.toml`, per-printer settings in Moonraker's database, Moontop presets) and UI-3 (layout solver: pane order, width shares, console height; Temperatures and Files columns, including heater Power). Still open from the UI plan: the Display category (ASCII glyphs, compact borders) and editable keys. Phase 13 (AFC) is done: a pane beside the console, built from Fluidd's AFC model plus retrograde's AFC-Lite rules, and tested against a sanitized U1 AFC-Lite status (testdata/afc-lite-u1.json, from retrograde). Not yet exercised on real AFC hardware or with live load/unload on the U1. AFC lanes now come only from `AFC.lanes` (jimmyjon711: a ViViD's drive and selector steppers are not lanes). Toolhead rework (2026-10-31, Justin's idea): WASD jogs XY, space/c jump and crouch Z, arrows move a field cursor for typed absolute positions and custom steps; moves are refused while printing, on unhomed axes and outside axis limits; a pane key may now shadow a job key while focused (c in Toolhead). Phase 16 is done: a shared numeric field widget (↑↓ row, ←→ adjust at once, type + enter) behind Tuning, now speed factor, flow, Z offset babystep and the velocity limits, and a new Extruder pane (7, bottom row) with tool select from the printer's T<n> commands, extrude / retract, pressure advance and smooth time; extrusion respects can_extrude. Not yet tried on the U1. Phase 17 is done: an Outputs pane (8) under Extruder whose rows come from each object's type (part fan, fan_generic, heater/controller fans read-only, output_pin PWM / digital / scaled from configfile, LEDs with brightness and hex colour), on the same field widget. Not yet tried on the U1. Phase 18 is done: macros read from the printer (internal/macros parses descriptions and template parameters), a browser of all of them (F3 / g: filter, run, pin), a Macros pane (9, under AFC) of the pinned ones only, pins stored per printer in Moonraker's database, and a parameter form that sends only filled fields with gcode delimiters refused. The Tuning form did not move onto a form builder: Phase 16's field widget already covers it. Added from Justin's use on the U1 and a Qidi Q2: filament load / unload in Extruder (detected per printer from common macro names, overridable in setup → Extruder) and gantry levelling in Toolhead (QGL or Z tilt, only when the printer has it, with its applied state). Phase 19 is done: macro prompts (internal/prompt parses action:prompt_* from live output and history; a full-screen dialog with buttons, esc sends prompt_end) and exclude object (O / F4 during a print: the labelled objects, current and excluded, enter excludes after a confirmation). The planned modal stack wasn't needed: prompts and the object list are screens like setup and the macro browser, and y/n confirmations still work over them. Phase 20 (Files v2) skipped for now: Justin finds the Files pane fine as it is. Phase 21 is done: a Jobs screen (J / F7) with print history, totals and the job queue (reprint, queue, delete record; start / pause the queue, remove; a in Files queues a file), live via notify_history_changed / notify_job_queue_changed, and a clear message when [history] or [job_queue] isn't enabled. Phase 22 is done: a Machine screen (M / F8) with host and Moonraker stats pushed live by notify_proc_stat_update, MCU stats from the mcu objects, services (restart, stop / start) and power devices (switch, respecting locked_while_printing), host reboot / shutdown; every action confirms. Added from djsplice on Discord: braille temperature sparklines in Temperatures (internal/spark; backfilled from server.temperature_store, sampled at 1 Hz; the Temperature history graph gap, as a sparkline). Phase 23 is done: a Software screen (U / F9) with the update manager (versions, commits behind, update one or all, streamed notify_update_response output, the per-kind endpoints as a fallback for older Moonraker) and announcements (dismiss), plus a title badge for what's waiting. Next: Phase 24 (bed mesh: profiles, calibrate, ASCII heat grid; screws tilt, manual probe).

UI-1 to UI-3 come before Phase 13 (AFC), decided 2026-10-31: AFC adds a pane and many per-lane objects that need the same show/hide and selection machinery.

Every phase keeps the existing rules: listener Cmds reissue themselves, sends are non-blocking `tea.Cmd`s, display comes from confirmed state, confirm-worthy actions go through the shared confirmation (the modal layer after Phase 19), and `go build`, `go vet` and tests stay green. New panes go in their own files; existing code moves only when a phase needs it.

## Excluded and open questions

**Excluded:** webcam, gcode 3D viewer, thumbnails, multi-printer farm view, theming and settings editors, auth, drag-drop upload (per the brief), plus MMU / Happy Hare and LED effects panels, which the U1 doesn't use.

**Decisions needed before Phase 13:**

- [x] Pull Phase 14 ahead of AFC as a hotfix? The resubscribe-on-ready gap is a correctness bug and E-stop is a safety item. My recommendation is yes, keeping AFC's number. **Decided: yes — Phase 14 shipped first.**
- [x] E-stop binding: a single chord with no confirmation (fast, risk of accidental press) or a key plus `y`? Mainsail makes the confirm optional. **Decided: key plus `y` (`ctrl+x`, then `y`).**
- [ ] Presets: reuse the presets Mainsail already stores in Moonraker's database, or keep Moontop's own?
- [ ] Multiple screens from Phase 21: acceptable, or keep everything on one dashboard?
- [ ] Run the discovery dump above and commit the fixtures.

## Sources

- Mainsail source, `develop` branch: [panels and dialogs](https://github.com/mainsail-crew/mainsail/tree/develop/src/components), [notification handling](https://github.com/mainsail-crew/mainsail/blob/develop/src/store/socket/actions.ts), [subscribe on init](https://github.com/mainsail-crew/mainsail/blob/develop/src/store/printer/actions.ts)
- Moonraker docs: [printer API](https://moonraker.readthedocs.io/en/latest/external_api/printer/), [JSON-RPC notifications](https://moonraker.readthedocs.io/en/latest/external_api/jsonrpc_notifications/)
- Moontop at commit `8bc9ce8`
