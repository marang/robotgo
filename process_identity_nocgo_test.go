//go:build !cgo

package robotgo

import (
	"os"
	"testing"
)

func TestCloseWindowProcessIdentityIsStableForCurrentProcess(t *testing.T) {
	first, err := closeWindowProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("first process identity: %v", err)
	}
	second, err := closeWindowProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("second process identity: %v", err)
	}
	if first <= 0 || second != first {
		t.Fatalf("process identities = (%d, %d), want equal positive values", first, second)
	}
}

func TestCloseWindowProcessIdentityRejectsInvalidPID(t *testing.T) {
	if _, err := closeWindowProcessIdentity(0); err == nil {
		t.Fatal("closeWindowProcessIdentity(0) succeeded")
	}
}
