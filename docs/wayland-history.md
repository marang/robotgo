# Wayland Support History (Evidence Timeline)

Last updated: 2026-03-02

This document records publicly auditable Git history only.
It is an engineering evidence summary, not legal advice.

## Scope

- Repositories compared:
  - `go-vgo/robotgo`
  - `vcaesar/robotgo-pro`
  - `marang/robotgo` (this fork)
- Goal:
  - Identify first Wayland-related "start" commits.
  - Identify first clearly useful Wayland functionality commits.

## Branches Checked

- `go-vgo/robotgo` remote branches:
  - `origin/bitmap-pr`, `origin/dep-pr`, `origin/dev`, `origin/master`, `origin/nobitmap`, `origin/op-pr`, `origin/robot`, `origin/robotb`, `origin/test-ci`, `origin/test-pr`, `origin/test1-pr`, `origin/utf`, `origin/win-pr`
- `vcaesar/robotgo-pro` remote branches:
  - `origin/dev`, `origin/main1`
- `marang/robotgo`:
  - Local repo history (`--all`), with `main` as the active production branch.

## Timeline Table

| Repo | Milestone | Date | Commit | Message | Notes |
|---|---|---|---|---|---|
| `go-vgo/robotgo` | First Wayland-related start (README mention) | 2022-09-14 | [`c3cda41`](https://github.com/go-vgo/robotgo/commit/c3cda41c2d55f17ccfd3d6db0ac52f07e3d7b182) | `Update: update godoc and fixed typo` | README-level mention only. |
| `go-vgo/robotgo` | First Wayland support code | N/A | N/A | N/A | No Wayland code/path commit found in checked branches. |
| `go-vgo/robotgo` | First useful Wayland functionality | N/A | N/A | N/A | No auditable public Wayland implementation found in checked branches. |
| `vcaesar/robotgo-pro` | First Wayland-related start (README mention) | 2025-11-28 | [`36c5d45`](https://github.com/vcaesar/robotgo-pro/commit/36c5d452d54b1ab1467db9b2783bc37cab0974cd) | `Update: update readme.md` | Public source shows README mention. |
| `vcaesar/robotgo-pro` | First Wayland support code (public source) | N/A | N/A | N/A | No Wayland source commit/path found in checked branches. |
| `vcaesar/robotgo-pro` | First useful Wayland functionality (public source) | N/A | N/A | N/A | Public repo ships binaries; no auditable Wayland source trail in checked branches. |
| `marang/robotgo` | First Wayland code added | 2025-04-12 | [`3c0f1bf`](https://github.com/marang/robotgo/commit/3c0f1bf3524fc2dd0199706fcaba5ab6151af75a) | `add get_bounds_wayland` | First explicit Wayland code path in this fork history. |
| `marang/robotgo` | First useful Wayland functionality | 2025-08-31 | [`17fc02d`](https://github.com/marang/robotgo/commit/17fc02d7b35b51110867c001f4b5aed73fa55ca0) | `feat: use wlr-screencopy for wayland capture` | First clearly useful runtime Wayland feature. |
| `marang/robotgo` | Wayland usefulness expanded | 2025-09-04 | [`63d9115`](https://github.com/marang/robotgo/commit/63d9115e3c4987e9561978465ee8a9c8aa9f24c1) | `wayland: native screencopy + portal fallback; input improvements; full-screen example; stability fixesremove unused fn` | Broader practical Wayland feature set. |

## Date Difference (Useful Functionality)

- This fork useful Wayland functionality: 2025-08-31
- `robotgo-pro` first public Wayland-related start: 2025-11-28
- Difference: 89 days (about 3 months)

## Method Summary

- Used branch-wide history scans:
  - commit message grep for `wayland`
  - path scans for `*wayland*`
  - README string history checks (`-S "Wayland"`)
- For usefulness, used first commits that clearly add runtime Wayland functionality (not only mention/docs).
