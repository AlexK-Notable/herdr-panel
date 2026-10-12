# herdr integration

herdr-panel runs in any terminal; package `herdr` is its only link to herdr.
Everything here was verified against **herdr 0.9.2-preview.2026-09-29
(`8e78f929d8f0`), socket protocol 22**, in source and by `make integration`.

## Opening a panel

| How | Size | Environment | When |
|---|---|---|---|
| A plugin's `[[panes]]` entry, opened from an `[[actions]]` entry or `herdr plugin pane open` | fixed in the manifest | the plugin's (`HERDR_PLUGIN_*`) | the usual way |
| A `[[keys.command]]` with `type = "popup"` in the user's `config.toml` | herdr's default | only `HERDR_ACTIVE_*` ids | a panel bound to a key, no plugin needed |
| `herdr.Client.Open` (socket `plugin.pane.open`) | chosen at run time | chosen at run time | a Go program that needs both |

A plugin manifest for a panel:

```toml
[[panes]]
id = "settings"
title = "Settings"
placement = "popup"
width = "70%"        # a number of cells, or "N%"
height = "70%"
command = ["./bin/herdr-panel", "panel.toml"]

[[actions]]
id = "settings"
title = "Settings"
contexts = ["workspace"]
command = ["sh", "-c", "exec \"$HERDR_BIN_PATH\" plugin pane open --plugin my.plugin --entrypoint settings --focus"]
```

`command` runs without a shell, in the plugin's directory; a relative
program is relative to it. A size given to `plugin.pane.open` wins over
the manifest's. `herdr plugin install` also requires the manifest's
top-level `min_herdr_version` (`plugin link` does not).

## Building herdr-panel in a plugin

herdr runs `[[build]]` only on `herdr plugin install`, never on `plugin
link`, and without a shell. So:

- **A Go plugin** pins herdr-panel as a tool (`go get -tool
  github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel@vX.Y.Z`) and builds
  it:

  ```toml
  [[build]]
  command = ["go", "build", "-o", "bin/herdr-panel", "github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel"]
  ```

- **A plugin in another language**:

  ```toml
  [[build]]
  platforms = ["linux", "macos"]
  command = ["sh", "-c", "GOBIN=\"$PWD/bin\" go install github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel@vX.Y.Z"]
  ```

  `go install` refuses a relative `GOBIN`, hence the shell.

- **A linked plugin** (development) builds its own binary, e.g. from its
  Makefile; `[[build]]` does not run for it.

herdr-panel needs Go 1.26. A Go plugin whose own `go.mod` supports an older
Go can keep it: pin herdr-panel in a module of its own (say `panel/go.mod`,
with `go 1.26.0` and the `tool` line) and build it with
`["go", "build", "-C", "panel", "-o", "../bin/herdr-panel", "github.com/AlexK-Notable/herdr-panel/cmd/herdr-panel"]`.
With `-C`, an older `go` command fetches Go 1.26 by itself (unless
`GOTOOLCHAIN=local`). A pin read with `-modfile` instead does not: toolchain
selection reads only the main module's `go.mod`, so on an older Go that build
fails, and a failed `[[build]]` stops the whole install.

`[[build]]` runs with the environment of the `herdr plugin install` command,
so Go settings (`GOPRIVATE`, `GOPROXY`, `GOFLAGS`) come from there. Both
forms above are checked from the `v0.1.0` tag by `TestInstallBuildsFromTag`
(`HERDR_PANEL_BUILD_TAG=v0.1.0 make integration`): it installs the scratch
plugins in `integration/testdata/plugins/` from GitHub into an isolated
herdr, and each must report `herdr-panel v0.1.0` and open its panel. In a
Go plugin, `go mod init` writes the local toolchain's version; set the
`go` line no higher than the version your users have.

## Popups: what herdr does

- **One at a time.** Opening a popup while one is open fails with
  `ui_busy`. `Client.Open` returns an error wrapping `herdr.ErrBusy` and,
  with `OpenOptions.BusyNotice` set, shows a notification saying why
  nothing opened.
- **No pane id.** `plugin.pane.open` answers a popup with a bare `ok`. A
  popup is not in `pane list`, cannot be read or closed by id, and ends
  when its process exits. `Client.ClosePopup` (socket `popup.close`) closes
  whichever popup is open. The other placements (overlay, split, tab,
  zoomed) answer with the pane, which `pane read` and `plugin pane close`
  accept.
- **Closing** sends SIGHUP, then SIGTERM, then SIGKILL, 250 ms apart, to the
  popup's whole session. The panel ends on the first signal (about 2 ms in
  `TestCloseCadence`), killing its running commands' process groups, and
  discards unsaved edits without asking. Only a process started in a new
  session (`setsid`) outlives a popup.
- **Input**: mouse reports are SGR, relative to the popup's content. Keys
  follow what the program asks for. Bubble Tea v2 pushes the kitty keyboard
  protocol and herdr answers with flags 1 (disambiguation), so Esc, alt,
  ctrl, shift+tab and bracketed paste all decode correctly (slice S).
- **The frame's title.** herdr draws a border around a popup. For a
  plugin's popup the border carries the manifest's `[[panes]]` `title`; for a
  keybinding popup, the word `popup`; a split or tab has none. So in a
  plugin's popup the panel leaves out its own title row, and the unsaved
  count moves to the footer. herdr does not say a pane's placement: a popup
  is told apart by having no `HERDR_PANE_ID`, which every other plugin pane
  has (`TestFramedTitle`). Give the pane a `title` that names the panel.
- **The smallest content area** is 4×2; below 24×6 a panel shows a one-line
  "make me bigger" message.

## Context

`herdr.FromEnv` reads where a panel runs:

| Kind | What it has |
|---|---|
| `PluginPopup` | `HERDR_PLUGIN_CONTEXT_JSON` (workspace, tab, focused pane, their cwd), the plugin's id, root, config and state directories, `HERDR_SOCKET_PATH`, `HERDR_BIN_PATH` |
| `KeybindingPopup` | `HERDR_ACTIVE_WORKSPACE_ID`, `_TAB_ID`, `_PANE_ID` and `_PANE_CWD` only |
| `Outside` | nothing; only `{ctx.cwd}` and `{ctx.spec_dir}` have values |

The state directory falls back to `$XDG_STATE_HOME/herdr-panel` when herdr
gives none. Logs and crash logs go there.

## Notifications

`Client.Notify` asks herdr to show a notice. herdr may decline. That is not
an error: `Shown.Reason` says why (`disabled`, `rate_limited`,
`no_foreground_client`, `busy`). A panel that crashes inside herdr also
sends one, naming its crash log.

## Events

Live lists poll their source (`every_s`). herdr's `events.subscribe`
replays about ten seconds of backlog with no cursor, so a panel would act
on stale events. It is not used until herdr adds a cursor.

## Testing against herdr

`make integration` (module `integration/`) starts a real herdr, isolated:

- every `HERDR_*` variable is removed;
- every XDG directory is under a fresh temporary root, with `onboarding =
  false` in its own `config.toml`;
- the server is stopped only after `herdr status` names that root.

It runs a herdr client in a pseudo-terminal and reads its screen through
`x/vt`. It then:

1. opens a popup;
2. clicks a toggle and saves with `ctrl+s`;
3. checks the file on disk;
4. closes the popup with `popup.close` and checks that no temporary file is
   left;
5. opens the same panel as an overlay, and checks `herdr pane read`
   against the screen until they agree (a frame can be caught mid-repaint).

The suite skips when `herdr` is not on `PATH`.

`TestAutoTitleSettingsPanel` checks a real consumer the same way, when
`HERDR_PANEL_AUTOTITLE_DIR` names a herdr-auto-title checkout with `make
panel` done. It links the plugin, opens the panel through the plugin's own
action, saves a setting, waits for the apply (a plugin restart), reads the
workspace list, and checks that the restarted plugin outlives the popup. It
also redirects `HOME`: a plugin that falls back to `~/.config` would
otherwise find, and edit, the real user's files. A digest of those files,
taken before and after, proves they were not touched.

`TestFileViewerSettingsPanel` does the same for a plugin written by someone
else, herdr-file-viewer, with a settings panel added (`examples/file-viewer.toml`,
a `tomlfile` store), when `HERDR_PANEL_FILEVIEWER_DIR` names that checkout
with `bin/herdr-panel` built. It asks herdr for the plugin's config directory,
seeds the viewer's documented all-commented config, saves a setting, and
checks the file byte for byte.

`TestPluginManager` drives herdr-plugin-manager when
`HERDR_PANEL_PLUGINMANAGER_DIR` names its checkout with `make build` done.
It links the manager and a scratch plugin (`integration/testdata/plugins/popup-target`),
whose action the isolated config binds to a key. Then it clicks a plugin and
waits for the keys that follow the selection. It disables and enables the
plugin, checking herdr's own state each time, and checks that the manager
refuses to disable itself. Then it presses Run on the scratch plugin's popup
pane. The manager closes, and the scratch popup opens:
`after = "close_then_run"`, end to end. With a plain action instead, herdr
refuses the second popup, and the test fails. Last, it uninstalls the
scratch plugin: herdr stops listing it, and its linked folder is still
there. The manager refuses to uninstall itself.
