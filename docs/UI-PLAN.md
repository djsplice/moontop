# Moontop — Controls & Configurability Plan

Oct 31, 2026 · Justin Hayes

> UI-1 and UI-2 have shipped (see the roadmap's Done line for what UI-2 deferred); UI-3 is next. The "What's wrong today" and "Every control today" sections describe the app before UI-1.

## Summary

Three UI phases (UI-1 to UI-3) make every control discoverable and the whole interface configurable, htop-style. I recommend doing them before AFC (Phase 13), because AFC adds a sixth pane and many per-lane objects that need the same show/hide and selection machinery.

- **UI-1 Discoverability:** every pane gets a visible cursor, a `?` help screen lists every key, and number keys jump straight to a pane. All three come from one keymap registry, so the footer, help and later key remapping can't drift apart. Also fixes the redundancy you spotted and hides heat-wait temperature lines like Mainsail does.
- **UI-2 Setup screen and config:** an `F2` setup screen (htop's model) to show or hide panes, heaters, sensors and console lines, rename objects, and set options. Settings persist per printer.
- **UI-3 Layout and columns:** the layout is computed from the configured pane list (order, visibility, size) instead of today's hard-coded arrangement, and table columns become configurable.

The root cause today is that keys are hard-coded in six separate `switch` statements, and the footer hints are hand-written strings beside them. Nothing lists them all, and nothing checks that the hints match what the keys do.

## What's wrong today

Only the Files pane shows where you are; the other panes either have no cursor or hide it. Findings from the code at `a653a92` and your U1 session:

| Where | Problem | Cause |
| --- | --- | --- |
| Temperatures | Arrow keys appear to do nothing; no row is highlighted | The table is created with `WithFocused(false)` and is never focused, so it ignores keys and draws no cursor |
| Toolhead | No cursor at all; actions are keys you have to know (arrows, PgUp/PgDn, `[` `]`, `H`) | The pane has no selectable controls, only key handlers |
| Tuning | A `▸` marks the field, but nothing says up/down moves it | Footer hint only shows when the pane is focused |
| All panes | No way to see every key; `p`, `r`, `c`, `K`, `F` only appear when they apply; Tab is the only way between panes | Keys live in six `switch` statements; hints are separate hand-written strings |
| Console mode | Host shown twice (`── live · host ──` and the status line); quit shown twice (placeholder and status line) | Each was added in isolation |
| Console, both modes | Heat-wait reports (`B:26.3 /0.0 T0:168.1 /140.0`) flood the console during `M109`/`M190` waits | No filtering. Mainsail hides these by default (`hideWaitTemperatures`, pattern `^(?:ok\s+)?(B\|C\|T\d*):`) |
| Everything | Nothing can be hidden, reordered, renamed or resized; every sensor on the U1 shows whether you care or not | No config layer exists; layout is hard-coded for the current five panes |

## Every control today

The dashboard has about 30 key bindings across 8 scopes. This table is what UI-1's help screen will show, generated from the registry rather than written by hand.

| Scope | Key | Action | Shown in footer? |
| --- | --- | --- | --- |
| Global | `Tab` | Next pane (Files → Toolhead → Tuning → Temps → Console) | Yes |
| Global | `ctrl+x` | Emergency stop (confirms) | Yes |
| Global | `ctrl+c` | Quit | Yes |
| Global, not in console | `q` | Quit | No |
| Global, not in console | `p` / `r` / `c` | Pause / resume / cancel print (cancel confirms) | Only while a job is active |
| Global, not in console | `K` / `F` | Restart Klipper / firmware restart (confirm) | Only in the Klipper banner |
| Confirmation | `y` / `n` / `esc` | Confirm / cancel | Yes, in the prompt |
| Files | `↑` `↓` | Select file | Yes |
| Files | `/` | Filter | Yes |
| Files | `enter` | Start print (confirms) | Yes |
| Toolhead | `←` `→` / `↑` `↓` | Jog X / Y by the step | Yes |
| Toolhead | `PgUp` / `PgDn` | Jog Z | Yes |
| Toolhead | `[` / `]` | Step size down / up | Yes |
| Toolhead | `H` | Home all | Yes |
| Tuning | `↑` `↓` | Move between fields | Yes |
| Tuning | type, `enter` | Edit value, apply changed fields | Yes |
| Temperatures | `↑` `↓` | Select row (broken: the table is never focused) | Yes, but does nothing |
| Console pane | type, `enter` | Send gcode | Yes |
| Console pane | `↑` `↓` | History | No |

The line console (`moontop console`) adds `Tab`, `ctrl+n` and `ctrl+p` (completion), `ctrl+c` (clear line or quit), `ctrl+d` and `exit` (quit).

Two gaps stand out: `q` and history are never advertised, and the Temperatures hint advertises a key that does nothing.

## Design

### Keymap registry

Every action becomes data: an id (`job.pause`), its keys, a short help line, a scope (global, a pane, a modal) and a condition for when it applies (`job.state == printing`). Key handling looks actions up in the registry instead of `switch`ing on strings. The footer, the help screen and the setup screen's Keys page all render from the same registry, so a key can't exist without a hint or a hint without a key. Built on `bubbles/key` and `bubbles/help`, which are already in the dependency tree.

### One navigation model for every pane

- Every pane has a visible cursor on something selectable: a file, a heater row, a tuning field, a jog control, a console line.
- `↑`/`↓` (or `k`/`j`) move the cursor, `enter` acts on or edits the item, `space` toggles, `esc` backs out.
- `Tab` / `shift+Tab` cycle panes; `1`–`9` jump straight to a pane, numbered in its title (`1 Files`, `2 Toolhead`…).
- Toolhead keeps its arrow keys for jogging, so instead of a cursor every action is printed in the pane with its key (`H home`, `x y z` per-axis home, `m motors off`, `[ ]` step), and nothing has to be memorised. Built in UI-1.
- Temperatures gets a working cursor now. Setting a target on the selected row is Phase 15, which this unblocks.

### Help screen

`?` (or `F1`) opens a full-screen overlay listing every action, grouped by scope, with the ones that apply right now highlighted. Inactive actions show why ("only while printing"). `esc` closes it. The footer shrinks to the five most useful keys for the focused pane plus `? help`.

### Setup screen

`F2` (or `,`, since laptop F-keys need `fn`) opens a setup screen modelled on htop's: categories on the left, the selected category's items on the right, changes apply live.

```
╭ Setup ─────────────────────────────────────────────────────────────────╮
│ ▸ Panes          │  [x] 1 Files          ← → reorder   width 1/3     │
│   Temperatures   │  [x] 2 Toolhead                                    │
│   Console        │  [ ] 3 Tuning                                      │
│   Files          │  [x] 4 Temperatures                                │
│   Toolhead       │  [x] 5 Console        height 40%                   │
│   Safety         │  [x] AFC lanes   (when AFC objects exist)          │
│   Display        │                                                    │
│   Keys           │                                                    │
├──────────────────┴──────────────────────────────────────────────┤
│ ↑↓ select  space toggle  ←→ move  enter edit  esc done  saved ✓  │
╰────────────────────────────────────────────────────────────────╯
```

| Category | Settings |
| --- | --- |
| Panes | Show/hide, order, relative size (UI-2 show/hide; UI-3 order and size) |
| Temperatures | Show/hide each heater, sensor and temperature fan; rename (`extruder1` → `T1 ASA`); columns: state, current, target, power |
| Console | Hide heat-wait reports (on by default), hide bare `ok`, custom regex filters, scrollback length |
| Files | Columns (size, modified, estimated time, filament), sort order |
| Toolhead | Step sizes, XY and Z feedrates, which home/motor buttons show |
| Safety | Confirm restarts when idle (on), confirm E-stop (on, per your earlier decision) |
| Display | Unicode or ASCII glyphs, compact borders, title glyph |
| Keys | Read-only list from the registry in UI-2; remapping later |

### Config model and storage

One `Config` struct with defaults in code; the file stores only what you changed, so new defaults reach you on upgrade. Config is model state: the setup screen edits it through ordinary messages and every pane re-renders from it, with no restart. Settings are kept per printer, keyed by host, with a shared default section.

Storage is a decision (below). Mainsail keeps its settings in Moonraker's database under its own namespace, so they follow the printer to any browser. Moontop could do the same under a `moontop` namespace, keep a local file, or both.

### Layout from config

Today `computeLayout` knows exactly five panes in two fixed arrangements. UI-3 replaces that with a small solver: rows of panes from the config, each with a weight and a minimum size, falling back to stacking below a width threshold. Adding the AFC pane then becomes one config entry rather than new layout code.

### Console filters

Filters live in `internal/console`, so the dashboard console, `moontop console` and `moontop gcode` share them. Filtered lines are counted, not lost: the console shows `12 lines hidden` and a key reveals them.

## Phase plan

The proposed order is UI-1 → UI-2 → UI-3 → 13 (AFC) → 15 onward. UI-1 fixes what confused you today and is small enough to ship first on its own.

| Phase | Artifact | Architectural concept | Effort |
| --- | --- | --- | --- |
| UI-1 | Visible cursor in every pane (Temperatures fixed, Toolhead actions labelled on screen, multi-extruder heaters listed), `1`–`9` pane jumps and `shift+Tab`, `?` help overlay, footer trimmed to the focused pane's top keys, console redundancy removed, heat-wait filter on by default in the dashboard and `moontop console` | **Keymap as data.** One registry of actions (id, keys, help, scope, when-active) drives key handling, footer and help | M |
| UI-2 | `F2` / `,` setup screen; show/hide panes and objects; rename objects; console filters incl. custom regex; Safety and Toolhead options; settings saved per printer | **Config as model state.** Defaults in code, overrides persisted; the setup screen edits config through messages and panes re-render live | M |
| UI-3 | Pane order and sizes from config; configurable Temperatures and Files columns; the layout handles any set of panes, ready for AFC | **A generic layout solver** replacing the fixed wide/stacked arrangements | M |

Each phase keeps the existing rules: tests every phase, sends as non-blocking Cmds, display from confirmed printer state, confirmations through the shared prompt.

Knock-on effects on the existing roadmap:

- Phase 13 (AFC) becomes a pane registered in config, with lane objects shown or hidden like heaters.
- Phase 15 (heater control) needs UI-1's working Temperatures cursor: select a row, `enter` to set its target.
- Phase 16's stepper widget and Phase 18's form builder can reuse the setup screen's editors.
- Phase 21 (multiple screens) is already half there: help and setup are full-screen overlays on the same router.

## Decisions needed

- [x] **Order:** UI-1 to UI-3 before AFC? **Decided: yes, all three before AFC.**
- [x] **Where settings live:** **Decided: both.** Local `~/.config/moontop/config.toml` for display preferences; Moonraker's database (`moontop` namespace) for per-printer settings such as renames and hidden objects, winning over the file.
- [x] **File format:** **Decided: TOML.**
- [x] **Keys:** **Decided: htop-style `F1`/`F2`/`F10` plus letter aliases (`?`, `,`, `q`), and `j`/`k` for down/up.**
- [x] **Key remapping:** **Decided: later.** The Keys page is read-only in UI-2.
- [x] **Console filters:** **Decided: heat-wait temperature reports and `Run Current:` lines hidden by default** in the dashboard and `moontop console`; `moontop gcode` stays raw unless given `-filter`.
