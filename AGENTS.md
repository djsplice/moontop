# AGENTS.md

Notes for anyone (human or coding agent) changing Moontop: how to build, run
and test it, how the code is put together, and the rules every change follows.
`CLAUDE.md` just points here.

## What this is

Moontop is a terminal UI for Klipper printers, talking to
[Moonraker](https://moonraker.readthedocs.io/) over its JSON-RPC websocket and
HTTP file API. It aims at feature parity with Mainsail / Fluidd in a terminal.
Go, [Bubble Tea](https://github.com/charmbracelet/bubbletea) and
[Lip Gloss](https://github.com/charmbracelet/lipgloss). No cgo.

## Build, run, test

Needs the Go version in `go.mod`.

```sh
go build -o moontop ./cmd/moontop      # build
./moontop -host 192.168.1.50:7125      # dashboard (or set MOONRAKER_HOST)
./moontop console -host …              # inline line console
./moontop gcode -host … G28 "M117 hi"  # one-shot gcode
./moontop dump -host … -prefix AFC     # printer objects as JSON (fixture format)
./moontop version

gofmt -l .                              # must print nothing
go vet ./...
go test -race ./...                     # the whole suite; must pass before any commit
go test ./cmd/moontop -run TestAFC -v   # one area
```

CI (`.github/workflows/ci.yml`) runs gofmt, `go mod tidy` drift, vet, the race
tests on Linux, macOS and Windows, `goreleaser check` and a snapshot build. Keep
tests portable: no Unix-only paths, permissions or signals in tests.

There is no printer in CI. Tests run against **`internal/moonraker/moonrakertest`**,
a fake Moonraker:

```go
srv := moonrakertest.New(t)                       // websocket + HTTP, closed with the test
srv.Handle("printer.objects.list", moonrakertest.Result(map[string]any{"objects": objs}))
srv.HandleHTTP("/server/files/", handler)         // file API
m := readyModel(t, srv)                            // a dashboard model connected and Klippy-ready
m = press(t, m, "6", "l")                          // keys
m = update(t, m, notificationMsg{...})             // any tea.Msg, running returned Cmds
calls := srv.WaitForCall("printer.gcode.script", 1, time.Second)
```

Methods without a handler answer `"ok"`, so a test that depends on a result
must register one. To simulate a Moonraker without a component (no `[history]`,
no `[update_manager]`), make the handler return a "Method not found" error;
Moontop turns that into a "not enabled" message.
Real-printer fixtures live in `testdata/` (sanitized `moontop dump` output).

Lip Gloss renders no colour in tests, so test the function that picks a style
(e.g. a classifier returning a level) rather than ANSI codes in `View()`.

## Layout

```
cmd/moontop/            the program: dashboard model, panes, screens, CLI modes
  main.go               model, Update, View, pendingConfirmation
  keys.go               keymap registry (keys, footer and help all come from it)
  layout.go             the layout solver: every pane size is decided here
  theme.go              shared colours and focus styles
  cmds.go               tea.Cmd wrappers around Moonraker calls
  <pane>.go             one file per pane or screen (toolhead, extruder, jobs, …)
  cli.go, repl.go       `console`, `gcode`, `dump`, `version` subcommands
  version.go            version info (set by -ldflags in releases)
cmd/moonraker-test/     raw client harness: prints status updates from a real printer
internal/moonraker/     websocket client, typed API calls, notification names
internal/moonraker/moonrakertest/   fake Moonraker for tests
internal/config/        config.toml and per-printer settings
internal/console/       line classification, filters, history, backfill
internal/afc, macros, prompt, spark, moo   self-contained helpers with their own tests
docs/                   ROADMAP.md (phase plan and status), UI-PLAN.md, RELEASING.md,
                        and the GitHub Pages site (index.html, site.css, site.js, shots/)
```

## Rules the code follows

These have held since the first phase; keep them.

- **Sends never block.** Every Moonraker call is a `tea.Cmd`; nothing in
  `Update` or `View` waits on the network.
- **Listeners reissue themselves.** The notification listener Cmd returns a msg
  and is reissued; subscriptions are added to, never torn down and reopened.
- **One notification path.** The client exposes method + params on one channel;
  `Update` dispatches on the method name (constants in `internal/moonraker/notify.go`).
- **Display comes from confirmed printer state**, never from optimistic local
  updates. A jog shows the new position when Klipper reports it.
- **Every key lives in the registry** (`keys.go`) with its scope and help text.
  Don't match raw key strings in a pane; add a binding.
- **Dangerous actions confirm** through the shared `pendingConfirmation`, not
  per-feature flags.
- **Layout is computed in one place** (`layout.go`). Panes render into the size
  they're given and never measure the terminal themselves.
- **Shared widgets over copies**: numeric fields use `fields.go`; colours and
  focus borders come from `theme.go`.
- **Printer differences are detected, not assumed.** Check `printer.objects.list`,
  `configfile.settings` or `printer.gcode.help` before offering a feature
  (QGL vs Z tilt, load / unload macro names, AFC). Missing Moonraker components
  get a plain "not enabled" message, not an error.
- New panes and screens go in their own files; existing code moves only when
  a change needs it.

## Every change

- **Comes with tests** that fail without it: key handling, the gcode or
  RPC it sends, and what the view shows.
- **Comes with docs** when it changes what a user sees or does: `README.md`
  (keys, screens, install), `docs/ROADMAP.md` status, and a line in
  `CHANGELOG.md` under `## [Unreleased]`.
- **Is gofmt'd, vetted and green** under `go test -race ./...`. Never commit
  with a failing test, not even "to fix next".
- **Commit messages**: a short imperative or "Area: what changed" subject
  (`Toolhead: echo M84 to the console`, `Phase 24: bed mesh grid`), a blank
  line, then a body saying why when it isn't obvious.
- Don't bump versions or add `## [x.y.z]` headings in `CHANGELOG.md`: a
  versioned heading on `main` **publishes a release** (see `docs/RELEASING.md`).

## Gotchas

- Klipper is silent when gcode succeeds. Commands sent from keys are echoed to
  the console (`>>> M84`) so the user sees something happened.
- After an E-stop, Klippy is in `shutdown` and most keys do nothing; `F5` / `F6`
  (restart / firmware restart) work from anywhere, including the console.
- AFC lanes come only from `AFC.lanes`; drive or selector steppers are not lanes.
- `server.temperature_store` and `server.history.*` may be missing on minimal
  Moonraker installs.
- `install.sh` is served from the website as `docs/install.sh`
  (justinh-rahb.github.io/moontop/install.sh). Edit the top-level one and copy
  it over; CI fails if they differ.
- The README screenshot is a real 150-column render; regenerate it rather
  than editing it by hand when the layout changes.
