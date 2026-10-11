# Security

## A spec is code

A spec runs commands: `run`, `source`, `apply` and `test` start programs with
your user's rights, and a store path names a file the panel will write. So a
spec is code. Treat it like a script:

- Run specs you wrote, or that ship inside a plugin you chose to install.
- Do not point herdr-panel at a spec from a download, a shared directory or
  a repository you have not read.
- `herdr-panel validate` checks a spec's shape and references. It does not
  judge whether its commands are safe.

The spec file, the programs it names and the files it writes deserve the
same permissions as the plugin itself.

## What herdr-panel guarantees

- **No shell.** Every command is an argument vector, started directly.
  Placeholders expand inside one element each, so a value holding spaces,
  quotes, `;` or `$(...)` stays one argument, exactly as written. The program
  (the first element) can never come from `{row.*}` or `{field.*}`.
- **Secrets stay put.** A `secret` field is never drawn, logged, written to
  test output, put on a command line or put in an environment variable. A
  spec that writes `{field.<secret>}` anywhere is rejected. A `test` command
  reads the secret from the store itself.
- **Bounded commands.** Each command has a timeout (10 s by default), an
  output cap of 1 MiB and its own process group. On timeout, or when the
  panel closes, the whole group is killed.
- **Careful writes.** A store is written to a temporary file in the same
  directory, synced and renamed over the original. A symlink is followed and
  its target written. The file keeps its mode, and a new file is created
  `0600`. If the file changed on disk since the panel read it, the panel
  does not overwrite it: it asks.

## What it cannot guarantee: argument injection

"No shell" stops a value from becoming a second command. It does not stop a
value from being read as an **option** by the program it is passed to. If a
row's `id` is `--delete-everything`, then

```toml
run = ["my-tool", "remove", "{row.id}"]
```

hands `my-tool` an option, not a name. Row values come from your `source`
command, and field values come from whoever edits the panel, so they are
input.

Defences, strongest first:

1. **End the options with `--`** before any placeholder, when the program
   supports it:

   ```toml
   run = ["my-tool", "remove", "--", "{row.id}"]
   ```

2. **Join the value to its option** in one element, so it can only be that
   option's value: `"--id={row.id}"`.
3. **Read the value from the environment** in your own program, as
   `PANEL_ROW_ID` or `PANEL_FIELD_<ID>`, instead of from an argument.
4. **Validate in the program** that receives it. Your program knows what an
   id looks like; herdr-panel does not.

## A shell brings injection back

If a command runs a shell, the guarantee above is gone:

```toml
run = ["sh", "-c", "my-tool remove {row.id}"]   # don't
```

Here `{row.id}` becomes shell syntax, and a row holding `x; rm -rf ~` runs
`rm`. If you need a shell, pass values as positional arguments, which the
shell does not re-parse:

```toml
run = ["sh", "-c", 'my-tool remove -- "$1"', "sh", "{row.id}"]
```

The same applies to any program that evaluates its arguments: `bash -c`,
`python -c`, `ssh host cmd`, `xargs`, `find -exec`, `git -c`.

## The environment

Commands inherit the panel's environment, including `HERDR_SOCKET_PATH`, so
an action can call `herdr`. Anything secret in that environment (API keys
exported in your shell) reaches every command a spec runs. This is the same
as running the commands yourself.

## Reporting a vulnerability

See [SECURITY.md](../SECURITY.md).
