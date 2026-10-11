# Contributing to herdr-panel

## License

herdr-panel is licensed under the [MIT License](./LICENSE). By submitting
a contribution, you agree that it is licensed under the same terms
(inbound = outbound): there is no separate contributor agreement.

## Third-party material

Name the source and license of anything you did not write. Dependencies
must carry a permissive license — MIT, BSD, Apache-2.0, ISC and the like —
so herdr-panel stays MIT for everyone who embeds it. Copyleft dependencies
(GPL, AGPL, LGPL for linked Go code) are not accepted in the library or
the host binary.

## Working on it

Go 1.26 or newer.

```sh
make check        # format, vet, lint for Linux and macOS, tests with -race, licence table
make integration  # against a real herdr, isolated from yours (skips without herdr)
```

- **Every safety rule has a test that fails without it.** When you fix a
  bug, add the test first and watch it fail.
- **Never run anything against your own herdr session.** The integration
  suite starts its own herdr under a temporary root, with every `HERDR_*`
  variable removed; copy that pattern for anything new.
- **Docs change with the code.** A spec key, a CLI flag or an exported
  symbol is documented in the same change (`docs/`, `README.md`,
  `CHANGELOG.md`). A breaking change goes under **Breaking** in the
  CHANGELOG.
- `herdr-panel observe` shows you exactly what a spec draws and which ids
  you can click, without a terminal.
