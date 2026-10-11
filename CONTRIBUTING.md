# Contributing to herdr-panel

## License

herdr-panel is licensed under the **Functional Source License, Version
1.1, MIT Future License** (SPDX `FSL-1.1-MIT`) — full text at
[`LICENSE`](./LICENSE). It is **source-available, not open source**: the
license's grant is conditioned on using the Software for a Permitted
Purpose rather than a Competing Use. In practice that means internal and
commercial use are fine — embedding herdr-panel in your own plugin or tool
included — and only offering herdr-panel itself, or something substantially
similar, to others as a commercial product or service is barred. Each
version converts to the plain MIT license, irrevocably, on the second
anniversary of its release.

## The CLA is a precondition of merge

**Agreeing to [`CLA.md`](./CLA.md) is required before any Contribution
can be merged.** Read it — the operative clause grants the project owner
the right to relicense your Contribution, including under proprietary
terms, while you keep your own copyright. This exists so the project can
change license later (or not) without needing to track down and get
sign-off from every past contributor.

To signify agreement, a pull request needs **both** of the following. The
maintainer will not merge without both:

1. **The exact line, in the pull request description:**

   ```
   I have read and agree to the CLA in CLA.md.
   ```

2. **A `Signed-off-by: Name <email>` trailer on every commit in the PR**
   (`git commit -s` adds this automatically). The trailer is part of the
   commit message, so it travels with the history into any clone or
   mirror, which a PR description does not.

   This trailer's form is borrowed from the DCO convention, but its
   meaning here is defined by `CLA.md`: signing it means you agree to the
   broader relicensing grant there.

## Third-party material

Name the source and license of anything you did not write (clause 7 of
the CLA). Dependencies must carry a license that permits inclusion in a
source-available project — MIT, BSD, Apache-2.0, ISC and the like.
Copyleft dependencies (GPL, AGPL, LGPL for linked Go code) are not
accepted in the library or the host binary.
