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
| `title` | string | required | Shown in the panel's top line. |
| `size` | table | none | `{ width, height }`. Each is a number of cells (`60`) or a percentage of the terminal (`"70%"`). Used when the panel is opened over herdr's socket; a size sent with that request overrides the manifest's `[[panes]]` size. |
| `save` | string | `"save_then_apply"` | See [Saving](#saving). |
| `[stores.<name>]` | table | none | See [Stores](#stores). |
| `[[section]]` | array of tables | required | At least one. They are shown in order. |

## Stores

A store is where field values live. Version 1 has one kind, `envfile`: a
`KEY=value` file. Comments, blank lines, key order, `export` prefixes and
quoting are kept exactly. Only the edited value changes on disk.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `kind` | string | required | `"envfile"`. |
| `path` | string | required | May use `{env.*}` and `{ctx.*}`. A leading `~` is the home directory. A relative path is relative to the spec file's directory. |
| `create` | boolean | `true` | Create the file (mode 0600) on first save when it does not exist. |
| `apply` | command | none | Run after a save that changed this store, e.g. to restart the program that reads it. |

A value that is not in the file shows its field's `default`. A key written
commented out (`#KEY=value`) is read as absent. On first save it becomes a
real line in that place.

## Sections

Every section has an optional `title` and a `kind`:

- `fields`, the default: settings fields;
- `list`: rows printed by a command, with actions on each row;
- `buttons`: actions;
- `text`: fixed text, or the output of a command.

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
| `key` | string | all | The key in the store. For an env file it is letters, digits and `_`, and does not start with a digit. |
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
| `columns` | array | Column keys, or `{ key, title, width }` tables. A column without a width shares the space left over. |
| `id_column` | string | The key that identifies a row, `"id"` by default. The selection follows the row with that id across refreshes. |
| `empty` | string | Shown when there are no rows. |
| `row_actions` | array of actions | Run against the selected row. They may use `{row.<key>}`. |

### `buttons`

```toml
[[section]]
kind = "buttons"
buttons = [ { label = "Restart", key = "R", run = ["./my-plugin", "restart"], confirm = "Restart now?" } ]
```

### `text`

`text = "..."` shows fixed text. A `source` command instead shows the
command's output, which `every_s` can refresh.

## Actions

| Key | Type | Default | Meaning |
|---|---|---|---|
| `label` | string | required | The button's text. |
| `run` | array | required | The command. |
| `key` | string | none | A shortcut, named the way Bubble Tea names keys: `r`, `R`, `ctrl+o`, `f5`. Shortcuts are unique in the panel, and the reserved keys `? / q esc enter space tab shift+tab`, the arrows, `pgup pgdown home end ctrl+c ctrl+s ctrl+r` cannot be used. |
| `confirm` | string | none | A question asked inside the panel first. It may use placeholders. |
| `timeout_s` | integer | `10` | 1 to 3600. |
| `after` | string | `"refresh"` | `refresh` reloads lists and stores, `close` closes the panel, `none` does nothing. |

## Commands

A command is an array, `["program", "arg", ...]`, or a table,
`{ run = [...], timeout_s = 5 }`. It runs **without a shell**:

- **Placeholders** expand inside each element on its own. `{row.title}`
  holding `a b; rm -rf ~` is passed as one argument, exactly as written.
- **The program**:
  - a relative program (`./bin/x`, `bin/x`) is relative to the spec's
    directory;
  - a bare name (`herdr`) is looked up on `PATH`.
- **The environment** is inherited from the panel, including
  `HERDR_SOCKET_PATH`, so a command can call `herdr`. It also gets
  `PANEL_FIELD_<ID>` for every field (except secrets), `PANEL_ROW_<KEY>` for
  the selected row, and `PANEL_VIA`.
- **Success**: exit status 0 is success, unless the command prints
  `{"ok": false, "message": "..."}`. The message is shown either way.
- **Limits**: a timeout (`timeout_s`, 10 s by default) and an output cap
  bound every command. On timeout, the command and every process it started
  are killed.

## Placeholders

| Placeholder | Value | Where it can be used |
|---|---|---|
| `{row.<key>}` | The selected row's value | row actions and their `confirm` |
| `{field.<id>}` | A field's current value, as stored | commands; not store paths |
| `{ctx.<name>}` | `workspace_id`, `tab_id`, `pane_id`, `cwd`, `plugin_id`, `config_dir`, `state_dir`, `spec_dir` | everywhere |
| `{env.NAME}` | An environment variable | everywhere |
| `{env.NAME:-default}` | The variable, or `default` when it is unset or empty | everywhere |

`{{` and `}}` are literal braces. A placeholder without a value is an error
at run time. A placeholder that cannot have a value is an error when the
spec is validated: an unknown field id, an unknown `ctx` name, or `row`
outside a row action.

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
an older host explains instead of reporting an unknown key. A change that
breaks existing specs is spec version 2. herdr-panel reads every version it
knows.
