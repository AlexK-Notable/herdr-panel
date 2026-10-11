# herdr-panel

Spec-driven popup panels for [herdr](https://herdr.dev) plugins — and for
any terminal.

> **Status: design and spike only.** Nothing here is usable yet. The
> `spike/` directory holds the experiment that established what a herdr
> popup panel can rely on; the library itself has not been written.

## The idea

herdr plugins are written in many languages, and herdr gives them no way
to add settings or controls to its own UI. herdr-panel is one small
binary that draws a mouse- and keyboard-driven panel — settings fields,
lists with row actions, buttons — from a **spec file**, inside a herdr
popup. A plugin supplies the spec and a few commands that print JSON or
perform actions; a panel that only edits a config file needs no plugin
code at all. A Go library underneath is available for custom views.

herdr-panel is an unofficial community project. It is not affiliated
with, or endorsed by, the herdr project.

## What the spike found

On herdr `0.9.2-preview.2026-09-29`, in an isolated session:

- a popup receives SGR mouse reports relative to its own content, and
  keys in legacy xterm encoding;
- herdr calls made while a popup is open (renames, plugin actions,
  notifications) succeed; only a second popup is refused with `ui_busy`;
- a popup's size and placement must be requested over the socket
  (`plugin.pane.open`) — the CLI cannot ask for a popup;
- the smallest popup content area is 4×2;
- Lip Gloss v2's compositor hit-tests nested, overlapping layers, so one
  layer tree can both draw a panel and answer clicks.

`spike/harness.py` drives a herdr client in a pseudo-terminal against an
isolated config (`XDG_CONFIG_HOME=spike/xdg`, every `HERDR_*` variable
removed), so it never touches a live session.

## License

herdr-panel is licensed under the [MIT License](./LICENSE). Embed it in
your own plugins and tools, open or closed, commercial or not; keep the
copyright notice. See [`CONTRIBUTING.md`](./CONTRIBUTING.md) to contribute.

### Third-party software

| Component | License | How it is used |
|---|---|---|
| Bubble Tea v2, Lip Gloss v2, ultraviolet and the `charmbracelet/x` packages (Charmbracelet, Inc.) | MIT | Go dependencies |
| `rivo/uniseg`, `mattn/go-runewidth`, `clipperhouse/uax29`, `clipperhouse/displaywidth`, `lucasb-eyer/go-colorful`, `xo/terminfo`, `muesli/cancelreader` | MIT | Go dependencies |
| `golang.org/x/sys`, `golang.org/x/sync` (The Go Authors) | BSD-3-Clause, with Google's Go patent grant | Go dependencies |
| [herdr](https://github.com/herdrdev/herdr) | Apache-2.0 | Not included. herdr-panel talks to a running herdr through its CLI and socket API. |
| [pyte](https://github.com/selectel/pyte) | LGPL-3.0 | Spike tooling only (`spike/harness.py` imports an installed copy); not included and not used by the library. |

The Go dependency list comes from GitHub's dependency-graph SBOM (SPDX
2.3), with each license checked against the module's own `LICENSE` file.
Binary releases will ship the dependencies' license texts.
