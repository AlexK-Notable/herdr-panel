# Embedding herdr-panel in a Go program

Most plugins run the `herdr-panel` binary on a spec file and need no Go at
all. Embed the library when you want one of these:

- a panel inside your own binary, with no second program to build;
- a spec built in Go rather than written in TOML;
- a section drawn by your own code: a **view**.

The public packages are `panel` (run a panel), `spec` (load, build and
validate specs), `herdr` (the popup's context) and `paneltest` (drive a
panel in tests). Everything under `internal/` may change at any time.

## Running a spec

```go
err := panel.RunFile(ctx, "panel.toml", panel.Options{Via: "keybinding"})
```

`RunFile` loads the spec and, if it has problems, shows them inside the
panel until a key is pressed: in a herdr popup, a process that exits takes
its message with it. `Run` takes a spec you already have:

```go
s, err := spec.Load("panel.toml")      // or spec.Parse(data, name, dir)
err = panel.Run(ctx, s, panel.Options{})
```

What `Run` returns:

| Error | Meaning | The CLI exits |
|---|---|---|
| `nil` | the user closed the panel | 0 |
| `*panel.SignalError` | a signal ended it; herdr closes a popup with SIGHUP | 128 + signal |
| wraps `panel.ErrSpec` | `RunFile` could not load the spec, and showed why | 3 |
| wraps `panel.ErrNoView` | the spec shows a view that `Options.Views` lacks | 1 |
| `panel.ErrCrashed` | the panel caught a panic, showed it and wrote a crash log | 1 |
| `ctx.Err()` | your context ended | 1 |

### Options

| Field | Default | Meaning |
|---|---|---|
| `Input`, `Output` | stdin, stdout | the terminal |
| `Width`, `Height` | ask the terminal | the size, when `Output` cannot report one |
| `Via` | `""` | passed to every command as `PANEL_VIA` |
| `Context` | read from herdr's environment | the `{ctx.*}` values |
| `CrashDir` | herdr's state directory for the plugin | where a crash log goes |
| `Theme` | `DefaultTheme()` | styles; see below |
| `Views` | none | the spec's view sections, by name |

## Building a spec in Go

```go
s := &spec.Spec{
	Title:  "Settings",
	Stores: []*spec.Store{{Name: "c", Kind: spec.EnvFile,
		Path: spec.MustTemplate("~/.config/my-plugin/config.env"), Create: true}},
	Sections: []*spec.Section{{Fields: []*spec.Field{
		{Key: "VERBOSE", Type: spec.Bool, Label: "Verbose logging"},
	}}},
}
if err := spec.Validate(s); err != nil { ... }
```

`Validate` fills in what the TOML decoder would (a field's id and label
from its key, the lone store, bool values `"true"`/`"false"`, timeouts,
`after = refresh`), then checks everything `Parse` checks. Calling it again
changes nothing. Two differences from TOML:

- A bool `Default` is stored as written: with `TrueValue: "1"`, write
  `Default: "1"`, not `"true"`.
- A store creates its file on first save only when `Create` is set.

## Views (experimental)

A view is a section your code draws. A spec places it by name:

```toml
[[section]]
title = "Usage"
kind  = "view"
view  = "usage"
```

and the program supplies it:

```go
panel.Run(ctx, s, panel.Options{Views: map[string]panel.View{"usage": &usageView{}}})
```

A `View` has two methods:

```go
Render(panel.RenderInput) panel.RenderOutput
Update(tea.Msg) tea.Cmd
```

**Render** gets the width, whether the view has the focus, and the theme. It
returns lines, the clickable regions on them, and the line to keep on screen
while focused. Regions are in cells from the start of the line, after any
styling, so `lipgloss.Width` is the right ruler. A region past the end of its
line, or overlapping an earlier one, is dropped. A region's `ID` is letters,
digits, `_` and `-`, unique within the view; anything else is a programming
error and shows the crash screen.

**Update** receives:

| Message | When |
|---|---|
| `tea.KeyPressMsg`, `tea.PasteMsg` | the view has the focus |
| `panel.Click{Region, X, Y}` | a region was clicked; X and Y are within the region |
| `panel.Focus{Focused}` | the view gained or lost the focus |
| anything else | a message one of **its own** commands returned |

While a view has the focus, the panel keeps `tab`, `shift+tab`, `esc`,
`ctrl+c`, `ctrl+s` and `ctrl+r`. Every other key goes to the view, `q` and
`?` included, and so do the action shortcuts of the spec.

Commands work as in any Bubble Tea program, with three differences:

- **Results come back to the view that issued them**, never to the panel or
  another view. `tea.Batch` and `tea.Sequence` work; each part's result
  comes back.
- **`tea.Quit` asks the panel to close**, the way `esc` does: unsaved edits
  are offered first.
- **A panic** in `Render`, `Update` or a command shows the panel's crash
  screen and writes a crash log. The popup does not vanish.

**To poll, use `panel.Tick`**, not `tea.Tick`: its message comes back to
the view the same way, and under `paneltest` and `observe` time stands still
until the test advances it, where `tea.Tick` would sleep for real. Re-arm it
from `Update` for a repeating poll.

A view that has the focus when the panel starts is sent `panel.Focus{true}`
then, as on any later focus change.

Style with the theme you are given (`in.Theme.Focus`, `.Error`, `.Muted`…),
not your own colours. The default theme uses the 16 ANSI colours, so the
panel follows the terminal's palette, and `NO_COLOR` is honoured.

The `herdr-panel` binary supplies no views: it validates a spec with one, but
refuses to run it (exit 1). `panel.CheckViews(s, views)` makes the same check
up front.

The View API may change in a minor release until the v1 freeze. A runnable
example is `Example_view` in `panel/example_test.go`.

## Theme

`panel.Theme` maps the panel's roles to `lipgloss.Style`s: `Title`,
`Section`, `Label`, `Value`, `Muted`, `Focus`, `Selected`, `Dirty`, `Error`,
`OK`, `Rule`, `Button`, `DialogTitle`, `Dialog`. Start from
`panel.DefaultTheme()` and change what you need. Keep a mark that is not a
colour on `Focus`, `Error` and `Dirty` (reverse, bold, a glyph): meaning
must never rest on colour alone.

## Signals and teardown

The panel handles SIGHUP, SIGTERM and SIGINT itself. Any of them cancels
running commands (their whole process group is killed) and ends `Run` with
a `*SignalError`, within herdr's 250 ms between signals. Do not install
your own handler for them around `Run`.

Anything that must outlive the popup has to start in its own session
(`Setsid`). herdr signals the popup's whole session, and nothing else
survives.
