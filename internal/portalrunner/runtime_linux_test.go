//go:build linux

package portalrunner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProtectedRunIdentityIsCanonicalAndExact(t *testing.T) {
	t.Parallel()

	identity := protectedRunFixture()
	if err := identity.validate(); err != nil {
		t.Fatalf("validate fixture: %v", err)
	}
	if got := identity.workflowName(); got != "RemoteDesktop E2E" {
		t.Fatalf("workflowName() = %q", got)
	}
	if got := identity.runnerName("gnome"); got !=
		"robotgo-gnome-12345-2-remote-desktop" {
		t.Fatalf("runnerName() = %q", got)
	}

	for _, change := range []func(*ProtectedRunIdentity){
		func(value *ProtectedRunIdentity) { value.Commit = strings.Repeat("A", 40) },
		func(value *ProtectedRunIdentity) { value.RunID = "012345" },
		func(value *ProtectedRunIdentity) { value.RunAttempt = 0 },
		func(value *ProtectedRunIdentity) { value.Cell = "arbitrary" },
	} {
		changed := identity
		change(&changed)
		if err := changed.validate(); err == nil {
			t.Fatalf("invalid protected identity was accepted: %+v", changed)
		}
	}
}

func TestWaitForOperatorRequiresExactReadyLine(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		ok    bool
	}{
		{input: "READY\n", ok: true},
		{input: "READY", ok: true},
		{input: " READY \n"},
		{input: "ready\n"},
		{input: "READY later\n"},
		{input: strings.Repeat("R", operatorInputLimit+1)},
	} {
		err := waitForOperator(
			context.Background(),
			strings.NewReader(test.input),
		)
		if (err == nil) != test.ok {
			t.Fatalf("waitForOperator(%q) error = %v, ok = %t", test.input, err, test.ok)
		}
	}
}

func TestWaitForOperatorHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	cancel()
	err := waitForOperator(ctx, reader)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForOperator() error = %v, want context cancellation", err)
	}
}

func TestValidateVisibleQEMURequiresDesktopModules(t *testing.T) {
	t.Parallel()

	good := &scriptedCommandExecutor{outputs: []string{
		"name \"virtio-vga\"\nname \"qemu-xhci\"\nname \"usb-kbd\"\nname \"usb-tablet\"\n",
		"Available display backend types:\ngtk\n",
	}}
	if err := validateVisibleQEMU(context.Background(), good); err != nil {
		t.Fatalf("validateVisibleQEMU: %v", err)
	}

	missing := &scriptedCommandExecutor{outputs: []string{
		"name \"qemu-xhci\"\nname \"usb-kbd\"\nname \"usb-tablet\"\n",
	}}
	if err := validateVisibleQEMU(
		context.Background(),
		missing,
	); err == nil || !strings.Contains(err.Error(), "virtio-vga") {
		t.Fatalf("validateVisibleQEMU() error = %v, want virtio-vga rejection", err)
	}
}

func TestBoundedWriterSerializesConcurrentWriters(t *testing.T) {
	t.Parallel()

	var destination bytes.Buffer
	writer := &boundedWriter{destination: &destination, remaining: 1024}
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := writer.Write([]byte("safe\n")); err != nil {
				t.Errorf("Write: %v", err)
			}
		}()
	}
	group.Wait()
	if got, want := destination.Len(), 20*len("safe\n"); got != want {
		t.Fatalf("destination length = %d, want %d", got, want)
	}
}

func TestClearBytesRemovesTokenMaterial(t *testing.T) {
	t.Parallel()

	value := []byte("registration-secret")
	clearBytes(value)
	if !bytes.Equal(value, make([]byte, len(value))) {
		t.Fatalf("clearBytes() left token material: %q", value)
	}
}

type successfulGitHubCleanup struct {
	deleted chan string
}

func (client *successfulGitHubCleanup) ValidateRun(
	context.Context,
	ProtectedRunIdentity,
) error {
	return nil
}

func (client *successfulGitHubCleanup) RegistrationToken(
	context.Context,
	string,
) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (client *successfulGitHubCleanup) RunSucceeded(
	context.Context,
	ProtectedRunIdentity,
) (bool, error) {
	return true, nil
}

func (client *successfulGitHubCleanup) DeleteRunner(
	_ context.Context,
	_ string,
	name string,
) error {
	client.deleted <- name
	return nil
}

func TestRemoveRegisteredRunnerConfirmsExactName(t *testing.T) {
	t.Parallel()

	client := &successfulGitHubCleanup{deleted: make(chan string, 1)}
	var output bytes.Buffer
	if err := removeRegisteredRunner(
		context.Background(),
		client,
		"marang/robotgo",
		"exact-runner",
		&output,
	); err != nil {
		t.Fatalf("removeRegisteredRunner: %v", err)
	}
	select {
	case got := <-client.deleted:
		if got != "exact-runner" {
			t.Fatalf("deleted runner = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("runner deletion was not attempted")
	}
	if !strings.Contains(output.String(), "runner cleanup complete") {
		t.Fatalf("cleanup output = %q", output.String())
	}
}

type completionGitHub struct {
	successAfter int
	calls        int
}

func (client *completionGitHub) ValidateRun(
	context.Context,
	ProtectedRunIdentity,
) error {
	return nil
}

func (client *completionGitHub) RegistrationToken(
	context.Context,
	string,
) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (client *completionGitHub) RunSucceeded(
	context.Context,
	ProtectedRunIdentity,
) (bool, error) {
	client.calls++
	return client.calls >= client.successAfter, nil
}

func (client *completionGitHub) DeleteRunner(
	context.Context,
	string,
	string,
) error {
	return nil
}

func TestWaitForRunSuccessRejectsUnfinishedRunAtDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := waitForRunSuccess(
		ctx,
		&completionGitHub{successAfter: 100},
		protectedRunFixture(),
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForRunSuccess() error = %v, want deadline", err)
	}
}
