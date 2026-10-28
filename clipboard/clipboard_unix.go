// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build freebsd || linux || netbsd || openbsd || solaris || dragonfly
// +build freebsd linux netbsd openbsd solaris dragonfly

package clipboard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	commandpkg "github.com/marang/robotgo/internal/command"
)

const (
	xsel                     = "xsel"
	xclip                    = "xclip"
	wlCopy                   = "wl-copy"
	wlPaste                  = "wl-paste"
	envWaylandDisplay        = "WAYLAND_DISPLAY"
	waylandClipboardTextMIME = "text/plain;charset=utf-8"
	waylandClipboardTextType = "text"
)

var (
	// Primary chooses primary mode for the legacy ReadAll and WriteAll APIs.
	// Deprecated: use ReadAllContext or WriteAllContext with SelectionPrimary.
	Primary bool

	errMissingCommands        = errors.New("no clipboard utilities available; please install xsel or xclip")
	errMissingWaylandCommands = errors.New("native Wayland clipboard requires both wl-copy and wl-paste; please install wl-clipboard (X11 fallback is not used in Wayland sessions)")
	selectedTool              unixClipboardTool
	selectedToolError         error
)

type unixClipboardTool uint8

const (
	unixClipboardNone unixClipboardTool = iota
	unixClipboardXclip
	unixClipboardXsel
	unixClipboardWayland
)

func init() {
	selectedTool, selectedToolError = detectUnixClipboardTool(os.Getenv, exec.LookPath)
	Unsupported = selectedToolError != nil
}

func detectUnixClipboardTool(getenv func(string) string, lookPath func(string) (string, error)) (unixClipboardTool, error) {
	if getenv(envWaylandDisplay) != "" {
		_, copyErr := lookPath(wlCopy)
		_, pasteErr := lookPath(wlPaste)
		if copyErr != nil || pasteErr != nil {
			return unixClipboardNone, errMissingWaylandCommands
		}
		return unixClipboardWayland, nil
	}
	// Keep legacy discovery when DISPLAY is unset: callers may set it later.
	if _, err := lookPath(xclip); err == nil {
		return unixClipboardXclip, nil
	}
	if _, err := lookPath(xsel); err == nil {
		return unixClipboardXsel, nil
	}
	return unixClipboardNone, errMissingCommands
}

func unixClipboardCommand(tool unixClipboardTool, read bool, selection Selection) (string, []string, error) {
	selectionName := "clipboard"
	if selection == SelectionPrimary {
		selectionName = "primary"
	}
	switch tool {
	case unixClipboardWayland:
		name, args := wlCopy, []string{"--type", waylandClipboardTextMIME}
		if read {
			name, args = wlPaste, []string{"--no-newline", "--type", waylandClipboardTextType}
		}
		if selection == SelectionPrimary {
			args = append(args, "--primary")
		}
		return name, args, nil
	case unixClipboardXclip:
		direction := "-in"
		if read {
			direction = "-out"
		}
		return xclip, []string{direction, "-selection", selectionName}, nil
	case unixClipboardXsel:
		direction := "--input"
		if read {
			direction = "--output"
		}
		return xsel, []string{direction, "--" + selectionName}, nil
	default:
		return "", nil, errMissingCommands
	}
}

func readAll() (string, error) {
	selection := SelectionClipboard
	if Primary {
		selection = SelectionPrimary
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()
	return readAllContext(ctx, selection)
}

func readAllContext(ctx context.Context, selection Selection) (string, error) {
	if selectedToolError != nil {
		return "", selectedToolError
	}
	name, args, err := unixClipboardCommand(selectedTool, true, selection)
	if err != nil {
		return "", err
	}
	out, err := commandpkg.Output(ctx, name, args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", err
	}
	return string(out), nil
}

func writeAll(text string) error {
	selection := SelectionClipboard
	if Primary {
		selection = SelectionPrimary
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()
	return writeAllContext(ctx, text, selection)
}

func writeAllContext(ctx context.Context, text string, selection Selection) error {
	if selectedToolError != nil {
		return selectedToolError
	}
	name, args, err := unixClipboardCommand(selectedTool, false, selection)
	if err != nil {
		return err
	}
	if selectedTool == unixClipboardWayland {
		return writeWaylandClipboardContext(ctx, text, name, args)
	}
	if err := commandpkg.Run(ctx, strings.NewReader(text), name, args...); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	return nil
}
