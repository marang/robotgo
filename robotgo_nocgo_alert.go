//go:build !cgo && !linux

package robotgo

// Alert reports failure when no native dialog backend is selected.
func Alert(title, msg string, args ...string) bool {
	accepted, _ := AlertE(title, msg, args...)
	return accepted
}

// AlertE reports that this non-CGO platform has no dialog backend.
func AlertE(string, string, ...string) (bool, error) { return false, ErrNotSupported }
