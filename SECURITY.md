# Security

## The trust model

A spec runs commands: its sources, actions and apply steps are argument
vectors that herdr-panel executes. **A spec is code.** Treat a spec from
someone else the way you would treat their shell script. herdr-panel runs
those commands without a shell, with a timeout and an output cap, and expands
placeholders inside single arguments so values never split or inject — but a
command that hands a value to `sh -c` brings injection back. See
[`docs/security.md`](./docs/security.md).

## Reporting a vulnerability

Please report it privately through GitHub's "Report a vulnerability" button
on this repository's Security tab, not in a public issue. Include the version
(`herdr-panel version`), a spec that shows the problem, and what you expected.
