# Testing a panel

Two tools drive a panel without a terminal, through the same compositor that
draws it, so a click lands exactly where a user's would:

- **`herdr-panel observe`**: from the command line, any language, any spec.
- **`paneltest`**: from a Go test, with views.

Both identify what is on screen by **element id**:

| Id | Element |
|---|---|
| `field:<id>` | a settings field |
| `test:<id>` | a secret field's test button |
| `list:<section>` | a list (its filter, its selection) |
| `row:<section>:<row>` | a row, by its index in the list as filtered (not its line on screen: after scrolling, the first row drawn may be `row:1:12`) |
| `rowaction:<section>:<n>` | a row action's button |
| `button:<section>:<n>` | a button |
| `view:<name>:<region>` | a region of a view |
| `save`, `revert`, `close`, `help` | the footer |
| `dialog:<n>`, `option:<n>` | a dialog's buttons, a picker's options |

`<section>` counts sections from 0.

## `observe`

```sh
herdr-panel observe panel.toml --size 80x24 --sandbox --json
```

prints the frame, every element with its box, the focus and the status.

- `--sandbox` runs **no commands**: each is recorded with the argv and
  `PANEL_*` variables it would have received, and answered with success
  and no output. Stores are copied to a temporary directory first, so a
  save never touches the real file.
- `--stub stubs.json` answers commands instead:

  ```json
  {
    "commands": [
      { "match": ["my-tool", "rows"], "stdout": "[{\"id\": \"a\", \"label\": \"Alpha\"}]" },
      { "match": ["my-tool", "restart"], "stderr": "not running", "exit": 1 }
    ],
    "default": { "stdout": "" }
  }
  ```

  `match` is a prefix of the argv. Its first element matches by base name
  when it has no slash.
- `--script steps.txt` (or `-` for stdin) acts before printing, one step a
  line:

  ```
  click field:VERBOSE
  key ctrl+s
  type hello
  paste some text
  resize 60x20
  advance 5s
  snapshot after-save
  ```

  Each `snapshot` adds a frame to the output. Time stands still in
  `observe`: `advance` moves the clock, and the list polls and view ticks
  that fall due in that span run, in order.

The JSON has `"observe": 1`, its format version. Fields are only added
within version 1.

## `paneltest`

```go
func TestSettings(t *testing.T) {
	s, err := spec.Load("panel.toml")
	if err != nil {
		t.Fatal(err)
	}
	p := paneltest.New(t, s, paneltest.Config{
		Stubs: []paneltest.Stub{{Match: []string{"my-tool", "restart"}}},
	})
	p.Click("field:VERBOSE")
	p.Key("ctrl+s")
	p.RequireFrame("✓ saved and applied")
	if got := p.StoreText("config"); got != "VERBOSE=true\n" {
		t.Errorf("store = %q", got)
	}
	p.Golden()
}
```

- **Nothing runs.** Every command is answered by a stub; one no stub matches
  succeeds with no output. `p.Calls()` lists what was asked, with argv and
  `PANEL_*` variables.
- **Stores live in `t.TempDir()`**, never at the spec's path, unless
  `Config.Stores` names a file. `p.StorePath` and `p.StoreText` read them.
- **Time stands still**, so a frame depends only on what the test did. A
  list shows its source's rows from startup, and refreshes after actions.
  `p.Advance(d)` moves the clock: list polls (`every_s`) and a view's
  `panel.Tick` that fall due run, in order, including ticks re-armed on the
  way. A view that uses `tea.Tick` instead sleeps for real and cannot be
  driven; use `panel.Tick`.
- **A store's `create` setting applies.** A store not named in
  `Config.Stores` starts with no file when the spec lets the panel create
  it, else with an empty one, as a plugin would ship it.
- **Views** go in `Config.Views`, as in `panel.Options`; so do `Theme`,
  `Context` and `Via`.
- **Every method fails the test** on error: an element not on screen, an
  unknown key name, a crash inside the panel.
- **Golden frames**: `p.Golden()` compares the frame with
  `testdata/<test name>.golden`. Set `PANELTEST_UPDATE=1` to write it.

`Config.Context` defaults to an empty map, not herdr's environment,
so a test gives the same result inside and outside herdr.
