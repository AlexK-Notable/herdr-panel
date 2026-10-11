# herdr-panel — library build plan

## Context

herdr plugins cannot add to herdr's own UI. Menus, the settings panel and the sidebar are
closed, which was verified in source and in the spike. The only extension surface is a
plugin-owned popup. So every plugin that wants settings or controls builds its own TUI, in its
own language: the installed plugins are written in Python, TypeScript and Rust, and ours in Go.

herdr-panel is meant to be written once. It has two parts:

- **a spec-driven host binary**, which draws a mouse- and keyboard-driven panel (settings
  fields, lists with row actions, buttons) from a spec file;
- **a public Go API** for custom views.

The first consumer is the herdr-auto-title fork: settings, plus a workspace list with "rename
now". The repo is `~/repos/herdr-panel` → `github.com/AlexK-Notable/herdr-panel`: private, MIT,
`main` at `6bed05a`. It holds `spike/`, README.md, AGENTS.md and CONTRIBUTING.md.

Design record (znote): design `VaekjUNTPwEey_zx30Ofy`, prior art `EK_SOODKcugzTeQbnHfwo`,
spike `87qNNe7uxbtfJt3acThq3`, licensing `i0niRea8sDUBj_nhtSDHs`.

## Decisions binding this plan

| Topic | Ruling (each was my recommendation, accepted by the user, except Saving) |
|---|---|
| Shape | A spec-driven host binary, plus a public Go API for custom views |
| Distribution | Each consumer pins a version and builds from source. Never a downloaded binary. (Recipes: see "herdr facts" below) |
| Scope | A general terminal-panel core; herdr is one adapter package |
| herdr's config.toml | Panels never write it |
| License | MIT everywhere, no CLA (the user's words: "just go MIT everywhere") |
| Platforms | Linux + macOS, both tested in CI. No Windows, though herdr has a build for it |
| Name | Keep `herdr-panel`; module `github.com/AlexK-Notable/herdr-panel` |
| Stability | v0.x, with breaking changes listed in CHANGELOG. Spec v1 and the Go API v1 freeze together once two real panels work without special cases: auto-title and one plugin written by someone else |
| Saving | **The user chose per-spec** (I had recommended save-then-apply): `save = "save_then_apply"` (the default) or `"live"` |
| Go floor | `go 1.26.0`: Bubble Tea v2.1.0's own `go.mod` requires it (verified; an audit claim of 1.25 was wrong) |

## herdr facts this plan rests on (verified in source at tag `preview-2026-09-29-8e78f929d8f0`)

- **`[[build]]`**:
  - It runs as an argv, with no shell (`plugin_command.rs:5`), in the plugin's checkout
    directory.
  - It runs **only on `herdr plugin install`**: `cli/plugin.rs:231` is the only call site, and
    `plugin link` never builds.
  - It inherits the user's environment, minus 9 herdr runtime variables
    (`cli/plugin.rs:1532`).
  - Each entry has `command` and an optional `platforms` (`manifest.rs:37`).
  - Consequence: the README recipe `GOBIN=$PWD/bin go install …` is wrong. There is no shell
    to set the variable or expand `$PWD`.
- **Pane or popup close**: herdr sends `SIGHUP`, then `SIGTERM`, then `SIGKILL`, waiting
  **250 ms** after each, to every process in the pane's **session** (`pane.rs:1616`,
  `platform/linux.rs:726`). So:
  - A panel cannot block or delay a close.
  - Only a `Setsid` child outlives the popup.
  - Bubble Tea catches only SIGINT and SIGTERM (`tea.go`), so without a handler a panel dies
    on the SIGHUP.
- **Bubble Tea v2 pushes modifyOtherKeys and the kitty keyboard protocol**
  (`cursed_renderer.go:163`), and herdr **answers** with flags 1 (disambiguation). Slice S
  measured it: Esc, alt, ctrl, shift+tab and bracketed paste all decode correctly.
- The rest stays as measured by the spike:
  - one popup per session (a second one gets `ui_busy`);
  - only the socket can request popup placement and a size;
  - mouse reports are SGR, relative to the popup's content;
  - the smallest content area is 4×2;
  - a `type="popup"` keybinding popup gets only `HERDR_ACTIVE_*`.

## What a consumer gets (the contract)

1. **A documented, versioned spec**:
   - `spec = 1`, in TOML.
   - Unknown keys are errors.
   - Every error carries `file:line:col`.
   - A JSON Schema is published for editors and agents.
   - `docs/spec.md` documents it field by field.
   - Compatibility:
     - `requires = ">=0.3"` makes an older host refuse with a clear "needs herdr-panel ≥0.3"
       message, instead of an unknown-key error.
     - Within spec 1, keys are only ever added.
     - Spec 2 is a new major. The host reads every version it knows.
2. **Placeholders and paths**:
   - The forms are `{row.<col>}`, `{field.<key>}`, `{ctx.workspace_id|tab_id|pane_id|cwd}`,
     `{env.X}` and `{env.X:-default}`, plus a leading `~`.
   - A placeholder expands inside **one argv element** and never splits it.
   - A relative program (`./x`) and relative store paths resolve against the **spec's
     directory**, not the cwd.
   - `docs/security.md` warns that handing a placeholder to a program that runs `sh -c` brings
     injection back.
3. **A CLI with stable exit codes**:

   | Command | What it does |
   |---|---|
   | `herdr-panel <spec>` | Runs the panel. |
   | `validate <spec>…` | Lints specs; usable in CI. |
   | `observe <spec> [--size WxH] [--script f] [--sandbox] [--stub f] [--json]` | Drives a panel headlessly. Versioned JSON (`"observe": 1`). `--sandbox` runs no commands (it records the argv each would get) and points stores at temp copies. `--stub` supplies source rows and action results. |
   | `schema` | Prints the JSON Schema. |
   | `version` | Prints the version. |

   Exit codes:

   | Code | Meaning |
   |---|---|
   | 0 | Success. |
   | 1 | Runtime error. |
   | 2 | Usage error. |
   | 3 | Invalid spec. |
   | 4 | herdr unavailable, or `ui_busy`. |
   | 129 / 130 / 143 | Ended by SIGHUP / SIGINT / SIGTERM. |

4. **A small public Go API**:
   - `panel`: `Run(ctx, spec, Options)`; `Options` covers theme, IO and size. It also has an
     experimental `View` interface for custom sections:
     `Layer(Theme, Size) *lipgloss.Layer; Update(tea.Msg) tea.Cmd`.
   - `spec`: `Load`, `Validate` and the types, so a spec can be built in Go.
   - `herdr`: popup context, a socket client and notifications.
   - `paneltest`: helpers for testing embedders.
   - Everything else lives in `internal/` until the v1 freeze. `gorelease` flags API breaks
     in CI.
   - Runnable `Example` tests document the API on pkg.go.dev.
   - Deprecation rule: a symbol is deprecated for one minor release before it is removed.
5. **Safety guarantees**, each with a negative control that turns red:
   - **Writes**:
     - atomic (temp file in the target directory → fsync → rename → fsync the directory);
     - symlinks resolved before the rename;
     - mode kept, and new files created 0600 (new directories 0700);
     - refused when the file's content hash changed since it was loaded;
     - orphaned temp files from a killed run (our own prefix only) swept at startup.
   - **Commands**:
     - no shell;
     - a timeout, plus `WaitDelay`;
     - capped writers that never block the child;
     - `Setpgid`, and a group kill on timeout or quit;
     - the environment is **inherited**, with `HERDR_SOCKET_PATH` kept so actions can call
       herdr, plus `PANEL_FIELD_*`, `PANEL_ROW_*` and `PANEL_VIA`;
     - when the output is a `{"ok":…,"message":…}` result, exit 0 with `ok:false` counts as
       a failure.
   - **Teardown**:
     - SIGHUP or SIGTERM cancels the context, group-kills running commands, removes our temp
       files and exits, all inside herdr's 250 ms budget.
     - Anything that must outlive the popup is started with `Setsid`. That is the only way it
       survives.
   - **Fail visibly**:
     - A panic in Update, View or a Cmd is recovered.
     - It shows a fatal screen that waits for a key, writes a crash log and sends a herdr
       notification when one is available.
     - A bad spec gets an error screen. The panel never exits silently.
   - **Secrets**: never rendered, logged, put in argv or written to test output. A test
     command reads the secret itself.
   - **No focus theft** (AGENTS.md rule 2).
6. **UX guarantees**:
   - Every clickable has a key, and focus is visible.
   - `?` opens a help overlay.
   - The keymap is consistent: Tab/Shift-Tab, arrows, Enter/Space, Esc, `/` to filter.
   - Paste goes into the focused text field.
   - Colour:
     - colours are ANSI-16 theme roles;
     - `NO_COLOR` is honoured;
     - meaning never rests on colour alone.
   - Text is cut by display width.
   - Below the minimum size, the panel shows one "make me bigger" line.
   - The first frame appears within 50 ms on this host.
   - Saving and closing:
     - Esc inside the panel asks before discarding unsaved edits.
     - A herdr-level close (popup close, `prefix+x`) **discards without asking**, and the
       docs say so.
     - A failed apply leaves a visible "saved, not applied" state.
     - A value on disk that is not in a choice list shows as `custom: X` and is preserved
       untouched.
7. **Consumer testing**: `observe` returns the frame plus every clickable element with its id
   and box, taken from the same compositor that draws and hit-tests. `paneltest` drives a
   panel by element id and compares golden frames.
8. **herdr integration**:
   - snippets for the manifest, keybindings and `[[build]]`;
   - `ui_busy` turned into a notification;
   - both popup kinds' context read correctly;
   - a log file in the plugin's state directory, or under `XDG_STATE_HOME`;
   - the verified range of herdr versions recorded in `docs/herdr.md`.
9. **Docs and hygiene**:
   - README, plus `docs/{spec,herdr,embedding,testing,security}.md` (the trust model: a spec
     runs commands, so a spec is code);
   - runnable `examples/`, CHANGELOG, SECURITY.md, and issue/PR templates before the repo
     goes public.
   - Distribution is source-only, so Go's module cache already carries every dependency's
     licence. The README's third-party table is checked in CI against `go list -m all`.

## Architecture

Module `github.com/AlexK-Notable/herdr-panel`, `go 1.26.0`. Public: `panel`, `spec`, `herdr`,
`paneltest`. Internal:

| Package | Role |
|---|---|
| `internal/ui` | Bubble Tea model, focus ring, keymap, help, theme roles, error and fatal screens, dialogs, save semantics, panic recovery |
| `internal/widget` | toggle, choice, number/duration, text, secret status, button, list/table, text/log view, status line, confirm |
| `internal/store` | `Store{Load(ctx) (Values, Revision, error); Save(ctx, Values, Revision) error}`; `envfile` only in v0 |
| `internal/run` | **The only package allowed `os/exec`** (enforced by depguard) |
| `internal/source` | Source commands, polling, list state |
| `internal/observe` | Element tree from the compositor; script driver; JSON v1 |
| `internal/atomicfile` | The write path above, plus load-time hashing and the temp-file sweep |
| `cmd/herdr-panel` | The CLI |

**Rendering and hit-testing rules:**

- Each frame is a `lipgloss.Compositor` of ID'd layers.
- Z values are **absolute** per layer kind: base 0, sections 10, focus ring 20, modal backdrop
  90, dialog 100. They are set explicitly, because a child does not inherit z and the sort is
  unstable on equal z.
- A full-size base layer sits at 0,0.
- While a dialog is open, an **ID'd backdrop** swallows clicks behind it.
- Layers are clipped to their parent's box.
- The same compositor answers `Hit(msg.X, msg.Y)`.
- A property test checks that a click at the centre of every element hits that element's id.

**Lists:**

- rendering is virtualised;
- selection is kept **by row id** across refreshes;
- `/` filters, and PgUp/PgDn page;
- at most one poll is in flight, and a sequence number discards stale results;
- after a failure the poll backs off and shows an error row.

**Logging:** `log/slog` to the log file, never to the terminal.

## Dependencies (all permissive; each added to the README table in the same change)

| Dependency | Version | Licence | Where |
|---|---|---|---|
| `charm.land/bubbletea/v2` | v2.1.0 | MIT | main |
| `charm.land/lipgloss/v2` | v2.0.6 | MIT | main |
| `charm.land/bubbles/v2` | v2.0.0 (verify at M0) | MIT | main: `textinput`, `key`, `help` |
| `github.com/charmbracelet/x/ansi` | transitive | MIT | main: `Truncate`, width |
| `github.com/pelletier/go-toml/v2` | v2.4.3 | MIT | main: spec decode with positions |
| `github.com/charmbracelet/x/exp/golden` | v0.1.0 | MIT | tests |
| `github.com/joho/godotenv` | latest tag | MIT | tests only: a parse-equivalence oracle for the env-file store |
| `github.com/charmbracelet/x/vt` | pseudo-version | MIT | **`integration/` module only**: screen model of the herdr client |
| `creack/pty` | v1.1.24 | **verify licence before adding** | `integration/` only |
| `santhosh-tekuri/jsonschema/v6` (Apache-2.0), `invopop/jsonschema` (MIT), `golangci-lint` v2, `gorelease`, `govulncheck` | | | `tools/` module only |

Env-file editing is hand-written, because godotenv rewrites files and drops comments.

## Reuse (from `~/repos/herdr-auto-title`, MIT © 2026 Alexander Satretdinov)

Lift with attribution, as a per-file header and a README credit:

- **The socket transport**:
  - `internal/herdr/protocol.go`: request, frame, `APIError`, `ErrorCode`, `ErrUnanswered`;
  - `client.go`: `SocketClient.Call` and `unanswered`;
  - `dial_unix.go`.

  Parameterise the request-id prefix, and add `CodeUIBusy`.
- **Test patterns**: the `testServer` from `client_test.go`, and the JSON round trip of the
  `herdrtest` stub.
- **Tooling**:
  - the `Makefile` targets `check/fmt/vet/lint/test`;
  - the `tools/go.mod` pin for golangci-lint;
  - the `.golangci.yml` skeleton, with a new depguard allowlist and `os/exec` allowed only in
    `internal/run`;
  - the `ci.yml` layout, without the Windows job.
- **The command pattern** from `internal/namer/cli.go` (no shell, no output in errors), to be
  extended with the cap, `WaitDelay`, process group and teardown.

Do **not** reuse its atomic writes (no symlink, mode or conflict handling) or its godotenv write
path.

## Milestones

Stop for user review at each boundary, and write a znote checkpoint (plan_id `herdr-panel`).

**S — Vertical slice, the riskiest unknowns first.** A throwaway Bubble Tea v2 + Lip Gloss
compositor program (one toggle and one button) lives in `spike/slice/`, its own module. It runs
as a popup in the **isolated** herdr and logs:

- **key encoding under Bubble Tea's kitty push**: legacy or CSI u? Measure Esc latency, alt
  combinations and paste;
- **teardown**: close the popup and check that a SIGHUP handler ran (it leaves a marker file),
  that a `Setsid` grandchild survives and that a plain child dies;
- **size precedence**: the manifest `[[panes]]` size versus the socket's `width`/`height`
  (re-run spike scenario E);
- the first-frame time inside herdr;
- whether Bubble Tea's own panic recovery leaves the popup open long enough to read;
- **`[[build]]`**: `herdr plugin install` of a local git checkout with
  `["go","build","-o","bin/slice","."]`, plus a negative control (a build that fails must stop
  the install). Also whether `go install` accepts a relative `GOBIN`. If `install` can't take a
  local source, record that and test the build at M8 instead.

*Done when* the findings are in the spike znote and AGENTS.md's herdr traps are updated. If
the key encoding or teardown differs from the assumptions above, M3's design changes before
it starts.

**S — results (2026-10-10, `spike/s7_slice.py`, isolated herdr).**

| Question | Measured |
|---|---|
| Keys | herdr answers the kitty query with flags=1. Every test key decoded correctly, and so did paste. It took 159 ms from the harness's write of a lone Esc to Bubble Tea's decode; where that time goes is not measured. |
| Size | The socket's size beats the manifest's: 70×20 gives 67×18, and the manifest's 60×16 gives 57×14. The CLI uses the manifest. |
| First `View` call | It came 0 ms after process start and about 4 ms after the open call. This is the call, **not a painted frame**; M3's benchmark measures the frame. |
| Close (`popup.close`) | The 250 ms SIGTERM/SIGKILL cadence was not observed and rests on source alone; M3's teardown test measures it with SIGHUP ignored. Only SIGHUP was needed: the handler quit at 0 ms and `Run` returned nil. The plain child died. The `Setsid` child survived, and so did one from every other popup run. |
| Panic, Bubble Tea's own recovery | The popup vanished instantly, and the stack went with it. |
| Panic, hold screen after `Run` | It stayed readable until a key, for panics in Update and in a Cmd alike. |
| `[[build]]` | A failing build was refused ("Plugin was not installed"). The good build produced `bin/slice`. Install takes only GitHub `owner/repo[/subdir]`. A private repo needs gh's login, which isolation hid; the fix is `GH_CONFIG_DIR` for the install only. |
| Relative `GOBIN` | Refused: `cannot install, GOBIN must be an absolute path`. |

**Consequences for M3**:

- Bubble Tea's recovery returns only `ErrProgramPanic`, with the stack on stderr, which
  vanishes with the popup. So `internal/ui` recovers on its own in Update, View and every Cmd
  it returns. It captures the value and the stack, writes them to the crash log, and shows
  them on the hold screen.
- Bubble Tea's catch stays as a backstop. When it fires, stderr is redirected to the crash
  log so the stack survives.
- The keymap may rely on disambiguated keys inside herdr, and still works with legacy
  encoding elsewhere.

**M0 — Scaffolding.**

- `go.mod` (1.26.0), plus separate `tools/go.mod` and `integration/go.mod`.
- The Makefile: `check` = fmt, vet, golangci-lint for linux and darwin, `test -race`, the
  licence-table check; plus the targets `integration`, `bench` and `gorelease`.
- CI on Linux and macOS × Go 1.26 and stable, plus govulncheck and gitleaks, with actions
  pinned by SHA. Dependabot.
- SECURITY.md, CHANGELOG and stubs under `docs/`.
- `spike/` stays outside the module.

*Done when* `make check` is green locally and in CI on both operating systems.

**M1 — Spec.**

- Types, and a TOML decode with positions.
- Unknown-key errors.
- Semantic validation: store references, field types, bounds, choices, placeholders, save
  mode, `requires`.
- The placeholder engine.
- `validate` and `schema` CLIs.
- The schema generated in `tools/` and committed (`schema/spec-v1.json`).
- `docs/spec.md`, and `examples/` (minimal, auto-title, a list-heavy one).
- A fuzz test of the decoder.

*Done when*:

- every example validates;
- every error fixture reports the right `file:line:col`;
- the examples validate against the committed schema;
- a placeholder value containing spaces or quotes stays one argv element.

**M2 — Env-file store and safe writes.**

- `internal/atomicfile`, including the temp-file sweep.
- The `envfile` store keeps comments, order, `export` and quoting. A commented default
  `#KEY=default` becomes a real line on the first set, and missing keys are appended.
- The revision is a hash of the content at load.
- The conflict path offers a dialog in M3: reload, overwrite (re-read, re-apply only our
  edited keys, save) or cancel.

*Done when*:

- the round trip leaves files byte-identical except for the edited values;
- the godotenv oracle agrees on every fixture;
- the negative controls pass: the symlink target is written (not replaced), the mode is kept,
  a concurrent edit is detected, and a kill between the temp-file create and the rename leaves
  the original intact while the next start sweeps the orphan.

**M3 — UI core, widgets and observe.**

- The model, compositor and hit-test rules (property test included).
- Keymap, help and focus ring.
- Theme roles (`NO_COLOR`; light/dark from `BackgroundColorMsg` when answered, dark by
  default).
- The size degrade, error and fatal screens, panic recovery, and slog logging.
- The SIGHUP/SIGTERM teardown.
- Dialogs and save semantics, both modes.
- The field widgets, built on `bubbles` where they fit; secret masking.
- `observe` with `--sandbox`/`--stub`; `paneltest` and golden frames; the startup benchmark.

*Done when*:

- the auto-title settings example is fully operable through `observe` by element id;
- the golden frames pass;
- the first frame takes ≤ 50 ms;
- a deliberate panic shows the fatal screen and writes the crash log.

**M4 — Runner, sources, lists and actions.**

- `internal/run`, with the guarantees in contract item 5.
- The store's `apply` command, with the "saved, not applied" state.
- List sections with the list rules above.
- Row actions inline.
- The secret-status test button.

*Done when* these negative controls fail as designed:

- a command that sleeps past the timeout;
- one that floods its output;
- a grandchild holding stdout open;
- exit 0 with `{"ok":false}`;
- a slow poll overlapped by the next tick;
- a selection that survives a refresh that reorders rows.

**M5 — herdr adapter and integration harness.**

- The socket client (lifted) and a `ping`/protocol check.
- Context for plugin popups (`HERDR_PLUGIN_*`, `HERDR_PLUGIN_CONTEXT_JSON`) and for
  keybinding popups (`HERDR_ACTIVE_*`, with the state directory falling back to
  `XDG_STATE_HOME`).
- `herdr.Open` (Go API only: `plugin.pane.open` with popup placement, size, env and focus).
- `notification.show`, including its `shown:false` reasons.
- Live lists keep polling. `events.subscribe` stays out, because it replays about 10 s of
  backlog with no cursor.
- The integration harness in `integration/`, with these properties:
  - isolated `XDG_CONFIG_HOME`, and every `HERDR_*` removed;
  - a guarded server stop;
  - a herdr client in a pty, read through `x/vt`, with mouse input as SGR;
  - results checked against `herdr pane read`;
  - skipped when herdr is absent.
- `docs/herdr.md`.

*Done when* the suite, against an isolated herdr only:

- opens a real popup;
- clicks a toggle and saves;
- finds the file on disk changed;
- sees `herdr pane read` agree with `x/vt`;
- closes the popup and finds no orphaned temp files.

**M6 — First consumer: the auto-title panel** (in the fork, with its own short plan):

- The `panel workspaces` subcommand, printing JSON rows.
- `workspace rename-now|release`, reaching the daemon through a state-directory inbox it
  already polls.
- The `[[panes]]` popup and an action in `herdr-plugin.toml`, with the keybinding added to the
  live herdr config only with the user's approval.
- **Build**:
  - The fork is **linked**, so `[[build]]` never runs for it. Its Makefile builds
    `bin/herdr-panel` itself.
  - During development it uses a git-ignored `go.work` that points at `~/repos/herdr-panel`.
  - For installed users, the committed `go.mod` pins a tag through
    `go get -tool …/cmd/herdr-panel@vX`, and `[[build]] command = ["go","build","-o",
    "bin/herdr-panel","github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel"]`.
  - While the repo is private, this needs `GOPRIVATE=github.com/AlexK-Notable/*` and git auth.

*Done when* the user opens it in the live session and uses it.

**M7 — Second consumer**: a fork of a plugin written by someone else, e.g. herdr-file-viewer's
config.

- Every spec change it forces is recorded.
- Deferred items it triggers are built here.
- Then spec v1 and the Go API v1 become candidates.

**M8 — Docs and release.**

- Docs complete, and examples runnable.
- CHANGELOG, and a manual tag `v0.1.0`.
- Both consumer `[[build]]` forms verified from the tag in scratch plugins installed into an
  isolated herdr: the Go form, and for non-Go plugins
  `["sh","-c","GOBIN=\"$PWD/bin\" go install github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel@vX"]`
  with unix `platforms`. While the repo is private, `GOPRIVATE` is set. After it goes public,
  the check is repeated without it.
- The repo goes public **only when the user says so**, after a final gitleaks scan of the
  full history.

## Deferred, each with the trigger that brings it back

| Deferred | Brought back when |
|---|---|
| JSON spec input | A consumer generates specs from a non-Go tool |
| `jsonfile` / `tomlfile` stores | M7's plugin (or any consumer) keeps its config in JSON or TOML; most likely M7 |
| An `open` CLI subcommand | A non-Go consumer needs to open a popup with a runtime size or env that its manifest can't express |
| An `after_close` detached runner | An action must run after the popup closes (e.g. it opens another popup, which `ui_busy` blocks while this one is open) |
| Public `widget` / `store` / `run` / `observe` packages | An embedder needs one and `panel.View` doesn't cover it; otherwise at the v1 freeze |
| Event subscription for live lists | herdr gains a cursor for `events.subscribe`, or polling proves too slow |
| release-please / binary releases | Never needed while distribution is source-only |

## Working method

- At each milestone:
  1. build;
  2. pass the `make check` gate, and commit only on exit 0;
  3. run the negative controls;
  4. have an Opus reviewer subagent check the diff against AGENTS.md's rules;
  5. user review.
- Builder subagents, if any, run on Sonnet and reviewers on Opus. The model is always passed
  explicitly.
- Nothing touches the live herdr session; integration runs isolated (AGENTS.md rule 1).
- Commit and push to `AlexK-Notable/herdr-panel` after each unit of work (standing order). The
  repo stays private.
- README and AGENTS.md are updated in the same change as the code they describe. That
  includes the README's wrong `[[build]]` recipe and its package names, both fixed in M0.

## Verification (end to end)

1. `make check` is green on Linux and macOS CI (Go 1.26 and stable); govulncheck and gorelease
   are clean; the licence table matches `go list -m all`.
2. Unit, fuzz, property and golden tests pass, and every safety rule has a negative control
   that has been seen to turn red.
3. `herdr-panel validate examples/*.toml` passes, and the examples validate against the
   committed JSON Schema.
4. `herdr-panel observe examples/auto-title.toml --sandbox --script …` drives every widget by
   id.
5. `make integration` against an isolated herdr: open the popup, SGR click, save, check the
   file on disk changed, close with no orphans; `pane read` agrees with `x/vt`.
6. Both `[[build]]` forms install from the `v0.1.0` tag into an isolated herdr, and the popup
   opens by keybinding.
7. The auto-title panel works in the live session (M6), and the user confirms it.

## Adaptations made during the build

Each entry says what changed from the plan above, and why.

- **M0, lint set.** auto-title's `.golangci.yml` also enables layout rules (`wsl_v5`,
  `funlen`, `mnd`, `godot` and others). herdr-panel keeps the rules that guard its safety
  rules and correctness:
  - `depguard` (an import allowlist; `os/exec` only in `internal/run`; godotenv only in
    tests);
  - `forbidigo`, `gosec`, `errorlint`, `errcheck`, `nolintlint`, `gocritic`, `exhaustive`,
    `modernize` and the standard set.

  The pure layout rules are left out. The `os/exec` rule was checked with a negative
  control: a temporary import in `cmd/` failed both `depguard` and `forbidigo`.
- **M0, the `integration/` module** is created in M5 with its first test, not as an empty
  module now.
- **M1, the decoder** is built on go-toml's `unstable` parser: TOML is turned into a tree
  that keeps every key's and value's position, and the tree is turned into types. So every
  error has `file:line:col`, unknown keys and wrong types included. Plain struct decoding
  cannot do that for fields such as `source`, which accept two shapes.
- **M1, the JSON Schema** is written by hand in Go (`spec.JSONSchema`), not generated by
  invopop from the structs. The structs are not the TOML shape: commands take two forms,
  and field types are enums. The schema is committed as `schema/spec-v1.json`, with a test
  that it is current. The examples are validated against it with santhosh-tekuri/jsonschema
  as a **test dependency of the main module**, not a tools-module program, along with a
  negative control of five bad shapes it must reject.
- **M1, spec additions** forced by the examples: `confirm` is a template (so it can say
  `Delete {row.title}?`), plus the action `after` key and `create` on stores.
- **M2, unterminated quotes.** The fuzzer found a file the store could corrupt: after an
  unclosed `A="`, an appended line is read back as part of A's value. No rewrite can make
  such a file safe, so `Save` refuses it with `ErrUnterminated` and the line number. `Load`
  still reads it, ignoring the broken line, as godotenv does. The failing input is kept as
  a fuzz seed.
- **M3, the text input is our own** (`internal/ui/editor.go`, about 120 lines), not Bubbles'
  `textinput`. The latter's virtual cursor blinks on a timer, so `observe` frames and golden
  files would not be deterministic. Bubbles is therefore not a dependency.
- **M3, the dialog backdrop is in the logic, not a layer.** A full-screen ID'd layer would
  have to draw spaces over the panel. Instead, while a dialog is open, a click hits only the
  dialog's elements, and every other click is swallowed. `TestDialogIsModal` proves it.
- **M3 took the runner forward from M4** (`internal/run`): the UI's buttons, apply and lists
  need it. Mutation checks confirmed each guarantee: removing the group kill, `WaitDelay` or
  the `ok:false` rule each turns its test red.
- **M1 review fixes** (Opus review, `misc/reviews/m1-spec-review.md`):
  - **Secrets cannot be placeholders.** `{field.<secret>}` in any command or `confirm` is a
    validation error. Before, the UI just had no value for it, so the spec failed at run time.
  - **`Validate` serves specs built in Go.** It fills the defaults the TOML decoder fills
    (id, label, bool values, timeouts, `after`, id column, column titles) and repeats the
    decoder's shape checks. A bool `Default` is stored as written: with `TrueValue: "1"`, write
    `Default: "1"`. A test proves a second `Validate` changes nothing.
  - **A `requires` refusal is the only error shown**, because a newer spec's other "errors"
    are most likely keys the newer version added. `HostVersion` treats a `replace`d module
    (a `go.work` build) as devel.
  - **Shortcut names are checked** against Bubble Tea's naming (`ctrl+O` and `shift+a` are
    rejected), `delete` and `backspace` are reserved, and **`h j k l` are not**: a spec
    shortcut is matched before the built-in keys, so it takes the vim alias over while the
    arrows still move. That keeps the auto-title example's `h` = "hand back".
  - The program (argv[0]) cannot come from `{row.*}` or `{field.*}`. Two field ids that map
    to the same `PANEL_FIELD_` name are rejected.
  - `validate` exits 1 when any file was unreadable, even after a spec error (it took the
    larger code, so 3 won). `docs/security.md` is written: the trust model, argument
    injection and `--`, and the shell caveat.
  - `FuzzParse` also checks that the JSON Schema accepts everything `Parse` accepts.
- **M3, `panel.View` is line-based, not a Lip Gloss layer.** The plan sketched
  `Layer(Theme, Size) *lipgloss.Layer`. The renderer draws the body as one scrolling column
  of lines, so a free layer would neither scroll nor clip with its section. A view instead
  returns `RenderOutput{Lines, Regions, Cursor}` from `Render(RenderInput)`. Structs in and
  out, so fields can be added without breaking anyone. Regions become elements
  `view:<name>:<region>` and are hit-tested like everything else. A view's commands are
  wrapped: a panic shows the crash screen, results return only to that view, `tea.Batch`
  and `tea.Sequence` are unpacked (Bubble Tea's sequence message is unexported, so it is
  recognised by its shape), and `tea.Quit` becomes a close that asks about unsaved edits.
  Mutation checks turned each of the three red. The spec gained `kind = "view"` with
  `view = "<name>"`; the CLI validates such a spec but refuses to run it (exit 1).
- **M3, `paneltest` reaches the panel's internals through `internal/bridge`**: `panel` sets
  a function there in `init`, so `paneltest` builds a panel exactly as `Run` does without
  `panel` exporting internals or the view adapter being written twice. `panel.Options`
  gained `Width`/`Height` (for a test or a non-terminal output) and `Theme`; `panel.Theme`
  has the internal theme's fields, so it converts directly.
- **M3, the startup budget** is measured through a real Bubble Tea program writing to a
  buffer, from `RunFile` to the first full frame: a median of about 20 ms over 7 runs of
  the auto-title example (`TestFirstFrameBudget`, `BenchmarkFirstFrame`). Under the race
  detector the time is logged, not checked.
- **M3, the close cadence** is tested in a subprocess (`TestCloseCadence`): a command whose
  SIGHUP-ignoring grandchild is reachable only by the process-group kill. SIGHUP and SIGTERM
  each end the panel in about 2 ms with exit 129 or 143, and the grandchild dies. Killing
  only the direct child turned the test red. The helper is started with `os.StartProcess`,
  so `os/exec` stays confined to `internal/run`.
- **M3, `observe.KeyMsg` could not press `+`**: the key name split on `+`. Fixed and covered.
- **M4 needed no new code.** M3 had built the runner, apply, lists and row actions. M4's
  negative controls were proven by mutation: no output cap, no one-poll guard, no stale
  check, reselect by position, and a timeout ×1000 each turned their test red. M3 had
  already done the same for the group kill, `WaitDelay` and `ok:false`.
- **M5, a popup has no pane id.** The integration test found what the schema hides:
  herdr 0.9.2 answers `plugin.pane.open` for a **popup** with a bare `{"type":"ok"}`
  (`panes.rs` `open_plugin_popup_pane`). A popup is not in `pane list` and cannot be read or
  closed by id. So `herdr.Opened` is empty for a popup, and `Client.ClosePopup` (socket
  `popup.close`, found in `api.rs`) is the only outside close. The integration suite checks
  `pane read` against the screen on an **overlay** of the same panel, which does have an
  id. The popup is closed with `popup.close`, which sends the real signal cadence.
- **M5, `pane read` and the screen agree only once a frame settles.** A single snapshot
  failed 5 of 6 runs (the overlay is resized as it opens and repaints). The test polls until
  they agree, for up to 5 s: 6 of 6 passed. A positive control (one word changed on the
  screen copy) made it fail.
- **M5, the isolated herdr skips onboarding** with `onboarding = false` in its own
  `config.toml`; the welcome dialog is modal and covered the popup.
- **M5, `x/vt` has no releases**, so the integration module pins today's pseudo-version
  (`v0.0.0-20261011004359-2acd7e345453`). It is a test-only dependency in its own module.
- **M5, a crash inside herdr also sends a notification** naming the crash log
  (`TestCrashNotifiesHerdr`, against a fake socket).
- **M3 review fixes** (Opus review, `misc/reviews/m3-review.md`). Every fix has a test that a
  mutation turned red, file restored byte-identical afterwards:
  - **H1** A panic in `Update` returned a nil model to Bubble Tea; `Update` now names its
    results so the recover returns the panel showing the crash screen. **H2** "Save and close"
    cancelled the apply before it ran: the close now waits until every store is saved *and*
    applied, and any failure keeps the panel open.
  - **M1** A click elsewhere commits the field being typed in (a click on `[Save]` saves it).
    **M2** The binary shows the "no such view" screen before refusing (exit 1). **M3** Only
    Bubble Tea's runtime requests from a view's command reach the runtime; every other
    message returns to the view. **M4** Live mode re-saves an edit made while a save was in
    flight. **M5** A load that started before a save cannot overwrite the save's snapshot.
  - **M6** Dialogs fit the screen: a picker's options window around the cursor, long text
    scrolls (↑ ↓ PgUp PgDn, and the wheel) behind "… N more" markers, the frame is cropped,
    and a dialog hidden by "make me bigger" takes only `esc` and `ctrl+c`.
  - **M7** The wheel and PgDn scroll the body; the view returns to the focus only when the
    focus or a list's selection moves. The wheel never moves the body behind a dialog.
  - **M8** The headless driver behaves like Bubble Tea where it hid H1 and H2: it uses the
    model `Update` returns, stops at `QuitMsg`, and the sandbox refuses a cancelled command.
  - **M9** `panel.Tick` replaces `tea.Tick` for views. Under a driver (`NoTimers`) every
    timer, the panel's own polling included, is parked on a clock that only
    `Driver.Advance` / `paneltest.Panel.Advance` / the `advance` script verb moves; ticks
    fall due in order, re-armed ones included. A view ticking every second is driven
    through 4 s in under a millisecond.
  - Lows: a view focused at startup is told (L1); docs give row ids as list indices (L2);
    the conflict dialog ignores `y`/`n`, since its first button drops your edits (L3);
    **`paneltest.Config` takes `Views`, `Theme`, `Context`, `Via`** instead of a
    `panel.Options` whose IO and size it ignored (L4, breaking); paneltest and `observe
    --sandbox` take `create` from the spec (L5); a bad or duplicate `Region.ID` shows the
    crash screen (L6); the signal goroutine ends with `Run` (L7); `Run` waits up to 50 ms
    for killed commands to end before returning (L9); `tea.Sequence` order is asserted
    (L8). Also: command messages lose escape sequences and line breaks before reaching the
    status line, and a list or text refresh asked for during a load runs once it answers.
  - **Found on the way: `os.Stderr` raced between two `Run`s** in one process (the new
    parallel `TestMissingViewIsShownThenRefused` exposed it under `-race`). One panel at a
    time owns the stderr redirect; another leaves stderr alone and says so on crash.
- **M6, the auto-title fork** (`~/repos/herdr-auto-title.panel`, branch `feat/settings-panel`;
  commits `0e7d3cf`, `f9efbbf`, `2113281`):
  - **The panel asks the plugin which file to edit.** The plugin reads `config.env` from the
    first of `$XDG_CONFIG_HOME`, `~/.config` and the platform directory that already holds it,
    which a spec template cannot express (on macOS it may be Application Support). The popup
    runs `herdr-auto-title settings`, which resolves the path with the plugin's own code,
    exports `HERDR_AUTO_TITLE_CONFIG_FILE`, and execs `bin/herdr-panel panel.toml`; the
    spec's store path is `{env.HERDR_AUTO_TITLE_CONFIG_FILE}`.
  - **The action opens the popup over the socket** (`herdr-auto-title settings open`, using
    the fork's own client and `plugin.pane.open`), so the manifest stays shell-free, as the
    fork's other entries are. `ui_busy` becomes a notification.
  - **Apply is the plugin's own `restart`**, not `herdr plugin action invoke`: no PATH lookup
    of `herdr` from inside a popup. Its new instance is started with `Setsid`, so it outlives
    the popup; `timeout_s = 30` covers the old instance's 15 s to leave.
  - **No `go.mod` pin and no herdr-panel `[[build]]` yet.** herdr-panel is private and untagged,
    so a committed `require`/`tool` line would point at a pseudo-version every other clone and
    CI could not fetch. `make panel` builds `bin/herdr-panel` from a checkout
    (`HERDR_PANEL_DIR`, default `../herdr-panel`), which a linked plugin needs anyway; no
    `go.work` is needed either. The pin and `[[build]]` come with the `v0.1.0` tag (M8).
  - **Verified in an isolated herdr** by `TestAutoTitleSettingsPanel` (opt-in, see
    `docs/herdr.md`). Writing it found that redirecting XDG directories is not isolation for a
    plugin that falls back to `~/.config`: the test also redirects `HOME` and digests the real
    files before and after.
  - **The fork is public; herdr-panel is private.** Its branch names herdr-panel (README link,
    Makefile), so it is committed locally and not pushed until the user decides.
