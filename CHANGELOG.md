# Changelog

All notable changes are listed here. herdr-panel is at v0: a minor version
may break the spec or the Go API, and every such break is listed under
**Breaking**.

## Unreleased

## v0.1.0 — 2026-10-11

The first release.

- Project scaffolding: module, lint and test gate, CI on Linux and macOS.
- The spec format, version 1 (`spec` package): TOML decoding with `file:line:col`
  errors, validation, placeholders, a JSON Schema, and the `validate` and `schema`
  commands. Reference: `docs/spec.md`.
- Safe writes (`internal/atomicfile`) and the env-file store: only edited lines change,
  symlinks stay links, modes are kept, concurrent edits are detected, and orphaned
  temporary files are swept.
- The panel (`panel.Run`, `panel.RunFile`): settings fields of six types, lists with row
  actions, buttons and text sections; mouse and keyboard; both save modes; dialogs for
  unsaved edits, conflicts and confirmations; a crash screen instead of a vanishing popup;
  SIGHUP, SIGTERM and SIGINT end it within herdr's 250 ms, killing running commands' whole
  process groups. The first frame takes about 20 ms.
- Commands run without a shell, with a timeout, an output cap and a process group
  (`internal/run`).
- `herdr-panel observe` drives a panel headlessly by element id, with `--sandbox`,
  `--stub` and `--script`, and prints versioned JSON.
- `paneltest`: the same from a Go test, with golden frames.
- Experimental: `panel.View`, a section drawn by Go code, placed by `kind = "view"`.
  Docs: `docs/embedding.md`, `docs/testing.md`, `docs/security.md`.
- `panel.Tick` for views that poll; `paneltest.Panel.Advance` and observe's `advance`
  move a test clock that list polling and view ticks follow.
- Dialogs fit small screens (scrolling text, windowed pickers); the wheel and PgDn scroll
  the panel.
- `herdr` package: the socket client, `Open` for popups and overlays, `ClosePopup`,
  `Notify`.
- A `tomlfile` store: top-level keys of a TOML file, edited line by line,
  typed by field, with comments and tables kept.
- A choice with no `default` can be set back to "not set", removing its key.
- `examples/file-viewer.toml`: settings for herdr-file-viewer.
- Clearing a field removes its key (always for a number, and for any field in a TOML
  file) instead of writing an empty value; a `required` field cannot be cleared.
- Text sections wrap long lines at words instead of cutting them.
- A store path that starts with an empty `{env.X}` is an error, not `/…`.
