package robotgo

import (
	"context"

	"github.com/marang/robotgo/clipboard"
)

// ClipboardSelection identifies the clipboard selection to access.
type ClipboardSelection = clipboard.Selection

const (
	ClipboardSelectionClipboard = clipboard.SelectionClipboard
	ClipboardSelectionPrimary   = clipboard.SelectionPrimary
)

// ReadAllContext reads clipboard text with cancellation and explicit selection.
func ReadAllContext(ctx context.Context, selection ...ClipboardSelection) (string, error) {
	return clipboard.ReadAllContext(ctx, selection...)
}

// WriteAllContext writes clipboard text with cancellation and explicit selection.
func WriteAllContext(ctx context.Context, text string, selection ...ClipboardSelection) error {
	return clipboard.WriteAllContext(ctx, text, selection...)
}
