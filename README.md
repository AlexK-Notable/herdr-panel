# herdr-panel

Spec-driven popup panels for [herdr](https://herdr.dev) plugins — and for
any terminal.

> **Status: v0.1.** Two real panels run on it: settings and workspaces for
> herdr-auto-title, and settings for herdr-file-viewer, a plugin written by
> someone else. Until v1 a minor release may change the spec or the Go API;
> every such change is listed in the [CHANGELOG](./CHANGELOG.md).

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

## A panel

The settings of a plugin that keeps them in a TOML file, and a list of
things with an action on each row:

```toml
spec = 1
title = "My plugin"
size = { width = "70%", height = "70%" }

[stores.config]
kind  = "tomlfile"                                   # or "envfile"
path  = "{env.HERDR_PLUGIN_CONFIG_DIR}/config.toml"
apply = ["./my-plugin", "reload"]                    # optional: run after a save

[[section]]
title = "Startup"
fields = [
  { key = "hide_dotfiles", type = "bool", label = "Hide dotfiles", default = false },
  { key = "scroll_lines", type = "int", label = "Wheel step", min = 1, max = 10, default = 3 },
  { key = "side", type = "choice", label = "Tree side", choices = ["left", "right"], default = "left" },
]

[[section]]
title = "Workspaces"
kind = "list"
source = { run = ["./my-plugin", "rows"], every_s = 5 }   # prints JSON rows
columns = [ { key = "label", title = "Workspace" } ]
row_actions = [
  { label = "Rename now", key = "r", run = ["./my-plugin", "rename", "{row.id}"] },
]
```

The plugin's manifest declares the popup and an action that opens it:

```toml
# herdr-plugin.toml
[[panes]]
id = "settings"
title = "Settings"
placement = "popup"
width = "70%"
height = "70%"
command = ["./bin/herdr-panel", "panel.toml"]
```

A setting is written on its own line, or where the file documents it as a
commented default; nothing else in the file changes. Runnable examples are
in [`examples/`](./examples), among them the two real panels.

## Using it

| Command | What it does |
|---|---|
| `herdr-panel <spec.toml>` | Runs the panel in the current terminal. |
| `herdr-panel validate <spec>…` | Checks specs, with `file:line:col` errors; for CI. |
| `herdr-panel observe <spec> [--sandbox] [--stub f] [--script f] [--json]` | Drives a panel headlessly and prints the frame and every clickable element. |
| `herdr-panel schema` | Prints the JSON Schema of the spec, for editors and agents. |
| `herdr-panel version` | Prints the version. |

Exit status: 0 success, 1 runtime error, 2 usage, 3 invalid spec, 4 herdr
unavailable or busy, 129/130/143 ended by SIGHUP/SIGINT/SIGTERM.

| Read | For |
|---|---|
| [`docs/spec.md`](./docs/spec.md) | The spec, key by key. |
| [`docs/herdr.md`](./docs/herdr.md) | Opening a panel from a plugin, building herdr-panel in one, and what herdr does to a popup. |
| [`docs/testing.md`](./docs/testing.md) | `observe` and `paneltest`. |
| [`docs/embedding.md`](./docs/embedding.md) | The Go API, and custom views. |
| [`docs/security.md`](./docs/security.md) | The trust model: a spec runs commands, so a spec is code. |

## How it works

| Package | What it does |
|---|---|
| `panel` | The entry point: turns a spec into a running panel (`panel.Run`), with an experimental `View` interface for custom sections. |
| `spec` | The versioned schema, its decoder and validator (errors carry `file:line:col`), placeholders such as `{row.id}` and `{ctx.workspace_id}`, and a JSON Schema for editors and agents. |
| `herdr` | The herdr adapter: reads a popup's context, opens panels over the socket, sends notifications. The panel itself runs in any terminal without it. |
| `paneltest` | Drives a panel in tests by element id and compares golden frames. |
| `internal/…` | The UI core (layout, focus, keymap, theme roles, error screens) on [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) and [Lip Gloss v2](https://github.com/charmbracelet/lipgloss), whose compositor both draws the panel and answers the mouse; widgets; the env-file and TOML stores and atomic writes; the command runner; `observe`. |

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

## Building it into a plugin

A plugin pins a version and builds herdr-panel in its own `[[build]]` step,
which herdr runs on `herdr plugin install`, without a shell:

- a Go plugin pins it as a tool, `go get -tool
  github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel@v0.1.0`, and builds it
  with `["go", "build", "-o", "bin/herdr-panel", "github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel"]`;
- a plugin in another language uses `["sh", "-c", "GOBIN=\"$PWD/bin\" go
  install github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel@v0.1.0"]` with
  `platforms = ["linux", "macos"]` (`go install` refuses a relative `GOBIN`).

A linked plugin (`herdr plugin link`) is never built by herdr; build
`bin/herdr-panel` yourself. Details: [`docs/herdr.md`](./docs/herdr.md).
Linux and macOS are supported.

## Prior art and credits

A search of GitHub, npm, crates.io, PyPI and herdr's own issues and
discussions in October 2026 found no panel or UI library for herdr
plugins — only API clients and single-purpose popups. These showed the
way:

- [herdr-auto-title](https://github.com/AlexK-Notable/herdr-auto-title) (MIT, © 2026 Alexander Satretdinov) — the socket transport in `herdr/client.go` and its test server are adapted from its `internal/herdr` package.

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
| `github.com/pelletier/go-toml/v2` | MIT | library: spec decoding with positions |
| `charm.land/bubbletea/v2` | MIT | library: the program loop |
| `charm.land/lipgloss/v2` | MIT | library: styles, and the compositor that draws and hit-tests |
| `github.com/charmbracelet/ultraviolet` | MIT | library: terminal input and screen, under Bubble Tea |
| `github.com/charmbracelet/colorprofile` | MIT | library: colour downsampling, NO_COLOR |
| `github.com/charmbracelet/x/ansi` | MIT | library: display width and truncation |
| `github.com/charmbracelet/x/term` | MIT | library: raw mode for the crash hold screen |
| `github.com/charmbracelet/x/termios` | MIT | library: required by x/term |
| `github.com/charmbracelet/x/windows` | MIT | library: required by Bubble Tea (Windows only) |
| `github.com/clipperhouse/displaywidth` | MIT | library: required by Lip Gloss |
| `github.com/clipperhouse/uax29/v2` | MIT | library: required by Lip Gloss |
| `github.com/lucasb-eyer/go-colorful` | MIT | library: required by Lip Gloss |
| `github.com/mattn/go-runewidth` | MIT | library: required by Bubble Tea |
| `github.com/muesli/cancelreader` | MIT | library: required by Bubble Tea |
| `github.com/rivo/uniseg` | MIT | library: required by Lip Gloss |
| `github.com/xo/terminfo` | MIT | library: required by Bubble Tea |
| `golang.org/x/sync` | BSD-3-Clause | library: required by Bubble Tea |
| `golang.org/x/sys` | BSD-3-Clause | library: required by Bubble Tea |
| `github.com/charmbracelet/x/exp/golden` | MIT | tests: golden frames |
| `github.com/aymanbagabas/go-udiff` | BSD-3-Clause | tests: required by golden |
| `github.com/joho/godotenv` | MIT | tests: an oracle the env-file store must read files like; it never writes |
| `github.com/santhosh-tekuri/jsonschema/v6` | Apache-2.0 | tests: validates the examples against the published JSON Schema |
| `golang.org/x/text` | BSD-3-Clause | tests: required by jsonschema |
| `github.com/charmbracelet/x/vt` | MIT | integration tests only (`integration/`, its own module): the screen of the herdr client |
| `github.com/creack/pty` | MIT | integration tests only: runs the herdr client in a pseudo-terminal |
| [herdr](https://github.com/herdrdev/herdr) | Apache-2.0 | not included: herdr-panel talks to a running herdr through its CLI and socket API |
| [pyte](https://github.com/selectel/pyte) | LGPL-3.0 | spike tooling only (`spike/harness.py` imports an installed copy); not included |

Each license is checked against the module's own `LICENSE` file. herdr-panel
is distributed as source only; Go's module cache keeps every dependency's
license text beside its code.
