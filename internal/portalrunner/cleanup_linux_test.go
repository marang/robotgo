//go:build linux

package portalrunner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupAbandonedRunsRemovesOnlySentinelOwnedRuntimes(t *testing.T) {
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	stateRoot := filepath.Join(parent, "state")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(stateRoot, repository); err != nil {
		t.Fatal(err)
	}
	runDirectory, err := CreateRun(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(runDirectory, "private.log"),
		[]byte("private"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	imageDirectory := filepath.Join(stateRoot, "images")
	if err := os.Mkdir(imageDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CleanupAbandonedRuns(context.Background(), stateRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(runDirectory); !os.IsNotExist(err) {
		t.Fatalf("abandoned runtime survived cleanup: %v", err)
	}
	if _, err := os.Stat(imageDirectory); err != nil {
		t.Fatalf("persistent image directory was removed: %v", err)
	}
}

func TestCleanupAbandonedRunsRejectsForgedRuntime(t *testing.T) {
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	stateRoot := filepath.Join(parent, "state")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(stateRoot, repository); err != nil {
		t.Fatal(err)
	}
	forged := filepath.Join(stateRoot, "run-forged")
	if err := os.Mkdir(forged, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CleanupAbandonedRuns(
		context.Background(),
		stateRoot,
	); err == nil {
		t.Fatal("forged runtime accepted for cleanup")
	}
	if _, err := os.Stat(forged); err != nil {
		t.Fatalf("forged runtime was removed: %v", err)
	}
}

func TestQEMUArgumentsMustBindPIDAndDiskToOwnedRuntime(t *testing.T) {
	t.Parallel()
	runDirectory := "/private/state/run-owned"
	pidFile := filepath.Join(runDirectory, "qemu.pid")
	valid := []byte(
		"qemu-system-x86_64\x00" +
			"-pidfile\x00" + pidFile + "\x00" +
			"-drive\x00file=" + filepath.Join(runDirectory, "hosted.qcow2") +
			",if=virtio,format=qcow2\x00",
	)
	if !qemuArgumentsBindRun(valid, runDirectory, pidFile) {
		t.Fatal("owned QEMU arguments were rejected")
	}
	for _, invalid := range [][]byte{
		[]byte(
			"qemu-system-x86_64\x00" +
				filepath.Join(runDirectory, "hosted.qcow2") + "\x00",
		),
		[]byte(
			"qemu-system-x86_64\x00-pidfile\x00" + pidFile + "\x00" +
				"/other/hosted.qcow2\x00",
		),
		[]byte(
			"qemu-system-x86_64\x00-pidfile\x00" + pidFile + "\x00" +
				filepath.Join(runDirectory, "hosted.qcow2") + "\x00",
		),
		[]byte(
			"qemu-system-x86_64\x00-pidfile\x00/other/qemu.pid\x00" +
				"-drive\x00file=" +
				filepath.Join(runDirectory, "hosted.qcow2") + "\x00",
		),
	} {
		if qemuArgumentsBindRun(invalid, runDirectory, pidFile) {
			t.Fatalf("unowned QEMU arguments were accepted: %q", invalid)
		}
	}
}
