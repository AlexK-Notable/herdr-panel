# AGENTS.md

herdr-panel draws popup panels for [herdr](https://herdr.dev) plugins from
a spec file: a Go host binary, a Go library under it, and a herdr adapter
layer that the rest does not depend on. Read [`README.md`](./README.md)
first — it holds the design. This file holds the rules for working here.

**Status:** design and spike. Only `spike/` exists. The library has not
been started; when it is, update the layout below in the same change.

## Repository layout

```
spike/              the experiment that established what a popup can rely on
  harness.py        drives an isolated herdr client in a pty, rendered with pyte
  probe/            a herdr plugin whose popup logs env, context, size, input
  s1_start.py …     scenario scripts, run one at a time
  go/               Lip Gloss v2 hit-testing check
  out/probe.jsonl   the probe's evidence
  xdg/herdr/        the isolated herdr config; everything else in it is ignored
```

Planned, per the README's layer table: `cmd/herdr-panel`, then packages
`core`, `widgets`, `spec`, `stores`, `runner`, `herdr`, `observe`. Only
`herdr` may import herdr-specific code; the panel must run in any
terminal without it.

## Decisions already made — do not reopen

| Decision | Ruling |
|---|---|
| Shape | A spec-driven host binary, plus a public Go library for custom views. |
| Distribution | Each consuming plugin builds a pinned copy in its own `[[build]]` step. No shared copy on `PATH`, and **never** a downloaded prebuilt binary. |
| Scope | A general terminal-panel core; herdr is one adapter layer. |
| herdr's own config | Panels never write herdr's `config.toml`. Plugin configs only. |
| Saving | Save, then apply — unless the user rules otherwise. |
| License | MIT everywhere. No CLA; contributions are inbound = outbound MIT. |

## Hard rules

1. **Never touch a live herdr session.** Anything that starts, drives or
   stops herdr runs isolated: `XDG_CONFIG_HOME=<repo>/spike/xdg` (or a
   test's own directory) **and every `HERDR_*` variable removed** — a
   leftover `HERDR_SOCKET_PATH` reaches the user's real session. Use
   `spike/harness.py`'s `isolated_env()`, `cli()`, `api()` and
   `stop_server()` — the last refuses unless `herdr status` reports the
   isolated socket. Any new teardown must do the same check.
2. **Never steal focus.** No `xdg-open`, `hyprctl`, `xdotool`, `wmctrl`,
   and no herdr focus or switch verbs, in the host, the library, tests or
   example action commands. herdr-internal focus on a key the user pressed
   is fine; raising or moving windows is not.
3. **Permissive dependencies only** — MIT, BSD, Apache-2.0, ISC. No GPL,
   AGPL, or LGPL code linked into Go. Add every new dependency to the
   README's third-party table in the same change, and check its license
   in the module's own `LICENSE` file, not only GitHub's SBOM column.
4. **Commands never use a shell**, always have a timeout and an output
   cap, and never count exit status 0 alone as success when the command
   speaks the `{"ok": …}` result contract.
5. **Writes are atomic** (temporary file, then rename), **resolve
   symlinks before the rename**, keep the file mode, and refuse to
   overwrite a file whose hash changed since it was loaded.
6. **Fail visibly.** A bad spec, a crashed source or a panic renders an
   error screen and restores the terminal. Never exit silently: a popup
   that vanishes tells the user nothing.
7. **Secrets** are never rendered, logged or written to test output.

## herdr traps

Measured on herdr `0.9.2-preview.2026-09-29`; re-check after upgrades.

- **The CLI cannot open a popup.** `herdr plugin pane open --placement`
  offers `overlay, split, tab, zoomed` and takes no size. Popups, sizes
  and env go through the socket method `plugin.pane.open`
  (`placement: "popup"`, `width`, `height`, `focus`, `env`).
- **One popup at a time, session-wide.** Opening a second returns
  `ui_busy`. Everything else tested — renames, `plugin action invoke`,
  notifications — works while a popup is open. Only an action that opens
  another popup has to wait for this one to close.
- **`invocation_source` is always `api`** inside a plugin popup, however
  it was opened. An opener that needs to say how passes its own env var.
- **`[[keys.command]] type = "popup"`** gives the process no plugin
  context: no `HERDR_PLUGIN_*`, so no config or state directory. Only
  `HERDR_ACTIVE_{PANE,TAB,WORKSPACE}_ID` and `HERDR_ACTIVE_PANE_CWD`.
- **Keys arrive in legacy xterm encoding** (bare `ESC`, `ESC[A`, `ESC x`
  for alt). A lone Escape is told apart by timing.
- **The smallest popup content area is 4×2.** Layouts must degrade to a
  "make me bigger" line, not garbage.
- **The socket takes one NDJSON request per connection.**
- **Bare group commands can act.** `herdr server` with no subcommand tries
  to start a server. Print a group's help with `--help`.
- **`prefix+x` closes the pane**, as in tmux. When a test needs a harmless
  unbound key, check the keymap first.

## Testing

- **A screen model can lie.** pyte does not know the kitty keyboard
  protocol herdr pushes and pops around prefix mode, and drew part of
  `ESC[<1u` as the text `1u` — which looked exactly like keystrokes
  leaking into the shell. herdr's own `herdr pane read <pane> --source
  visible` showed the prompt clean. The harness now strips those
  sequences. Before concluding anything from a rendered screen, check it
  against `pane read`.
- **Prove a check can fail.** A test or probe that would print the same
  thing whether the feature works or not proves nothing; break the
  feature once and watch the test go red.
- **Drive panels through `observe`**, by element id, not by pixel
  coordinates guessed from a screenshot.

## Commands

```sh
cd spike && python3 s2_main.py        # the main spike scenario
cd spike/go && go run .               # hit-testing check
cd spike && python3 -c 'import harness; print(harness.stop_server())'   # isolated server only
```

Go code: `gofmt` and `go vet` clean; tests beside the code. Commit
messages say what changed and why, in the imperative.
