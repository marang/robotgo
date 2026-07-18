## Summary

<!-- Describe the practical behavior or user-facing result. -->

## Why

<!-- Link the issue when one exists and explain why this change belongs here. -->

Related issue:

## Changes

-

## Platform and backend impact

<!-- Note affected OSes, display servers, build tags, fallbacks, and explicit
unsupported behavior. Use "none" only after checking. -->

## Risk and cleanup

<!-- Cover compatibility, resource ownership, sensitive-data handling, and
rollback or fallback behavior. -->

## Validation

<!-- Keep only commands and real-runtime evidence that were actually run. -->

- [ ] `go test ./...`
- [ ] Relevant tagged suites from `TEST.md`
- [ ] `go vet ./...`
- [ ] Linter or static analysis
- [ ] Cross-platform or non-CGO compilation where affected
- [ ] No sensitive test or development artifacts remain

## Review readiness

- [ ] Targets `main`
- [ ] Commits and PR text are in English
- [ ] Tests cover normal and failure/fallback behavior
- [ ] User-facing documentation is updated where required
- [ ] Build tags and platform boundaries were reviewed
- [ ] Resource and file-descriptor cleanup was reviewed
- [ ] CI and all reviewer comments, including Codex when configured, will be
      checked before merge
