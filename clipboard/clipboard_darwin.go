// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin
// +build darwin

package clipboard

import (
	"context"
	"fmt"
	"strings"

	commandpkg "github.com/marang/robotgo/internal/command"
)

var (
	pasteCmdArgs = "pbpaste"
	copyCmdArgs  = "pbcopy"
)

func readAll() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()
	return readAllContext(ctx, SelectionClipboard)
}

func readAllContext(ctx context.Context, selection Selection) (string, error) {
	if selection != SelectionClipboard {
		return "", fmt.Errorf("clipboard: primary selection is unsupported on macOS")
	}
	out, err := commandpkg.Output(ctx, pasteCmdArgs)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", err
	}
	return string(out), nil
}

func writeAll(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()
	return writeAllContext(ctx, text, SelectionClipboard)
}

func writeAllContext(ctx context.Context, text string, selection Selection) error {
	if selection != SelectionClipboard {
		return fmt.Errorf("clipboard: primary selection is unsupported on macOS")
	}
	if err := commandpkg.Run(ctx, strings.NewReader(text), copyCmdArgs); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	return nil
}
