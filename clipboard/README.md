This directory based on [clipboard](https://github.com/atotto/clipboard).

[![Build Status](https://travis-ci.org/atotto/clipboard.svg?branch=master)](https://travis-ci.org/atotto/clipboard)
[![GoDoc](https://godoc.org/github.com/atotto/clipboard?status.svg)](http://godoc.org/github.com/atotto/clipboard)
<!--[![Build Status](https://drone.io/github.com/atotto/clipboard/status.png)](https://drone.io/github.com/atotto/clipboard/latest) -->

# Clipboard for Go

Provide copying and pasting to the Clipboard for Go.

<!--Download shell commands at https://drone.io/github.com/atotto/clipboard/files-->

Build:

    $ go get github.com/atotto/clipboard

Platforms:

* OSX
* Windows 7 (probably work on other Windows)
* Linux, Unix: Wayland sessions (`WAYLAND_DISPLAY` set) require both `wl-copy`
  and `wl-paste`; writes require wl-clipboard 2.3.0 or newer. Outside Wayland,
  the existing `xclip`-then-`xsel` command selection is retained.


Document: 

* http://godoc.org/github.com/atotto/clipboard

Notes:

* Text string only
* UTF-8 text encoding only (no conversion)
* `ReadAllContext` and `WriteAllContext` accept `SelectionClipboard` or
  `SelectionPrimary` without changing the deprecated global `Primary` default.
  A missing helper, unsupported selection, or failed Wayland operation returns
  an error without falling back to X11 or another selection.
* Wayland writes check the helper version before transferring text through
  stdin, preserve newline bytes, and clean an invocation-owned private spool
  directory on success, failure, and cancellation. Older or unrecognized copy
  helpers are rejected. Cleanup errors are reported and terminate the owner.
* A successful `wl-copy` background owner intentionally survives the call and
  later context cancellation, retaining its open, unlinked storage until
  ownership ends. Legacy calls keep their five-second timeout; context calls
  honor the caller's context, with version probing capped at five seconds.
* The opt-in [clipboard example](../examples/clipboard/main.go) demonstrates
  regular/primary access. The [testing guide](../TEST.md) documents hermetic
  fixtures and a separately gated, newly created headless-Sway runtime test.

Windows transactions stay on one OS thread. Reads are bounded by the native
allocation and require terminated UTF-16; unavailable text is an error, distinct
from an empty string. Writes reject embedded NUL and prepare storage before
clearing the old clipboard. A private message-only window owns each eager write
and is destroyed after closing the clipboard. Cancellation is checked before
publication; once clearing begins, a failure cannot restore arbitrary prior
clipboard formats. Cleanup failures are returned alongside the original error.

TODO:

* Clipboard watcher(?)

## Commands:

paste shell command:

    $ go get github.com/atotto/clipboard/cmd/gopaste
    $ # example:
    $ gopaste > document.txt

copy shell command:

    $ go get github.com/atotto/clipboard/cmd/gocopy
    $ # example:
    $ cat document.txt | gocopy



