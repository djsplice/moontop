# Contributing to Moontop

Thanks for helping out. Bug reports, printer quirks and pull requests are all welcome.

## Reporting bugs

Open an issue with the bug template. The most useful things to include are the
output of `moontop version`, your terminal and OS, your printer and firmware
(Klipper / Moonraker versions, AFC if any), and a screenshot if it's visual.

## Pull requests

1. Fork and branch from `main`.
2. Make your change. **Every change comes with tests**, and with docs updates
   (README, `docs/`, `CHANGELOG.md` under `[Unreleased]`) when it changes what a
   user sees or does.
3. Make sure these pass locally:

   ```sh
   gofmt -l .          # prints nothing
   go vet ./...
   go test -race ./...
   ```

4. Open the PR and fill in the template.

[AGENTS.md](../AGENTS.md) describes how the code is put together and the rules
it follows (keymap registry, confirmed state, non-blocking sends, the fake
Moonraker used by tests). Please read it before a larger change. It applies to
people as much as to coding agents.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By
taking part you agree to uphold it.
