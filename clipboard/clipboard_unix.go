// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build freebsd || linux || netbsd || openbsd || solaris || dragonfly
// +build freebsd linux netbsd openbsd solaris dragonfly

package clipboard

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	commandpkg "github.com/marang/robotgo/internal/command"
)

const (
	xsel  = "xsel"
	xclip = "xclip"
)

var (
	// Primary chooses primary mode for the legacy ReadAll and WriteAll APIs.
	// Deprecated: use ReadAllContext or WriteAllContext with SelectionPrimary.
	Primary bool

	errMissingCommands = errors.New("no clipboard utilities available; please install xsel or xclip")
	selectedTool       unixClipboardTool
)

type unixClipboardTool uint8

const (
	unixClipboardNone unixClipboardTool = iota
	unixClipboardXclip
	unixClipboardXsel
)

func init() {
	if _, err := exec.LookPath(xclip); err == nil {
		selectedTool = unixClipboardXclip
		return
	}
	if _, err := exec.LookPath(xsel); err == nil {
		selectedTool = unixClipboardXsel
		return
	}

	Unsupported = true
}

func unixClipboardCommand(tool unixClipboardTool, read bool, selection Selection) (string, []string, error) {
	selectionName := "clipboard"
	if selection == SelectionPrimary {
		selectionName = "primary"
	}
	switch tool {
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
	name, args, err := unixClipboardCommand(selectedTool, false, selection)
	if err != nil {
		return err
	}
	if err := commandpkg.Run(ctx, strings.NewReader(text), name, args...); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	return nil
}
