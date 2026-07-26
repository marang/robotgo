//go:build linux

package portalrunner

import (
	"path/filepath"
	"testing"
)

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
			"qemu-system-x86_64\x00-pidfile\x00/other/qemu.pid\x00" +
				filepath.Join(runDirectory, "hosted.qcow2") + "\x00",
		),
	} {
		if qemuArgumentsBindRun(invalid, runDirectory, pidFile) {
			t.Fatalf("unowned QEMU arguments were accepted: %q", invalid)
		}
	}
}
