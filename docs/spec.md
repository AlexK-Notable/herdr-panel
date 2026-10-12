# The spec format, version 1

A spec is a TOML file describing one panel: the stores that hold values,
the sections it shows and the commands behind them.

- `herdr-panel validate <spec>` checks it. Every problem is reported as
  `file:line:col: message`, all at once.
- `herdr-panel schema` prints a JSON Schema, also published as
  [`schema/spec-v1.json`](../schema/spec-v1.json), for editors and agents.
- Examples are in [`examples/`](../examples).

Unknown keys are errors, everywhere. A misspelt `lable` is reported, not
ignored.

**A spec runs commands, so a spec is code.** See [security.md](./security.md).

## Top level

| Key | Type | Default | Meaning |
|---|---|---|---|
| `spec` | integer | required | The format version: `1`. |
| `requires` | string | none | The oldest herdr-panel that can run this spec, written `">=0.3"`. An older host refuses with that message. |
| `title` | string | required | Shown in the panel's top line. In a plugin's herdr popup, herdr's frame shows the manifest's pane title instead, and the panel leaves this line out (see [docs/herdr.md](herdr.md#popups-what-herdr-does)). |
| `size` | table | none | `{ width, height }`. Each is a number of cells (`60`) or a percentage of the terminal (`"70%"`). Used when the panel is opened over herdr's socket; a size sent with that request overrides the manifest's `[[panes]]` size. |
| `save` | string | `"save_then_apply"` | See [Saving](#saving). |
| `[stores.<name>]` | table | none | See [Stores](#stores). |
| `[[section]]` | array of tables | required | At least one. They are shown in order. |

## Stores

A store is where field values live. Two kinds:

- **`envfile`**: a `KEY=value` file. Comments, blank lines, key order,
  `export` prefixes and quoting are kept exactly.
- **`tomlfile`**: a TOML file whose **top-level** keys hold the fields.
  Comments, tables, key order and every other key are kept exactly; tables
  are never edited.

Either way, only the edited lines change on disk, and a save that would
leave a file its program cannot parse is refused.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `kind` | string | required | `"envfile"` or `"tomlfile"`. |
| `path` | string | required | May use `{env.*}` and `{ctx.*}`. A leading `~` is the home directory. A relative path is relative to the spec file's directory. A path that starts with `{env.X}` needs `X` set and non-empty. |
| `create` | boolean | `true` | Create the file (mode 0600) on first save when it does not exist. |
| `apply` | command | none | Run after a save that changed this store, e.g. to restart the program that reads it. |

A value that is not in the file shows its field's `default`. A key written
commented out (`#KEY=value`, `#key = value`) is read as absent. On first
save it becomes a real line in that place, so a setting lands where the
file documents it.

In a `tomlfile`:

- A field is written as the TOML type its program reads: a `bool` as
  `true`/`false`, an `int` or `duration_s` as an integer, everything else
  as a string. A `bool`'s `true_value`/`false_value` do not apply.
- A commented default is a comment whose text is itself a one-line
  `key = value`; prose such as `# editor = the program e opens` is not
  one, and nor is a line inside another key's multi-line value.
- A key with no commented default goes after the last top-level key, or,
  if there is none, above the first table header, **commented-out headers
  included**: uncommenting `#[keys]` later must not pull the new key into
  that table. (Once the file has top-level keys below such a header, new
  keys join them there.)
- A value spanning several lines (a multi-line string or array), and a key
  holding an inline table, an array or a date, are left for you to edit by
  hand; so is a key that names a table.
- A comment after a value (`n = 3 # why`), or after a commented default,
  is kept. Line ends (CRLF, a missing final one) and a byte order mark are
  kept.
- **Clearing a field removes its key** rather than writing `key = ""`: the
  program then uses its own default. (A cleared `int` or `duration_s` is
  removed from an env file too.)

## Sections

Every section has an optional `title` and a `kind`:

- `fields`, the default: settings fields;
- `list`: rows printed by a command, with actions on each row;
- `buttons`: actions;
- `text`: fixed text, or the output of a command;
- `view`: drawn by Go code (experimental; see below).

### `fields`

```toml
[[section]]
title = "Naming"
fields = [
  { key = "NAME_AGENTS", type = "bool", label = "Name agents", default = true },
]
```

| Key | Type | Applies to | Meaning |
|---|---|---|---|
| `key` | string | all | The key in the store. For an env file it is letters, digits and `_`, and does not start with a digit. For a TOML file it is a bare top-level key: letters, digits, `_` and `-`. |
| `type` | string | all | `bool`, `choice`, `int`, `duration_s`, `text` or `secret`. |
| `id` | string | all | Names the field in `{field.<id>}`. Defaults to `key`, and must be unique in the spec. |
| `store` | string | all | Needed only when the spec has more than one store. |
| `label` | string | all | Defaults to `key`. |
| `help` | string | all | A line shown under the focused field. |
| `default` | scalar | all but `secret` | Shown when the store has no value. |
| `required` | boolean | all | An empty value blocks Save. |
| `true_value`, `false_value` | string | `bool` | What is written to the file: `"true"` and `"false"` by default, or e.g. `"1"` and `"0"`. A `default` of `true` or `false` maps onto them. |
| `choices` | array | `choice` | Strings, or `{ value, label }` tables. |
| `min`, `max` | integer | `int`, `duration_s` | Inclusive bounds. |
| `pattern` | string | `text` | A regular expression the whole value must match (RE2 syntax). |
| `max_len` | integer | `text`, `secret` | In characters. |
| `placeholder` | string | `text` | Shown, dimmed, while the value is empty. |
| `test` | command | `secret` | A button that runs this command. The command reads the secret itself; herdr-panel never passes it. |

The types:

- **`duration_s`** stores whole seconds. The panel shows `1m30s` and accepts
  either form.
- **`secret`** is never displayed. The panel shows *set* or *unset*, and
  entering a new value replaces it. A secret field cannot have a `default`,
  because the spec file would then hold the secret.
- **A value on disk that is not one of a choice field's `choices`** is shown
  as `custom: <value>` and left untouched unless you change it.
- **A choice field with no `default`** can also be left out of the file, so
  that its program decides. It shows as `not set`, and its picker offers
  *not set* first, which removes the key. Not for a `required` field, nor
  when one of the choices is already `""`.
- **`required`**: an empty value cannot be saved, and the field cannot be
  reset with Delete.

### `list`

```toml
[[section]]
title   = "Workspaces"
kind    = "list"
source  = { run = ["./my-plugin", "rows"], every_s = 5 }
columns = [ { key = "label", title = "Workspace" }, { key = "locked", width = 8 } ]
row_actions = [
  { label = "Rename now", key = "r", run = ["./my-plugin", "rename", "{row.id}"] },
]
```

| Key | Type | Meaning |
|---|---|---|
| `source` | command | Prints the rows as a JSON array of objects, or as `{"rows": [...]}`. Values are shown as text. |
| `every_s` | integer | Re-run the source this often. It may also sit inside the `source` table. Without it the list refreshes only after an action. |
| `columns` | array | **Required.** Column keys, or `{ key, title, width }` tables. A column without a width shares the space left over. |
| `id_column` | string | The key that identifies a row, `"id"` by default. The selection follows the row with that id across refreshes. |
| `empty` | string | Shown when there are no rows. |
| `row_actions` | array of actions | Run against the selected row. They may use `{row.<key>}`. |
| `id` | string | Names the list, so other sections can follow its selected row with `{sel.<id>.<key>}`. Letters, digits, `_` and `-`; unique. Only lists have one. |

#### Following another list's selection

A section can show something about the row selected in a list: give the
list an `id`, and use `{sel.<id>.<key>}` in the other section's `source`
(and its `title`, row actions or buttons).

```toml
[[section]]
id = "plugins"
kind = "list"
source = ["./x", "plugins"]
columns = ["name"]

[[section]]
title = "Keys for {sel.plugins.name}"
kind = "list"
source = ["./x", "keys", "{sel.plugins.id}"]
columns = ["key", "action"]
empty = "Select a plugin."
```

- The following section runs its source again whenever the selected row
  changes, or its values change when the list refreshes. A selection moved
  by hand rests 150 ms first, so arrowing past rows runs nothing for them.
- While nothing is selected (an empty list, or a filter that hides every
  row), the following section runs nothing and shows its `empty` text; it is
  not an error. In a title, a value without a selection shows as `…`.
- `{sel.<id>.<key>}` reads any key of the selected row's JSON, shown in a
  column or not; a key the row lacks is empty.
- A list cannot follow its own selection, directly or through other lists:
  that is a spec error.

### `buttons`

```toml
[[section]]
kind = "buttons"
buttons = [ { label = "Restart", key = "R", run = ["./my-plugin", "restart"], confirm = "Restart now?" } ]
```

### `text`

`text = "..."` shows fixed text. A `source` command instead shows the
command's output, which `every_s` can refresh. Lines wrap at words to the
panel's width (a word longer than the line is broken), and at most 200 lines
are shown.

A text section with nothing to show takes no room, not even the gap between
sections: blank text, or an untitled section whose source has not answered
yet. So a `source` that prints only when there is something to say
(overrides, warnings) can sit at the top of a panel. A titled text section
keeps its title, and shows `loading…` while its source runs.

### `view` (experimental)

```toml
[[section]]
title = "Usage"
kind  = "view"
view  = "usage-graph"
```

A section drawn and driven by Go code. A program that embeds herdr-panel
supplies it with `panel.Options.Views["usage-graph"]` (see
[embedding.md](./embedding.md)). `herdr-panel validate` accepts the spec, but
the `herdr-panel` binary has no views to supply, so `herdr-panel run` and
`observe` refuse it with exit status 1. A view name is used once per spec.

## Actions

| Key | Type | Default | Meaning |
|---|---|---|---|
| `label` | string | required | The button's text. |
| `run` | array | required | The command. |
| `key` | string | none | A shortcut, named the way Bubble Tea names keys: `r`, `R`, `?`, `ctrl+o`, `alt+x`, `f5`. A letter after `ctrl+` or `alt+` is lower case, and `shift+` never precedes a letter (write `R`, not `shift+r`). Shortcuts are unique in the panel. The reserved keys `? / q esc enter space tab shift+tab`, the arrows, `pgup pgdown home end delete backspace ctrl+c ctrl+s ctrl+r` cannot be used. The vim keys `h j k l` can: a shortcut on one of them wins, and the arrows still move. |
| `confirm` | string | none | A question asked inside the panel first. It may use placeholders. |
| `timeout_s` | integer | `10` | 1 to 3600. |
| `after` | string | `"refresh"` | `refresh` reloads lists and stores, `close` closes the panel once the command succeeds, `none` does nothing. `close_then_run` closes the panel **first** and runs the command once the panel has exited (see below). |

**`after = "close_then_run"`** is for an action that opens something the
open panel is in the way of, such as another herdr popup (herdr allows one
at a time). The panel closes at once; the command runs, detached, after the
panel's process has exited, so it outlives the popup. Its result is never
shown, and `timeout_s` does not apply: a command that can fail should report
the failure itself (a herdr notification, say). The panel refuses it while
edits are unsaved. In herdr the popup may still be closing when the command
starts, so a command that opens another popup should retry on `ui_busy`.

## Commands

A command is an array, `["program", "arg", ...]`, or a table,
`{ run = [...], timeout_s = 5 }`. It runs **without a shell**:

- **Placeholders** expand inside each element on its own. `{row.title}`
  holding `a b; rm -rf ~` is passed as one argument, exactly as written.
- **The program**:
  - a relative program (`./bin/x`, `bin/x`) is relative to the spec's
    directory;
  - a bare name (`herdr`) is looked up on `PATH`.
- **The program cannot come from a row or a field**: `{row.*}` and
  `{field.*}` are not allowed in the first element. Pass such values as
  arguments.
- **The environment** is inherited from the panel, including
  `HERDR_SOCKET_PATH`, so a command can call `herdr`. It also gets
  `PANEL_FIELD_<ID>` for every field (except secrets), `PANEL_ROW_<KEY>` for
  the selected row, and `PANEL_VIA`. The name is the id or column key in
  upper case, with anything other than a letter or digit turned into `_`:
  field `max-len` becomes `PANEL_FIELD_MAX_LEN`. Two field ids that would
  get the same name (`a-b` and `a_b`) are an error.
- **Success**: exit status 0 is success, unless the command prints
  `{"ok": false, "message": "..."}`. The message is shown either way.
- **Limits**: a timeout (`timeout_s`, 10 s by default) and an output cap
  bound every command. On timeout, the command and every process it started
  are killed.

## Placeholders

Placeholders work in four places only: **store paths**, **commands**
(`run`, `source`, `apply`, `test`), **`confirm`** questions and **section
titles** (`{sel.*}`, `{ctx.*}` and `{env.*}` only). Everywhere else (labels,
help, `text`, `empty`) a `{` is just a brace; in a section title, write `{{`
for one.

| Placeholder | Value | Where it can be used |
|---|---|---|
| `{row.<key>}` | The selected row's value | row actions and their `confirm` |
| `{field.<id>}` | A field's current value, as stored | commands and `confirm`; not store paths; never a `secret` field |
| `{sel.<list>.<key>}` | A key of the row selected in list `<list>` | sources, actions, `confirm` and section titles; see [following a selection](#following-another-lists-selection) |
| `{ctx.<name>}` | `workspace_id`, `tab_id`, `pane_id`, `cwd`, `plugin_id`, `config_dir`, `state_dir`, `spec_dir` | all three places |
| `{env.NAME}` | An environment variable | all three places |
| `{env.NAME:-default}` | The variable, or `default` when it is unset or empty | all three places |

`{{` and `}}` are literal braces, also inside a `:-default`. A placeholder
without a value is an error at run time. A placeholder that cannot have a
value is an error when the spec is validated: an unknown field id, an
unknown `ctx` name, `row` outside a row action, or `sel` naming no list. A
`{row.*}`, `{field.*}` or `{sel.*}` placeholder can never be the program. So is `{field.<id>}` of a
`secret` field: a secret never reaches a command line, an environment
variable or the screen. A `test` command reads the secret from the store
itself.

Where the `ctx` values come from:

- **Inside a herdr plugin popup**, from `HERDR_PLUGIN_CONTEXT_JSON` and the
  plugin's directories.
- **Inside a popup opened by a `type = "popup"` keybinding**, from
  `HERDR_ACTIVE_*`.
- **Outside herdr**, only `cwd` and `spec_dir` have values.

## Saving

| `save` | Behaviour |
|---|---|
| `save_then_apply` | Edits are kept in the panel and marked as changed. `ctrl+s` (or the Save button) writes every changed store, then runs each one's `apply`. A field with an invalid value blocks Save. `esc` asks before discarding edits. |
| `live` | Each edit is written as soon as it is made, then applied. |

What happens in some cases:

- **The file changed on disk** since the panel read it: the panel does not
  overwrite it. It offers to reload, to overwrite (re-reading the file and
  re-applying only your edits), or to cancel.
- **A save succeeded but `apply` failed**: the panel says *saved, not
  applied* and shows the command's error.
- **Closing the popup from herdr** (its close button, or closing the pane)
  ends the panel at once: unsaved edits are discarded without a question.

## Compatibility

Within spec version 1, keys are only ever added, never removed or changed
in meaning. A spec that uses a newer key should say `requires = ">=X.Y"`, so
an older host explains instead of reporting an unknown key: when `requires`
refuses a spec, that refusal is the only error shown. A development build
(one built from a working tree, a `go.work` or a `replace`) accepts every
`requires`. A change that
breaks existing specs is spec version 2. herdr-panel reads every version it
knows.
