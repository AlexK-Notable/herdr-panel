# AGENTS.md

herdr-panel draws popup panels for [herdr](https://herdr.dev) plugins from
a spec file: a Go host binary, a Go library under it, and a herdr adapter
layer that the rest does not depend on. Read [`README.md`](./README.md)
first — it holds the design. This file holds the rules for working here.

**Status:** under construction, following [`docs/PLAN.md`](./docs/PLAN.md)
milestone by milestone. Update the layout below in the same change as the
code it describes.

## Repository layout

```
spike/              the experiment that established what a popup can rely on
  harness.py        drives an isolated herdr client in a pty, rendered with pyte
  probe/            a herdr plugin whose popup logs env, context, size, input
  s1_start.py …     scenario scripts, run one at a time
  slice/            slice S: a Bubble Tea v2 panel installed with a real [[build]]
  slice-bad/        its negative control, a build that always fails
  s7_slice.py       runs slice S end to end against the isolated herdr
  go/               Lip Gloss v2 hit-testing check
  out/probe.jsonl   the probe's evidence
  xdg/herdr/        the isolated herdr config; everything else in it is ignored
```

Library (module `github.com/AlexK-Notable/herdr-panel`, `go 1.26.0`):

```
cmd/herdr-panel/    the CLI
panel/ spec/ herdr/ paneltest/    the public API
internal/           ui, widget, store, run, source, observe, atomicfile
tools/              its own module: golangci-lint, gorelease, govulncheck
scripts/            check-third-party (README table vs compiled modules)
docs/               PLAN.md, and the user docs
```

Only `herdr` may import herdr-specific code; the panel must run in any
terminal without it. Only `internal/run` may import `os/exec` (depguard and
forbidigo enforce it).

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
   test's own directory), **`XDG_STATE_HOME`, `XDG_DATA_HOME` and
   `XDG_CACHE_HOME` too**, and **every `HERDR_*` variable removed**. A
   leftover `HERDR_SOCKET_PATH` reaches the user's real session, and herdr
   puts plugin state directories under `XDG_STATE_HOME`, so isolating only
   the config directory once leaked the spike's plugin state into the live
   `~/.local/state/herdr`. Use
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
- **Keys follow what the popup's program asks for.** A program that pushes
  nothing gets legacy xterm encoding (bare `ESC`, `ESC[A`, `ESC x` for alt).
  Bubble Tea v2 pushes the kitty keyboard protocol, and herdr answers its
  query with flags 1 (disambiguation): Esc, alt, ctrl, shift+tab and
  bracketed paste all decode correctly (slice S, `spike/s7_slice.py`).
- **The socket's size beats the manifest's.** `plugin.pane.open` with
  `width`/`height` overrides the `[[panes]]` size; without them, and from the
  CLI, the manifest's size applies. A 60×16 popup has 57×14 inside.
- **Closing a popup sends SIGHUP first**, then SIGTERM and SIGKILL 250 ms
  apart, to every process in the popup's session. Bubble Tea does not catch
  SIGHUP; handle it. Only a `Setsid` child survives — which also means every
  `Setsid` child a test starts must be killed by the test.
- **A panic closes the popup and the message with it.** Bubble Tea's own
  recovery prints the stack to stderr and `Run` returns `ErrProgramPanic`;
  the process exits and the popup vanishes. A hold screen after `Run`
  returns keeps the failure readable.
- **`herdr plugin install` takes only GitHub `owner/repo[/subdir]`** (with
  `--ref`), clones over https, and runs `[[build]]` as an argv with no shell
  in the manifest's directory. `plugin link` never builds. A private repo
  needs git auth: with `XDG_CONFIG_HOME` isolated, gh cannot find its login,
  so the spike passes `GH_CONFIG_DIR` for the install only. `go install`
  refuses a relative `GOBIN`.
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

`make check` is the gate (format, vet, lint for linux and darwin, race
tests, the third-party table); commit only when it exits 0. Tests sit
beside the code. Commit
messages say what changed and why, in the imperative.
