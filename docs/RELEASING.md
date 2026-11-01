# Releasing Moontop

Releases are driven by `CHANGELOG.md`. Nothing is tagged by hand.

## Cutting a release

1. On `main`, rename `## [Unreleased]` in `CHANGELOG.md` to the new version and
   date, and start a fresh `## [Unreleased]` above it:

   ```markdown
   ## [Unreleased]

   ## [0.2.0] - 2026-11-14

   ### Added
   - …
   ```

   Update the link references at the bottom
   (`[0.2.0]: https://github.com/justinh-rahb/moontop/releases/tag/v0.2.0`).

2. Push (or merge a PR) to `main`. `.github/workflows/release.yml` then:
   - reads the first `## [x.y.z]` heading;
   - stops if tag `vx.y.z` already exists;
   - runs the tests, creates and pushes the tag;
   - runs GoReleaser with that section of the changelog as the release notes.

   It can also be started by hand from the Actions tab (*Release → Run workflow*),
   e.g. to retry after fixing a secret.

A version with a suffix (`0.2.0-rc.1`) is published as a pre-release.

## What a release publishes

| Channel | What | Where |
| --- | --- | --- |
| GitHub release | `tar.gz` (Linux, macOS, FreeBSD) and `zip` (Windows) for amd64 / arm64 (and armv7 on Linux), `checksums.txt` | Releases page |
| install.sh | downloads the right archive and checks its checksum | the website: `justinh-rahb.github.io/moontop/install.sh` (a copy of the top-level file, published from `docs/`) |
| `go install` | builds from source at the tag | `github.com/justinh-rahb/moontop/cmd/moontop@latest` |

Binaries are built with `CGO_ENABLED=0 -trimpath` and stamped with
`-X main.version / commit / date`, which `moontop version` prints.

## One-time setup

1. Settings → Actions → General → Workflow permissions: allow read and write
   (the release job pushes the tag and creates the release with `GITHUB_TOKEN`).
2. Settings → Code security: enable **Private vulnerability reporting**
   (`SECURITY.md` and the code of conduct point to it).

Homebrew, Scoop and Linux packages (deb / rpm / apk) are left out for now;
GoReleaser can add them later (`homebrew_casks`, `scoops`, `nfpms`).

## macOS note

The binaries aren't signed or notarized. A download made by the install
script (curl) isn't quarantined, so it runs. One downloaded in a browser is:
run `xattr -d com.apple.quarantine ./moontop` once.

## Checking the config locally

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=publish   # builds everything into dist/
```

CI runs both on every push and PR.
