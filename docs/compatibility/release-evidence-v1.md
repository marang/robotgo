# Release Evidence v1

Schema version: **1**

RobotGo release evidence records the exact source and test result behind a
published release instead of relying on an unversioned statement that CI was
green. The workflow lives in `.github/workflows/release-evidence.yml`.

## Evidence matrix

Every published release and manually dispatched release-evidence run executes
the default suite in six independent cells:

| Platform | Native CGO | Pure Go |
|---|---:|---:|
| Linux | yes | yes |
| macOS | yes | yes |
| Windows | yes | yes |

Each cell emits `evidence.json` and `test.log`. The JSON document records:

- the exact Git commit, Git tree, and full ref;
- GitHub Actions run ID, attempt, and matrix identity;
- Go version, `GOOS`, `GOARCH`, CGO state, and active implementation;
- the passed command and SHA-256 digest of its complete test log;
- the sanitized Runtime Diagnostics v1 report.

The bundle also contains `required-checks.json`. It records the successful
release status set for the exact commit: the public API compatibility gate,
X11 default suite, lint, vet, race, ASan/LeakSanitizer, OCR, all three default
and Pure-Go platform legs, Wayland, targeted X11 evidence, the
release-evidence validator, all six hosted
Sway cells (native input, capture, window, single-/multi-output geometry, and
portal availability), and the four release-only GNOME/KDE multi-output
RemoteDesktop/ScreenCast cells. It also invokes the two release-only GNOME/KDE
multi-output bounds cells and the isolated hosted Hyprland active-window
geometry check. Missing, pending, skipped, neutral, cancelled, timed-out,
stale, or failed required checks abort snapshot publication. The current
workflow manifest has 29 entries.

Twenty-three manifest entries are intended to mirror the `main`
branch-protection contexts. The four portal and two display-bounds entries are
intentionally release-only because their
disposable nested desktop guests do not boot for ordinary pull requests. Add,
rename, or remove a branch-protection context in both contracts in the same
change; change a portal or display-bounds check together with its reusable
workflow and release-manifest entry.

The generator rejects a matrix whose operating system or CGO state disagrees
with the running binary. The verifier rejects unknown fields, trailing JSON,
unsupported matrix labels, path traversal, non-regular files, schema drift, and
test-log digest mismatches.

## Published assets

After all six cells, the protected-check contract, and the validation job pass,
the workflow verifies every cell again and creates:

```text
robotgo-release-evidence-<tag>-<commit>.tar.gz
robotgo-release-evidence-<tag>-<commit>.tar.gz.sha256
```

For a GitHub `release.published` event these files are attached to the existing
release. The write-authorized publish job does not check out or execute
repository code; it only verifies the already packaged SHA-256 and uploads the
two assets. Manual runs retain the bundle as a GitHub Actions artifact for 90
days and do not modify a release.

The current post-API-freeze candidate contract passes in
[`Release Evidence` run 30284816440](https://github.com/marang/robotgo/actions/runs/30284816440)
on merged `main` commit
`912722cd480bd542419bd16e7267bbf22201e1ff`. Its bundle contains all six
native/Pure-Go snapshots and exactly 29 successful checks, including
`api-compat`, all GNOME/KDE multi-output release lanes, Sway, and Hyprland. The
bundle SHA-256, exact commit/tree/ref, six evidence documents, and 29-check
manifest were independently reverified after download; the temporary
verification directory was then removed.

The preceding pre-API-freeze 28-check exact-candidate contract passes in
[`Release Evidence` run 30272753885](https://github.com/marang/robotgo/actions/runs/30272753885)
on merged `main` commit
`a641236b1b8f8bd80d4fbffc526a10aa5862b001`. The packaged schema-v1
`required-checks.json` contains both successful GitHub Actions GNOME/KDE
multi-output bounds rows. This manual candidate evidence does not alter the
immutable assets of an already published release.

The latest published bundle is attached to
[`v1.0.0-beta.2`](https://github.com/marang/robotgo/releases/tag/v1.0.0-beta.2):

- [`robotgo-release-evidence-v1.0.0-beta.2-f3530594f30b.tar.gz`](https://github.com/marang/robotgo/releases/download/v1.0.0-beta.2/robotgo-release-evidence-v1.0.0-beta.2-f3530594f30b.tar.gz)
- [`robotgo-release-evidence-v1.0.0-beta.2-f3530594f30b.tar.gz.sha256`](https://github.com/marang/robotgo/releases/download/v1.0.0-beta.2/robotgo-release-evidence-v1.0.0-beta.2-f3530594f30b.tar.gz.sha256)

It records exact source commit
`f3530594f30baa29fd61828c77b2b5c0140f3d15`; all six snapshot cells and all 25
protected checks passed, including the four real GNOME/KDE multi-output portal
checks. The archive digest is
`5d21e7da7b2f8d8745dfa9a48b14e301ac7d97763d453fbe789f6355fe01efe0`.

The first published bundle remains attached to
[`v1.0.0-beta.1`](https://github.com/marang/robotgo/releases/tag/v1.0.0-beta.1):

- [`robotgo-release-evidence-v1.0.0-beta.1-1bab5e173f6b.tar.gz`](https://github.com/marang/robotgo/releases/download/v1.0.0-beta.1/robotgo-release-evidence-v1.0.0-beta.1-1bab5e173f6b.tar.gz)
- [`robotgo-release-evidence-v1.0.0-beta.1-1bab5e173f6b.tar.gz.sha256`](https://github.com/marang/robotgo/releases/download/v1.0.0-beta.1/robotgo-release-evidence-v1.0.0-beta.1-1bab5e173f6b.tar.gz.sha256)

It records exact source commit
`1bab5e173f6b96f61d349473b348f839291b9a89`; all six matrix cells and all 15
protected checks passed. The archive digest is
`93c45caae406d33fefb0fbbd60ec1cb9d347027b155efcde376c9685161d0207`.

## Verification

After extracting a bundle at the repository root, verify each matrix cell with:

```bash
CGO_ENABLED=0 go run ./internal/cmd/releaseevidence verify \
  -evidence release-evidence-linux-native/evidence.json \
  -expected-matrix linux-native
```

Repeat for `linux-purego`, `macos-native`, `macos-purego`,
`windows-native`, and `windows-purego`. Verify the outer archive before
extracting it:

```bash
sha256sum -c robotgo-release-evidence-*.tar.gz.sha256
```

This six-cell snapshot matrix does not replace real-compositor evidence. The
release workflow directly invokes GNOME and KDE multi-output
RemoteDesktop/ScreenCast checks in disposable hosted guests and records their
successful exact-SHA check runs in the bundle. It also directly invokes the
consent-free GNOME/KDE multi-output bounds checks in native-CGO and Pure-Go
builds. The separate hosted Sway/wlroots native, single-/multi-output, and
explicit portal-availability rows are also required by the release gate for
the exact release commit. The hosted Hyprland/`vkms` window cell is required as
well. These compositor jobs remain
linked to their own sanitized compositor-evidence artifacts. Published beta.2
remains immutable historical evidence with its 25-check manifest.
