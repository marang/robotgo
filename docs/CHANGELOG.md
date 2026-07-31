# CHANGELOG

## Unreleased

- Expanded the deny-by-default agent and local MCP action contract with bounded
  scroll, drag, canonical keyboard chord, and identity-revalidated window
  activation operations. Extended actions require immutable event, distance,
  duration, key/modifier/window, rate, and session-lifetime limits; drag,
  shortcut, and activation also require confirmation. A session-owned pressed
  input ledger releases keys and buttons across success, failure,
  cancellation, timeout, disconnect, and close, while Wayland activation stays
  explicitly unavailable instead of inheriting a broader window capability.
  Chords are now injected only through process-targeted backends, activation
  validates and mutates the same resolved native handle, and the catalog
  structurally reports supported scroll axes. Ambiguous input-down failures
  trigger an immediate release and remain session-owned if cleanup fails.
  Explicit process identities use a separate title-query contract and no
  longer inherit the legacy handle-mode default, while existing `GetTitleE`
  behavior remains compatible. The complete agent mutation driver, including
  move, click, text, drag, scroll, and chord primitives, bypasses global mouse
  and key post-event delays.
  Errors returned after any desktop mutation dispatch are `unverified`;
  failures proven to occur during preflight remain `failed`. Failed release
  taints the session and retains its exclusive owner until cleanup succeeds.
  Mutation capabilities now provide a stable unavailable, unsupported, or
  permission-denied code without requiring reason-string parsing.
  Native Windows process-targeted chords now preserve down/up semantics for
  extended keys, release the main key before modifiers, and propagate missing
  targets plus `PostMessageW`/`SendInput` failures instead of reporting a
  successful injection. Failed legacy tap transactions make a best-effort
  reverse-order release pass over only the successfully dispatched prefix, so
  newly visible partial failures neither leave modifier state behind nor emit
  orphan key-up events. Persistent native Windows holds and taps now share a
  physical-key reference ledger: overlapping chords cannot release one
  another's modifiers, failed tap or hold releases remain retryable, and an
  attempted tap whose physical main key is already held is rejected rather
  than reporting an unobservable transition. Native holds still record the
  exact applied prefix, reject foreign or duplicate releases, and report a
  typed no-state-acquired error after a pre-dispatch failure so agent cleanup
  never touches foreign input.
- Split KDE hosted-session readiness into bounded prerequisite, Plasma Shell,
  portal frontend, portal backend, and stability phases. The complete session
  must now pass three consecutive probes, later phases continuously revalidate
  their prerequisites, allow naturally managed shell/portal bounces to settle,
  and expose only explicitly allowlisted failure stages outside the disposable
  guest. A terminal Plasma Shell failure gets one bounded failed/start-limit
  reset and at most one bounded, non-blocking user-unit restart followed by an
  explicit bounded readiness wait that continuously revalidates the base
  session; reset, queue, and start failures remain distinguishable. Recovery
  and ready claims are serialized with in-lock contract revalidation without
  unbounded phase resets. Volatile shared outcome markers live outside the
  per-user runtime directory so its disappearance remains diagnosable, and a
  second failure remains terminal. Failed recovery now exposes only an
  allowlisted systemd result category and bounded numeric exit or signal
  status; raw unit output remains private, and recovery observers remain
  bounded beyond those result probes so every waiter consumes the same precise
  outcome. KDE receives its own bounded timeout
  hierarchy while GNOME
  retains its shorter guard and maps its legacy `unknown` terminal marker to
  `session-unstable`. This prevents slow early startup from consuming the
  shell's budget or exposing private diagnostics.
- Restore the standalone `plasma-desktop-data` runtime dependency and validate
  its default desktop-shell metadata in the minimized KDE image. The broad
  `plasma-desktop` package remains forbidden, but a failed Plasma Shell restart
  can no longer be caused by omitting its required `org.kde.plasma.desktop`
  shell package.
- Made hosted GNOME/KDE image-install stalls fail earlier and diagnostically:
  all three portal workflows now share one tested outer timeout wrapper, guest
  installation has a shorter phase deadline, and failures expose only a
  bounded pre-registration build-log tail before mandatory transient cleanup.
  The resulting evidence identified the broad `ubuntu-desktop-minimal` and
  `plasma-desktop` dependency closures, so the images now install and verify
  only the explicit Ubuntu GNOME session or Plasma Workspace/Wayland contract
  required by their real-desktop tests. The GNOME guest preserves the
  originally proven Ubuntu Shell mode without restoring the broad desktop
  metapackage and sets the fixed XKB source used by its virtual consent
  keyboard. The 113 MB kernel-extra package remains
  exact-version/SHA-256 pinned but uses an Ubuntu-listed HTTPS mirror with an
  immutable Launchpad fallback rather than the throttled snapshot; unused APT
  translation, desktop-metadata, and command-not-found indexes are skipped.
  GNOME consent input now emits readiness only after non-modal negotiation has
  completed, uses a two-way start gate bound to the exact random `Start` request
  path, ignores lingering earlier request objects, and no longer races an
  on-demand portal backend with a fixed delay. Parentless dialogs receive an
  independent neutral QMP focus click before their documented mnemonics.
- Made the seven-day stable qualification window a fail-closed release
  preflight gate: `v1.0.0` cannot pass before
  `2026-08-05T10:13:46Z`; the gate validates GitHub's authoritative API time
  instead of trusting the operator clock, and deterministic boundary/error
  tests prevent remote clock lookup or parsing failures from silently
  permitting publication.
- Classified permission-granted macOS runtime evidence as an externally blocked,
  non-release-blocking promotion under LAB-69. Native and Pure-Go permission
  scopes remain implemented/evidence-pending until an isolated project-owned,
  maintainer/community, donated/sponsored, or officially capable GitHub-hosted
  environment becomes available.
- Hardened stable-line publication against local tags inherited from another
  remote: origin preflight now rejects an exact local tag collision, and the
  operator procedure aborts on every error before it can push a tag.

## RobotGo v1.0.0-rc.1 — 2026-07-29

This first stable-line release candidate freezes the reviewed public Go API,
makes every supported platform claim traceable to blocking evidence, and
hardens release publication against local tags inherited from the separate
upstream repository.

Highlights since `v1.0.0-beta.2`:

- Reduced noisy and wasteful Actions runs by limiting feature-branch
  validation to pull-request events while retaining `main` and release-tag
  pushes, separating manual Sway/Hyprland evidence from `main`-push
  concurrency, and adding a repository-local CI preflight that detects
  formatting, workflow, baseline, support-contract, test, vet, and lint
  failures before push.
- Added a checked-in, human-reviewable public Go API freeze across every
  importable library package, Linux native/Wayland/portal/PipeWire,
  native macOS/Windows, and Linux/Windows/macOS Pure-Go variants; package-name,
  exported, or package-discovery drift now fails CI and the expanded 29-check
  exact-release manifest. Native Linux X11 now also exports the existing
  `GetXidByPid` compatibility name consistently with the Wayland build.
- Added a checked, machine-readable Runtime Compatibility Matrix that maps
  every supported platform scope to exact release checks and rejects mixed
  supported/evidence-pending states. Native and Pure-Go macOS
  permission-granted capture/input/window behavior is now truthfully classified
  as implemented/evidence-pending, with self-owned remote evidence tracked in
  LAB-69; consent-free macOS build/API/display/permission contracts remain
  supported. The contract also distinguishes Pure-Go one-shot Wayland
  Screenshot capture from persistent ScreenCast frames, which require Linux
  CGO plus the `pipewire` build tag.
- Selected `v1.0.0-rc.1` then `v1.0.0` as the independent module's stable
  release line after verifying the authoritative origin contains only the fork
  beta tags; documented safe handling of local upstream-derived tags and
  defined API/platform/evidence qualification gates.
- Added protected, sanitizer-backed Hyprland active-window geometry evidence
  on one isolated GitHub-hosted `vkms` display, with no physical input/GPU
  devices and mandatory private-runtime cleanup.
- Promoted the Hyprland window cell into the exact-candidate release gate,
  expanding the current workflow manifest from 25 to 26 checks while retaining
  the published beta.2 evidence unchanged.
- Added privacy-safe hosted GNOME/KDE multi-output bounds evidence for native
  CGO and Pure-Go Wayland builds, then promoted both lanes into the exact
  release gate, expanding the current candidate manifest from 26 to 28 checks.
- Added an authoritative origin release preflight that requires the selected
  commit to equal `origin/main`, checks both Git and GitHub for an existing RC
  or stable tag/release, rejects non-fork remotes, ignores local
  upstream-derived tags, and permits publishing only the explicit tag ref.
- Stabilized hosted GNOME/KDE guest provisioning under transient snapshot
  throttling with bounded APT/Curl retries and network timeouts, serialized
  per-workflow desktop image builds, and a larger but still bounded
  execution/cleanup window.

See the [complete release notes](releases/v1.0.0-rc.1.md) for installation,
migration, supported and evidence-pending scopes, qualification rules, and
evidence verification.

## RobotGo v1.0.0-beta.2 — 2026-07-27

This second pre-release adds a policy-gated agent automation layer and local
MCP adapter, completes scoped Wayland input/capture evidence on GNOME, KDE, and
wlroots, expands trustworthy Wayland window contracts, and hardens cleanup and
release validation across platforms.

Highlights since `v1.0.0-beta.1`:

- Added `agent.Session` with typed operations, dry-run/execute separation,
  bounded policy and quotas, audit events, in-memory observations,
  observe-act-verify lineage, and privacy-safe visual find/wait conditions.
- Added the local stdio `robotgo-mcp` adapter with sanitized results,
  explicit observation release, and no network listener.
- Added protected real-compositor evidence for native Sway and real
  GNOME/KDE RemoteDesktop and ScreenCast portals, including multi-output
  absolute input and per-stream capture.
- Added truthful Wayland active-window geometry and identity APIs, bounded
  compositor helper lifecycle, and explicit unsupported behavior where the
  compositor cannot prove state.
- Hardened portal screenshot decoding, external-command teardown, clipboard,
  OCR, alerts, Windows clipboard/paste integration, pointer tests, and
  sensitive test-artifact cleanup.
- Replaced CircleCI with a protected GitHub Actions X11 default-suite gate.
- Expanded exact release evidence to 25 successful checks for the candidate
  SHA, including four release-only GNOME/KDE portal jobs, plus the six-cell
  Linux/macOS/Windows native/Pure-Go snapshot matrix.

This remains a beta release. Wayland behavior is compositor- and
permission-dependent, the agent session is intentionally process-global, and
the MCP adapter is a local transport rather than a remote authorization
service.

See the [complete release notes](releases/v1.0.0-beta.2.md) for installation,
upgrade guidance, compatibility limits, and evidence verification.

## RobotGo v1.0.0-beta.1 — 2026-07-19

This is the first published pre-release of the independent
`github.com/marang/robotgo` module. It establishes the fork's Wayland-first,
explicit-error, Pure-Go, diagnostics, and release-evidence contracts while
retaining the existing cross-platform automation API wherever behavior can be
implemented safely.

See the [complete release notes](releases/v1.0.0-beta.1.md) for highlights,
installation, compatibility limits, and evidence verification.

<!--### RobotGo-->
## RobotGo v0.100.0, MT. Baker; Enhancement bitmap and image, add arm support...

### Add

- [NEW] add more image function support
- [NEW] add ImgToBitmap(), ToImg(), FindEveryBitmap(), FindEveryColor(), Svae(), Read(), SaveJpeg() and other function support

- [NEW] add func ToImage: convert C.MMBitmapRef to standard image.Image
- [NEW] add ToImage examples code
- [NEW] add more image function and update go mod

- [NEW] add find every color function
- [NEW] add free and find all point function
- [NEW] update find bitmap and color

- [NEW] add byte to image function
- [NEW] add more image convert function

- [NEW] add mac os M1 support
- [NEW] add windows arm support

- [NEW] add more key toggle and press function

- [NEW] add ToRGBA() function support
- [NEW] add ImgToBitmap and RGBAToBitmap support
- [NEW] Update and move image function to img.go

- [NEW] add more img to bitmap examples code and Update file name

### Update

- [NEW] Update README.md and CHANGELOG.md
- [NEW] add macOS to .travis.yml
- [NEW] update go mod pkg

- [NEW] Update hook examples link to newest
- [NEW] update dockerfile and appveyor to go1.14.4

- [NEW] Update README.md, add more examples
- [NEW] move hook and event to gohook
- [NEW] move cbitmap and bitmap-bytes to bitmap dir

- [NEW] update some name
- [NEW] update dockerfile and appveyor.yml

- [NEW] update clipboard code
- [NEW] update hook code and more API

- [NEW] optimize code and update version
- [NEW] add paste string err return and optimize code
- [NEW] update go.yml and travis.yml to go1.15
- [NEW] Update Ubuntu apt-get to apt

- [NEW] update go version and key code
- [NEW] update test code and go mod
- [NEW] Update README.md and test code

- [NEW] update parameter name and version
- [NEW] update dockerfile and appveyor.yml
- [NEW] update error return and print

- [NEW] update ShowAlert optimize return code
- [NEW] add more test and update go mod

- [NEW] compatible with 32-bit platforms
- [NEW] add more bitmap examples

- [NEW] update point structure to public

- [NEW] add more examples
- [NEW] update examples and version
- [NEW] Update clipboard example code

- [NEW] Update README.md Section ####Other to windows (#348) …
- [NEW] Update png.h path
- [NEW] Update go mod
- [NEW] Update circle.yml and travis.yml

- [NEW] Remove unless example code and update circle.yml
- [NEW] Removed drop api example code and Update README.md

- [NEW] Update go mod and xx.yml
- [NEW] Update README.md and example

- [NEW] add more bitmap examples code
- [NEW] Update go mod and Update README.md
- [NEW] gofmt to 1.17 build tag
- [NEW] Update bitmap examples code

- [NEW] Update version and keycode
- [NEW] Update docs remove drop API


### Fixed

- [FIX] Update go mod and fixed #290
- [FIX] Update gohook to v0.30.2 fixed bug
- [FIX] Fixed Mouse buttons reversed type
- [FIX] Fixed returns "Invalid key code specified." if specified character is not v… … add keyCodeForCharFallBack

- [FIX] This fixes the disappearing backslash issue #351
- [FIX] Export ToUC function and update test code
- [FIX] Fixes #258: char* arrays in C not being copied correctly
- [FIX] Fixed Linux TypeStr() function double quote

- [FIX] update free bitmap fixed #333
- [FIX] update gops to v0.20.0 fixed bug and other mod pkg
- [FIX] update gohook fixed warning



## RobotGo v0.90.0, MT. Rainier

### Add

add gohook modern and concurrent API
add new gohook examples, thks for cauefcr

Support for multiple screens
add getMousePos() multiple screens support
add move smooth multiple screens support

add all platform system scale support
add get screen size test code

add screen and bitmap multiple screens support
add int32_t types support

update keycode type use uint16 with gohook, not type convert
add ToBitmapBytes func (#204)

gohook: sched_yield support for non-POSIX windows gcc

add gops test code support
add Process() function test code
add more gops test code

add more win32 function export

add get mouse color function

add uint32 to Chex function support

add key_Toggles() c function
add keyTap and keyToggle "...string" parameters support, Fixed #209

add robotgo simple test code

add Is64Bit() c and go function

add process FindPath() function

add keycode "delete" support and fixed "\\" error
add more keycode support, "up, down, left, right"...

export hook keycode and add godoc

use robotn fork xgb and update go mod

add hook example to robotgo examples

update gohook and tt mod file

add more and update test code

add drag smooth function support and examples

add ShowAlert() test support

update keypress rand sleep [reduce] and update code style, update c delay default value to 0

add mouse toggle return and add more test

add SetDelay function code and update other code

add scaled function code

add go opencv file

add readme.md file

add move mouse and move smooth relative code

add move mouse and move smooth relative examples

add more test code and update go tt mod

add more bitmap test code

add SaveImg function code

add drop function hint print support

add more key test code
add more test code
add paste string test code
add xvfb run codecov test

add keycode test support

add FindPath example code

add KeyTap() args[2] delay support

add find bitmap nil args support

add find color nil args support

add drag and move mouse multiple screens support

add drag mouse test code

Use CGDisplayBounds not CGDisplayPixelsWide, optimize get mac display size …

Update TypeStr function, add type delay and speed support

update PasteStr function code return error

### Update

Update robot info test code and Add go.yml test support

use while not for match special key map
remove unless x11 special key and sort

update go mod pkg
update mod vendor
remove vendor and update .gitignore

update and fmt config.yml, add Linux go test support
update Linux CI support x11 test

move hook to hook.go

update appveyor and test code
update version and code style

update move mouse smooth test code

update clipboard code and add test code

update test code and add codecov support

update show alert test code

update keycode.go

update window examples code

update test code remove windows alert test

move gops code to ps.go

update version

update unix get title type

gofmt go code and update code style

add ToBitmapBytes examples code

update example code, fixed golint warning

update bitmap example code

Update CHANGELOG.md

update code style

update godoc

update keytap code and code style

update Bitmap struct delete fuzzy api

update key examples code

add bitmap from string clear api

update go mod vendor
update go mod pkg not proxy

update bitmap example code

update test code fixed appveyor CI

update test code fixed equal error

update hook godoc

update event example code

update godoc and code style

update key example code

Update example README.md

update and tidy go mod

update code remove duplicate code and update godoc

update xgb getXid log

update GetBounds x11 error log

update cgo code and version

update TypeString function code [Drop]

update key example code

Update TypeStr function, optimize x11 type string

Update TypeStrDelay function, remove unused code

update code fixed x11 type sleep

Update key example code

use gops to simplify code
update key examples code

update bitmap examples code

update colorpicker and findcolor example code

update bitmap example code

update robotgo test code, add more test

Update README.md

rename type names make clearer

update types.h code and fixed bug

remove unused code fixed x11 build error

update robot info test code and appveyor

Update README.md, Add more CI badge

update gohook pkg and robot info test code

Update linux upper code, add more special key support

Create go.yml
Update go.yml
add more test and update go.yml
Update dockerfile to go1.13.5

update dockerfile and appveyor.yml
Update dockerfile and appveyor.yml to go1.14.3

remove Travis go1.11.x
update appveyor and dockerfile to go1.13.1
update dockerfile, go.yml and appveyor.yml to go1.14

update travis.yml to go1.14.x and remove go1.13.x
Update and fmt appveyor.ymlu
update dockerfile and appveyor to go1.12.5

update appveyor and dockerfile to go1.12.6

add CI go1.13 support
update config.yml
update and fmt travis.yml
Update Travis remove go1.12.x

Update issue and pull request template

### Fix

Update to utf-code function Fixed #189

Update x11 keypress upper code Fixed #243

type conversion needed in addMouse (#201)

update hook, Fixed #202 fatal error: concurrent map writes

add key Kind Fixed #203

optimize get title code, Fixed #165 and typo

Fixed gohook#3 mouse is_drag error on x11

Fixed #213 AddEvents() can't listen correctly multiple times

update clipboard error hand Fixed #212

Update go.mod fixing issue "invalid pseudo-version: does not match version …

update keyboard example code, #238

update go mod file Fixed #239

update gops and other mod files fixed bug


## RobotGo v0.80.0, Sierra Nevada

# Sierra Nevada

### Add

- [NEW] Add asynchronous event support
- [NEW] Add multiple keypress event listener support
- [NEW] Add hook start and end func
- [NEW] Add AddEvents, AddMouse, AddMousePos hook function
- [NEW] Add mul() scale func and optimize code
- [NEW] Refactor AddEvent() func and add keycode.go, update example
- [NEW] Add mouse map keycode
- [NEW] Add android null file
- [NEW] Add AddEvent "center" support
- [NEW] Update README.md, Add binding link

 <br/>

- [NEW] Format README.md and docs markdown
- [NEW] Update bitmap_save return code
- [NEW] Optimize code not defer and remove useless code
- [NEW] Update code style and godoc
- [NEW] Update go mod vendor
- [NEW] Add more event examples
- [NEW] add AddEvents, AddMouse, AddMousePos examples code


### Update

- [NEW] Update event example code add print hint
- [NEW] Update godoc
- [NEW] Update CHANGELOG.md
- [NEW] Update .gitignore
- [NEW] Update code style and examples
- [NEW] Update pkg to newest
- [NEW] Update CI add go1.12.x support
- [NEW] Move GetText() func code

### Fix

- [FIX] Add AddEvents func, Fixed #98, #61, #69...
- [FIX] Add asynchronous event support, Fixed #196, #89...
- [FIX] add AddMouse func, Fixed #138
- [FIX] Update _Ctype_char to C.char, Fixed go1.12 build error #191
- [FIX] Update hook, Fixed #195 warning and json break bug
- [FIX] Fixed color picker, Update README.md and docs


See Commits for more details, after Jan 7.


## RobotGo v0.70.0, Caloris Montes

# Caloris Montes

### Add

- [NEW] Update keyboard use sendInput not keybd_event
- [NEW] Update mouse use sendInput not mouse_event
- [NEW] Add drag mouse other button support
- [NEW] Add more numpad key support
- [NEW] Add numpad key and escape abbreviation support
- [NEW] Add new window10 zoom ratio
- [NEW] Add linux numpad key support
- [NEW] Add key "insert, printscreen" mac support
- [NEW] Add check mouse button func
- [NEW] Add keyTap run error return support and update godoc


 <br/>

- [NEW] Optimize and clearer keytap code
- [NEW] Optimize and clean keyToggle code
- [NEW] Update dockerfile clean image
- [NEW] Add color picker and getMousePos() example
- [NEW] Use go mod not dep, add go.mod remove dep files
- [NEW] Add GetColors func return string
- [NEW] Optimize defer code
<br/>

- [NEW] Add more godoc
- [NEW] Add add key "ctrl", "cmd" [ abbreviation ] support
- [NEW] Add add key "capslock", "numpad_lock" support
- [NEW] Add left and right "Ctrl, Shift, Alt, command" key support
- [NEW] Update check key flags support "cmd" and "ctrl"
- [NEW] Update key docs
- [NEW] Add millisleep func and update godoc
- [NEW] Add AddEvent() key "cmd" support
- [NEW] Update key example code
- [NEW] Update README.md, add Note go1.10.x issue
- [NEW] Update keytap and toggle return "" and code style


### Update

- [NEW] Update issue template more obvious
- [NEW] Update godoc
- [NEW] Update CHANGELOG.md
- [NEW] Update .gitignore
- [NEW] Update code style and examples
- [NEW] Update pkg to newest
- [NEW] Add more scale example
- [NEW] Add drag mouse example
<br/>

- [NEW] Update key docs and clear file name
- [NEW] Remove old useless code
- [NEW] Update README.md
- [NEW] Update CI add go1.11.4 version

### Fix

- [FIX] Fixed bitmapClick() parameter bug
- [FIX] Fixed some README.md typo
- [FIX] Update scale example code close #153
- [FIX] Update code style and fixed #endif error


See Commits for more details, after Otc 8.


## RobotGo v0.60.0, Mount Olympus: Mytikas

### Add

- [NEW] Add GetBounds func support (add get_client and get_frame C_func)
- [NEW] Add GetXId and GetXidFromPid func
- [NEW] Refactoring GetTitle() func allow by pid
- [NEW] Refactoring CloseWindow() allow by pid
- [NEW] Add SetHandPid() and GetHandPid() func support
- [NEW] Add FindCBitmap func support
 <br/>

- [NEW] Refactoring bitmap example code
- [NEW] Refactoring key example code
- [NEW] Refactoring window example code
- [NEW] Add an cbitmap example [#169]
- [NEW] Refactoring screen and event example code
- [NEW] Refactoring mouse example code
<br/>

- [NEW] Add more godoc
- [NEW] Add getTitle example by pid
- [NEW] Add close window example by pid
- [NEW] Add getBounds example
- [NEW] Split func and remove dep more clean
- [NEW] Simplify SaveCapture code
- [NEW] Update and merged get_pixel_color remove duplicate code
- [NEW] Update README.md, add Note go1.10.x


### Update

- [NEW] Update issue template more obvious
- [NEW] Move public mdata to pub
- [NEW] Update godoc
- [NEW] Update CHANGELOG.md
- [NEW] Move some pub method to pub.h and rename some c_func
- [NEW] Update code style and name style ( key, window and other )
- [NEW] Update robotgo unix export getXidFromPid func
- [NEW] Update set handle return use bool
<br/>

- [NEW] Update code style and move scale to win_sys.h
- [NEW] Update example add more lang
- [NEW] Update pkg to newest
- [NEW] Remove duplicate code and old useless code
- [NEW] Update and clean README.md
- [NEW] Update CI add go1.11.x version
- [NEW] Update scroll godoc and clearer parameter name
- [NEW] Update hint and code style
- [NEW] Update FindIds doc and only set name once in loop

### Fix

- [FIX] Update type_string fixed #155, fixed window missing  some character
- [FIX] Fixed GetWindowText return address of local variable and not use ternary operator ( GetTittle )
- [FIX] Update README.md Fixed Release badge

See Commits for more details, after Aug 8.


## RobotGo v0.50.0, The Appalachian Mountains

## Add

- [NEW] Add simple ocr support
- [NEW] Add max and min window api and win32.h file
- [NEW] Automatic free internal bitmap and add bitmapStr example
- [NEW] Update findBitmap and findColor default tolerance 0.5 to 0.01, [improve find accuracy and time]
- [NEW] Add more Window example
- [NEW] Add cross compile docs
- [NEW] Add free bitmap and tolerance godoc
- [NEW] Add GetForegroundWindow and FindWindow func support
- [NEW] Add bitmap to CBitmap func, Add ToCBitmap example to examples
- [NEW] Add get Scale and GetScaleSize func, get primary display DPI scale factor fix #129, #135
   Add Custom DPI Scaling support,
   Add scale default value,
   Add scale example

## Update

- [NEW] Update README.md [add freeBitmap example]
- [NEW] Optimize findColor and uniform API with findBitmap
- [NEW] Update godoc, CI and README.md
- [NEW] Update CHANGELOG.md
- [NEW] Update examples [add freeBitmap and update findColor]
- [NEW] Optimize bitmap code, optimize args and not try [many methods]
- [NEW] Update getPid type to int32
- [NEW] Update var and other code style, fix non-standard code
   Update code and update some name
- [NEW] Update pkg to newest
- [NEW] Remove duplicate code and old useless code
- [NEW] Update mouse click and fix moveClick and movesClick args
- [NEW] Update code style use if not try
- [NEW] Update clipboard example
- [NEW] Update typestr use return not else
- [NEW] Update mouse toggle, keytap and savebitmap func args
- [NEW] Update examples remove duplicate code
- [NEW] Update bitmap and other examples
- [NEW] Simplify linux dependency installation commands
- [NEW] Update issue_template.md
-[NEW] Update pull_request_template.md
- [NEW] Move govendor to dep
- [NEW] Update robotgo ci to 1.10.3

## Fix

- [FIX] Update active pid to fix #140, fixed linux activePid
- [FIX] Fixed findBitmap and findPic memory leak
- [FIX] Add getPxColor destroyMMBitmap fix memory leak
- [FIX] Fix float args not float32
- [FIX] Fix windows clipboard memory leak
- [FIX] Update macos .a downgrade to 10.10 just warning not exit [fix #102, #128, #134]
- [FIX] use 10.10 to compile .a verifyed multi os
- [FIX] Fix #145 not assert
- [FIX] Fix some warning use supplemental code

See Commits for more details, after Apr 30.


## RobotGo v0.49.0, Olympus Mons

### Add

- [NEW] Add get image size func
- [NEW] Add linux type string utf-8 support
- [NEW] Add scroll mouse support x, y
- [NEW] Add AddEvent() "esc" support fix #105
- [NEW] Add AddEvent "space" fix #110
- [NEW] Add clipboard choose primary mode on unix
- [NEW] Add move smooth return
- [NEW] Add more bitmap func and examples
- [NEW] Add MicroSleep func
- [NEW] Add find image by path


### Update

- [NEW] Update KeyToggle code
- [NEW] Update activePid allow Windows via hwnd
- [NEW] Update godoc and README.md
- [NEW] Update CHANGELOG.md
- [NEW] Update Kill() parameter and examples
- [NEW] Update examples and remove useless function
- [NEW] Update appveyor, circle and dockerfile
- [NEW] Update code style
- [NEW] Update and optimize func
- [NEW] Update travis support go 1.10
- [NEW] Update CI (use custom go image) and add func internalFindBitmap
- [NEW] Update godoc and deprecated GetBHandle
- [NEW] Optimize code func args and name


### Fix

- [FIX] Fix mac input method keytap not work
- [FIX] Fix clipboard golint
- [FIX] Update move smooth fix #96 (set mouse smooth speed)
- [FIX] Fix Getportion param to go type
- [FIX] Fix XFlush wait for events flushing

See Commits for more details, after Jan 25.

## RobotGo v0.48.0, Ben Nevis

### Add

- [NEW] Add active window by name func ActiveName
- [NEW] Add type string utf-8 support

Add func CharCodeAt, UnicodeType, PasteStr and update TypeStr, TypeString

- [NEW] Add count of bitmap func CountBitmap
- [NEW] Add func SaveCapture and examples
- [NEW] Add time sleep func Sleep
- [NEW] Add more key listen
- [NEW] Add func PointInBounds and examples
- [NEW] Add func GetPxColor return C.MMRGBHex
- [NEW] Add FindColorCS param tolerance
- [NEW] Add func ToBitmap and examples
- [NEW] Add CBitmap type and examples
- [NEW] Add more examples
- [NEW] Add func ToMMBitmapRef
- [NEW] Add func BitmapClick and MovesClick
- [NEW] Add func ToMMRGBHex convert color hex
- [NEW] Add  func count bitmap color and CountColorCS
- [NEW] Add more color processing and conversion

Add func ToMMRGBHex, U32ToHex, U8ToHex, PadHex, HexToRgb, RgbToHex and examples

- [NEW] Add func tochar bitmap and gostring and fmt code


### Update
- [NEW] Remove robot and examples
- [NEW] Update vendor and appveyor.yml
- [NEW] Update keyboard code
- [NEW] Update godoc
- [NEW] Update CHANGELOG.md
- [NEW] Change TostringBitmap return string
- [NEW] Update C language code and other naming
- [NEW] Update code and code style
- [NEW] Update move mouse smooth


### Fix

- [FIX] Fix mac set active and active by pid
- [FIX] Fix windows active by pid #101
- [FIX] Fix FindColor param tolerance
- [FIX] Fix find bitmap float args
- [FIX] Fix some range error
- [FIX] Update doc fix #97
- [FIX] Update README.md fix link error

See Commits for more details, after Dec 13.

## RobotGo v0.47.0, Mount Cook

### Add

- [NEW] Add windows 32bit and 64bit dependency
- [NEW] Add macOs dependency
- [NEW] Add pkg to vendor

Solve the problem of dependence, remove zlib/libpng dependencies

- [NEW] Add FindColorCS(x, y, w, h int, color CHex), CHex type and examples #84
- [NEW] Add kill the process
- [NEW] Add public event and update code
- [NEW] Add  Windows 32bit and 64bit Appveyor CI


### Update
- [NEW] Update png io
- [NEW] Update cgo link
- [NEW] Update .gitignore
- [NEW] Update README.md and godoc
- [NEW] Update CHANGELOG.md
- [NEW] Update circle to 2.0, add robotgo Dockerfile custom image
- [NEW] Update and fmt C code
- [NEW] Update GetTitle default value "null" to ""


### Fix

- [FIX] Fix FindColor inconvenient parameters
- [FIX] Fix installation requirements #72
- [FIX] Fix GetTitle `return address of local variable` in the higher gcc version. #81

See Commits for more details, after Nov 10.


## RobotGo v0.46.6, Pyrenees Mountains: Aneto Peak

## RobotGo v0.46.0, Pyrenees Mountains

### Add

- [NEW] Add ActivePID
- [NEW] Add FindBit
- [NEW] Add robot branch, where there is no zlib and libpng dependency

### Update

- [NEW] Update README.md
- [NEW] Update FindIds
- [NEW] Update examples
- [NEW] Update vendor
- [NEW] Update godoc and docs
- [NEW] Update and fix bitmap

### Fix

- [FIX] Fix MoveMouseSmooth args
- [FIX] Fix name err
- [FIX] Fix FindBitmap

## RobotGo v0.45.0, Mount Qomolangma

### Add
- [NEW] Add Process
- [NEW] Add TypeStr
- [NEW] Add DeepCopyBit
- [NEW] Add CopyBitpb
- [NEW] Add ReadBitmap
- [NEW] Add vendor.json
- [NEW] Add ReadAll: clipboard
- [NEW] Add WriteAll: clipboard
- [NEW] Add Pids : get the all process id
- [NEW] Add FindName: find the process name by the process id
- [NEW] Add FindNames: find the all process name
- [NEW] Add PidExists: determine whether the process exists
- [NEW] Add FindIds: find the process id by the process name
- [NEW] Add FreeBitmap and Update docs


### Update
- [NEW] Update docs
- [NEW] Update test
- [NEW] Update godoc
- [NEW] Update CHANGELOG.md
- [NEW] Update .gitignore
- [NEW] Update examples and docs
- [NEW] Update examples link
- [NEW] Update README.md and clipboard


### Fix

- [FIX] Fix release key
- [FIX] Fix godoc error


## RobotGo v0.44.0, Mount Kailash

### Add

- Add CHANGELOG.md
- Format some code
- Add fedora dependencies

### Update

- Update test
- Update keys.md
- Update and Split example
- Update godoc and docs
- Update and Cleanup README.md
- Update CONTRIBUTING.md and issue_template.md

### Fix

- Fix typesetting and MD error
- Fix fedora dependencies #55
- Fix doc.md and README.md
