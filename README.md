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

herdr-panel is licensed under the **Functional Source License, Version
1.1, MIT Future License** — SPDX identifier `FSL-1.1-MIT`. Full text at
[`LICENSE`](./LICENSE).

It is **source-available, not open source**: the License Grant is
conditioned on using the Software for a Permitted Purpose, and Permitted
Purposes specifically include your internal use and access — so you may
use, copy, modify, create derivative works of, and redistribute the
Software, including for internal and commercial purposes such as
embedding it in your own herdr plugin, subject to that condition. What
the license bars is **Competing Use**: offering the Software, or
something that substitutes for it or offers substantially similar
functionality, to others as a commercial product or service.

Each version of the Software converts to the plain MIT license,
irrevocably, on the second anniversary of the date that version was made
available — the conversion applies per version, not to the repository as
a whole.

This is a summary for orientation; the [`LICENSE`](./LICENSE) file is
the actual grant and controls if the two ever disagree. Contributions
require agreeing to the [`CLA`](./CLA.md); see
[`CONTRIBUTING.md`](./CONTRIBUTING.md).

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
