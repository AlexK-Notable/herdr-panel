# herdr-panel

Spec-driven popup panels for [herdr](https://herdr.dev) plugins — and for
any terminal.

> **Status: under construction (v0).** The build follows
> [`docs/PLAN.md`](./docs/PLAN.md); `spike/` holds the experiments that
> established, on a real herdr, what a popup panel can rely on.

## Why

herdr's own surfaces are closed to plugins. Its right-click menus and its
settings panel are hard-coded, the sidebar cannot be drawn into, and the
manifest's `contexts` field is read by nothing. What a plugin *can* open
is a popup: a session-modal terminal that runs one process and closes
when that process exits.

Every plugin that wants settings, a list of things to act on, or a
confirm button therefore builds its own TUI inside that popup, in its own
language — the plugins on one machine here are written in Python,
TypeScript, Rust and Go. herdr-panel is meant to be written once instead:

- **a host binary** that draws a panel from a **spec file** — settings
  fields, lists with row actions, buttons — with the mouse and the
  keyboard;
- **a Go library** underneath, for panels that need a custom view.

A plugin supplies a spec and, where it needs them, a few commands that
print JSON or perform an action. A panel that only edits a config file
needs no plugin code at all, in any language.

herdr-panel is an unofficial community project. It is not affiliated
with, or endorsed by, the herdr project.

## What a panel will look like

A spec for the settings of a naming plugin, plus a list of workspaces
with an action on each row:

```toml
spec = 1
title = "Auto Title"
size = { width = "70%", height = "60%" }

[stores.config]
kind  = "envfile"
path  = "~/.config/herdr-auto-title/config.env"
apply = { run = ["herdr", "plugin", "action", "invoke", "restart", "--plugin", "herdr.auto-title"] }

[[section]]
title = "Naming"
fields = [
  { store = "config", key = "HERDR_AUTO_TITLE_AGENTS", type = "bool", label = "Name agents" },
  { store = "config", key = "HERDR_AUTO_TITLE_NAMER", type = "choice", label = "Model via",
    choices = [ { value = "cli", label = "claude -p" }, { value = "api", label = "Anthropic API" } ] },
  { store = "config", key = "HERDR_AUTO_TITLE_WORKSPACE_INTERVAL_S", type = "duration_s", min = 30,
    label = "Workspace re-ask" },
]

[[section]]
title   = "Workspaces"
kind    = "list"
source  = ["./herdr-auto-title", "panel", "workspaces"]   # prints JSON rows
columns = ["label", "locked"]
row_actions = [
  { label = "Rename now", run = ["./herdr-auto-title", "workspace", "rename-now"] },
]
```

The plugin's manifest then declares the popup and an action that opens
it, and the user binds a key:

```toml
# herdr-plugin.toml
[[panes]]
id = "settings"
title = "Auto Title"
placement = "popup"
command = ["./bin/herdr-panel", "panel.toml"]
```

The spec format is a sketch. It freezes at version 1 only once it serves
two real plugins — one of ours and one written by someone else — without
special cases.

## How it works

| Package | What it does |
|---|---|
| `panel` | The entry point: turns a spec into a running panel (`panel.Run`), with an experimental `View` interface for custom sections. |
| `spec` | The versioned schema, its decoder and validator (errors carry `file:line:col`), placeholders such as `{row.id}` and `{ctx.workspace_id}`, and a JSON Schema for editors and agents. |
| `herdr` | The herdr adapter: reads a popup's context, opens panels over the socket, sends notifications. The panel itself runs in any terminal without it. |
| `paneltest` | Drives a panel in tests by element id and compares golden frames. |
| `internal/…` | The UI core (layout, focus, keymap, theme roles, error screens) on [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) and [Lip Gloss v2](https://github.com/charmbracelet/lipgloss), whose compositor both draws the panel and answers the mouse; widgets; the env-file store and atomic writes; the command runner; `observe`. |

`herdr-panel observe` renders a panel headlessly and prints the frame plus
every clickable element, taken from the same layer tree that drew it, so an
agent or a test can drive a panel by element id without a real mouse.

### Rules the host enforces

- **Writes are safe.** Atomic, through a temporary file, after resolving
  symlinks; file modes kept; and a file that changed on disk since the
  panel loaded it is never overwritten.
- **Commands are honest.** No shell, a timeout on everything, stderr and
  exit status shown, and exit status 0 is not success when the command
  reports `{"ok": false}`.
- **Failures are visible.** A bad spec or a crash shows an error screen,
  never a popup that silently vanishes.
- **Secrets stay hidden.** Shown only as set or unset, with a test button.
- **It fits the user's terminal.** Colours are ANSI palette roles, never
  hard-coded RGB; meaning never rests on colour alone; text is cut by
  display width; and every clickable thing has a key.
- **It never steals focus.** The host does not raise windows or move
  focus, and action commands must not either.

## What herdr gives a panel

Measured by the spike on herdr `0.9.2-preview.2026-09-29`:

| | |
|---|---|
| Mouse | SGR reports — press, release, wheel, right button — relative to the popup's content. |
| Keys | Whatever the program asks for: legacy xterm encoding by default, and the kitty keyboard protocol (disambiguation) when the program pushes it, as Bubble Tea v2 does. |
| Closing | SIGHUP, then SIGTERM, then SIGKILL, 250 ms apart, to the popup's whole session. Only a `setsid` child outlives the popup. |
| herdr calls while open | Renames, plugin actions and notifications work. Only a second popup is refused, with `ui_busy`. |
| Opening | Size and placement go through the socket method `plugin.pane.open`; the installed CLI cannot ask for a popup. |
| Size | A 60×14 popup gives 57×12 inside its border; the smallest content area is 4×2. A size sent over the socket overrides the manifest's. |
| Context | Plugin popups get `HERDR_PLUGIN_CONTEXT_JSON` (workspace, tab, focused pane) and their plugin's config and state directories. A popup opened by a `type = "popup"` keybinding gets neither — only `HERDR_ACTIVE_*` ids. |
| Theme | No colour-query reply and no `COLORFGBG` — hence ANSI roles. |

## The spike

`spike/` drives a real herdr client inside a pseudo-terminal, against a
**fully isolated** herdr: `XDG_CONFIG_HOME=spike/xdg` and every `HERDR_*`
variable removed, so it has its own server, socket, config and plugin
registry and never touches a session you are working in. A probe plugin
(`spike/probe`) logs everything its popup process receives.

Requirements: herdr 0.9.2 or newer, Python 3 with
[pyte](https://github.com/selectel/pyte), and Go for the hit-testing
check in `spike/go`.

```sh
cd spike
python3 s1_start.py     # start the isolated herdr and link the probe plugin
python3 s2_main.py      # mouse, keys, sizes, openers, calls made while a popup is open
python3 s6_modal.py     # popups under herdr's own modals, and Escape
(cd go && go run .)     # Lip Gloss v2 compositor hit-testing
```

The probe's log lands in `spike/out/probe.jsonl`. Stop the isolated
server afterwards with `python3 -c 'import harness; print(harness.stop_server())'`
from `spike/`; it refuses unless herdr reports the spike's own socket.

## Roadmap

1. `core`, `widgets` and the env-file store, with `observe` from the first commit.
2. The `runner` and list sections; the herdr adapter and its opener.
3. The first real panel: settings and workspaces for [herdr-auto-title](https://github.com/AlexK-Notable/herdr-auto-title).
4. A second panel for a plugin written by someone else; then spec version 1.
5. Tagged releases. A plugin pins a version and builds it in its own
   `[[build]]` step. herdr runs that step without a shell, so a Go plugin
   pins herdr-panel with `go get -tool …/cmd/herdr-panel@vX.Y.Z` and builds it
   with `["go", "build", "-o", "bin/herdr-panel", "github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel"]`;
   a plugin in another language uses
   `["sh", "-c", "GOBIN=\"$PWD/bin\" go install github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel@vX.Y.Z"]`
   (`go install` refuses a relative `GOBIN`). The full plan is in
   [`docs/PLAN.md`](./docs/PLAN.md).

## Prior art and credits

A search of GitHub, npm, crates.io, PyPI and herdr's own issues and
discussions in October 2026 found no panel or UI library for herdr
plugins — only API clients and single-purpose popups. These showed the
way:

- [herdr-launcher](https://github.com/arjenblokzijl/herdr-launcher) — declarative TOML forms in a popup; the idea of passing field values as environment variables.
- [herdr-command-center](https://github.com/speardragon/herdr-command-center) — one popup for many commands, and careful handling of `ui_busy`.
- [herdr-client](https://github.com/vika2603/herdr-client) — a Go client generated from `herdr api schema`.
- [herdr-organizations](https://github.com/frizynn/herdr-organizations), a fork of [herdr-projects](https://github.com/eliasstravik/herdr-projects) — a tabbed popup with in-panel settings and confirmations.

## License

herdr-panel is licensed under the [MIT License](./LICENSE). Embed it in
your own plugins and tools, open or closed, commercial or not; keep the
copyright notice. See [`CONTRIBUTING.md`](./CONTRIBUTING.md) to contribute.

### Third-party software

Every module the library or its tests compile against has a row here;
`make licenses` fails when the two disagree.

| Module | License | Where |
|---|---|---|
| [herdr](https://github.com/herdrdev/herdr) | Apache-2.0 | not included: herdr-panel talks to a running herdr through its CLI and socket API |
| [pyte](https://github.com/selectel/pyte) | LGPL-3.0 | spike tooling only (`spike/harness.py` imports an installed copy); not included |

Each license is checked against the module's own `LICENSE` file. herdr-panel
is distributed as source only; Go's module cache keeps every dependency's
license text beside its code.
