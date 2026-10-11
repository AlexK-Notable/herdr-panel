# Changelog

All notable changes are listed here. herdr-panel is at v0: a minor version
may break the spec or the Go API, and every such break is listed under
**Breaking**.

## Unreleased

- Project scaffolding: module, lint and test gate, CI on Linux and macOS.
- The spec format, version 1 (`spec` package): TOML decoding with `file:line:col`
  errors, validation, placeholders, a JSON Schema, and the `validate` and `schema`
  commands. Reference: `docs/spec.md`.
- Safe writes (`internal/atomicfile`) and the env-file store: only edited lines change,
  symlinks stay links, modes are kept, concurrent edits are detected, and orphaned
  temporary files are swept.
