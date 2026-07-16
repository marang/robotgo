//go:build cgo

package robotgo

import "testing"

func TestUpstreamKeyConstants(t *testing.T) {
	t.Parallel()

	if KeyGrave != "`" {
		t.Fatalf("KeyGrave = %q, want backtick", KeyGrave)
	}
	if KeyQuote != "'" {
		t.Fatalf("KeyQuote = %q, want single quote", KeyQuote)
	}
	if KeyDoubleQuote != "\"" {
		t.Fatalf("KeyDoubleQuote = %q, want double quote", KeyDoubleQuote)
	}
	if KeyQuoter != KeyDoubleQuote {
		t.Fatalf("KeyQuoter = %q, want KeyDoubleQuote alias", KeyQuoter)
	}

	if _, ok := keyNames[ScrollLock]; !ok {
		t.Fatalf("keyNames missing %q", ScrollLock)
	}
	if _, ok := keyNames[PauseBreak]; !ok {
		t.Fatalf("keyNames missing %q", PauseBreak)
	}
	if got, err := checkKeyFlags(Control); err != nil || got == 0 {
		t.Fatalf("checkKeyFlags(%q) = (%d,%v), want a modifier", Control, got, err)
	}
}
