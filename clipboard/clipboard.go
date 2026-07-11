// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package clipboard read/write on clipboard
*/
package clipboard

import (
	"context"
	"fmt"
	"time"
)

const defaultCommandTimeout = 5 * time.Second

// Selection identifies the clipboard selection to access.
type Selection uint8

const (
	SelectionClipboard Selection = iota
	SelectionPrimary
)

func selectionArg(selection []Selection) (Selection, error) {
	if len(selection) > 1 {
		return 0, fmt.Errorf("clipboard: expected at most one selection, got %d", len(selection))
	}
	if len(selection) == 0 {
		return SelectionClipboard, nil
	}
	if selection[0] != SelectionClipboard && selection[0] != SelectionPrimary {
		return 0, fmt.Errorf("clipboard: invalid selection %d", selection[0])
	}
	return selection[0], nil
}

// ReadAll read string from clipboard
func ReadAll() (string, error) {
	return readAll()
}

// ReadAllContext reads the requested selection and allows cancellation.
func ReadAllContext(ctx context.Context, selection ...Selection) (string, error) {
	selected, err := selectionArg(selection)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return readAllContext(ctx, selected)
}

// WriteAll write string to clipboard
func WriteAll(text string) error {
	return writeAll(text)
}

// WriteAllContext writes the requested selection and allows cancellation.
func WriteAllContext(ctx context.Context, text string, selection ...Selection) error {
	selected, err := selectionArg(selection)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return writeAllContext(ctx, text, selected)
}

// Unsupported might be set true during clipboard init,
// to help callers decide whether or not to
// offer clipboard options.
var Unsupported bool
