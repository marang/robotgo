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
protected status set for the exact commit: CircleCI, lint, vet, race,
ASan/LeakSanitizer, OCR, all three default and Pure-Go platform legs, Wayland,
X11 evidence, and the release-evidence validator. Missing, pending, cancelled,
or failed required checks abort snapshot publication.

The required-check manifest in the workflow and the `main` branch-protection
contexts are one contract. Add, rename, or remove a stable check in both places
in the same change.

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

This hosted-runner evidence does not replace the protected real GNOME, KDE, and
wlroots RemoteDesktop/ScreenCast matrix. Those rows remain pending until the
matching runners are provisioned.
