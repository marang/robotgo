//go:build linux && !cgo && x11integration && !wayland

package robotgo_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/marang/robotgo"
)

func TestPureGoX11CrashRestoresScratchAndOwnedInput(t *testing.T) {
	if os.Getenv(x11CrashObserverEnv) == "1" {
		x11CrashWriteObserverSnapshot(t)
		return
	}
	if os.Getenv(x11CrashHelperEnv) == "1" {
		runPureGoX11CrashWorkload(t)
		return
	}

	harness := newX11InputHarness(t)
	initialInput := harness.inputState()
	foreignKey := x11CrashForeignKey(t, harness, initialInput)
	foreignButton, foreignButtonMask := x11CrashForeignButton(t, initialInput.pointerMask)
	harness.fakeKey(foreignKey, true)
	t.Cleanup(func() { harness.fakeKey(foreignKey, false) })
	harness.fakeButton(foreignButton, true)
	t.Cleanup(func() { harness.fakeButton(foreignButton, false) })
	harness.conn.Sync()
	beforeKeyboard := harness.keyboardState()
	beforeKeyboardBytes := x11CrashKeyboardBytes(beforeKeyboard)
	beforeInput := harness.inputState()
	if !x11CrashKeyPressed(beforeInput.pressedKeys, foreignKey) || beforeInput.pointerMask&foreignButtonMask == 0 {
		t.Fatalf("foreign baseline was not established: keycode=%d button=%d input=%+v", foreignKey, foreignButton, beforeInput)
	}
	beforeXKB, compareXKB := x11CrashXKBCompSnapshot(t)

	restoreSubreaper, err := x11CrashEnableSubreaper()
	if err != nil {
		t.Fatalf("enable child subreaper for crash lifecycle: %v", err)
	}
	t.Cleanup(func() {
		if err := restoreSubreaper(); err != nil {
			t.Errorf("restore child-subreaper state: %v", err)
		}
	})

	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create crash-helper readiness pipe: %v", err)
	}
	defer func() {
		if err := readyRead.Close(); err != nil {
			t.Errorf("close crash-helper readiness reader: %v", err)
		}
	}()

	logPath := t.TempDir() + "/workload.log"
	logFile, err := os.Create(logPath)
	if err != nil {
		_ = readyWrite.Close()
		t.Fatalf("create crash-helper log: %v", err)
	}
	defer func() {
		if err := logFile.Close(); err != nil {
			t.Errorf("close crash-helper log: %v", err)
		}
	}()

	executable, err := os.Executable()
	if err != nil {
		_ = readyWrite.Close()
		t.Fatalf("resolve crash-helper executable: %v", err)
	}
	command := exec.Command(
		executable,
		"-test.run="+x11CrashTestPattern,
		"-test.count=1",
		"-test.timeout="+x11CrashHelperTimeout.String(),
	)
	command.Env = x11CrashHelperEnvironment(os.Environ())
	command.ExtraFiles = []*os.File{readyWrite}
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := command.Start(); err != nil {
		_ = readyWrite.Close()
		t.Fatalf("start Pure-Go X11 crash workload: %v", err)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- command.Wait() }()
	var (
		helperWaited   bool
		helperWaitErr  error
		guardianPID    int
		guardianWaited bool
		restored       bool
		claim          x11CrashWorkloadClaim
	)
	waitForHelper := func(timeout time.Duration) (error, bool) {
		if helperWaited {
			return helperWaitErr, true
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case helperWaitErr = <-waitResult:
			helperWaited = true
			return helperWaitErr, true
		case <-timer.C:
			return nil, false
		}
	}
	t.Cleanup(func() {
		if guardianPID == 0 && !helperWaited {
			if pid, err := x11CrashOnlyChildPID(command.Process.Pid); err == nil {
				guardianPID = pid
			}
		}
		if !helperWaited {
			_ = command.Process.Signal(syscall.SIGKILL)
			if _, ok := waitForHelper(x11CrashWaitTimeout); !ok {
				t.Errorf("crash workload did not exit during test cleanup; log:\n%s", x11CrashHelperLog(logPath))
			}
		}
		if guardianPID == 0 && helperWaited {
			if pid, err := x11CrashOnlyChildPID(os.Getpid()); err == nil {
				guardianPID = pid
			}
		}
		if guardianPID != 0 && !guardianWaited {
			status, ok, waitErr := x11CrashWaitAdoptedChild(guardianPID, x11CrashGuardianTimeout)
			if waitErr != nil {
				t.Errorf("wait for guardian pid %d during cleanup: %v", guardianPID, waitErr)
			} else if ok {
				guardianWaited = true
				if err := x11CrashAssertGuardianExit(status); err != nil {
					t.Errorf("guardian cleanup exit: %v", err)
				}
			}
			if !guardianWaited {
				if terminateErr := x11CrashKillAndReapGuardian(guardianPID); terminateErr != nil {
					t.Errorf("terminate leaked guardian pid %d: %v", guardianPID, terminateErr)
				} else {
					guardianWaited = true
				}
			}
		}
		if !restored && claim.ready && (guardianPID == 0 || guardianWaited) {
			restoreErr := x11CrashEmergencyRestore(harness, claim)
			restoreErr = errors.Join(
				restoreErr,
				x11CrashVerifyBaseline(beforeKeyboardBytes, beforeInput, beforeXKB, compareXKB),
			)
			if restoreErr != nil {
				t.Errorf("%s failed: %v", x11CrashEmergencyReason, restoreErr)
			}
		}
	})
	if err := readyWrite.Close(); err != nil {
		t.Fatalf("close parent crash-helper readiness writer: %v", err)
	}

	ready, err := x11CrashReadReady(readyRead)
	if err != nil {
		if waitErr, ok := waitForHelper(100 * time.Millisecond); ok {
			t.Fatalf("crash workload exited before READY: ready error=%v wait=%v log:\n%s", err, waitErr, x11CrashHelperLog(logPath))
		}
		t.Fatalf("wait for crash workload READY: %v; log:\n%s", err, x11CrashHelperLog(logPath))
	}

	// Arm claim-bounded emergency cleanup immediately after the first trusted
	// server snapshot. All remaining checks are diagnostic and may fail without
	// leaving the exact scratch claim or synthetic holds behind.
	harness.conn.Sync()
	duringKeyboard := harness.keyboardState()
	claim, err = x11CrashClaim(beforeKeyboard, duringKeyboard, ready)
	if err != nil {
		t.Fatalf("record exact crash-workload ownership claim: %v", err)
	}
	guardianPID = ready.guardianPID

	if ready.pid != command.Process.Pid {
		t.Fatalf("crash workload READY pid = %d, want started pid %d", ready.pid, command.Process.Pid)
	}
	if guardianPID == ready.pid {
		t.Fatalf("guardian pid = workload pid = %d, want a separate process", guardianPID)
	}
	childPID, err := x11CrashOnlyChildPID(ready.pid)
	if err != nil {
		t.Fatalf("identify workload guardian child: %v", err)
	}
	if childPID != guardianPID {
		t.Fatalf("workload child pid = %d, helper reported guardian pid %d", childPID, guardianPID)
	}

	scratchCode, err := x11CrashScratchMutation(beforeKeyboard, duringKeyboard)
	if err != nil {
		t.Fatalf("crash workload did not establish the expected scratch mapping: %v", err)
	}
	if scratchCode != ready.scratchCode {
		t.Fatalf("observed scratch keycode = %d, helper reported %d", scratchCode, ready.scratchCode)
	}
	if ready.keysym != x11KeysymForRune('😀') {
		t.Fatalf("helper reported scratch keysym %#x, want %#x", ready.keysym, x11KeysymForRune('😀'))
	}
	duringInput := harness.inputState()
	heldKey, err := x11CrashHeldInput(beforeInput, duringInput, harness)
	if err != nil {
		t.Fatalf("crash workload did not establish the expected held input: %v", err)
	}
	if heldKey != ready.heldKeycode {
		t.Fatalf("observed held keycode = %d, helper reported %d", heldKey, ready.heldKeycode)
	}
	if compareXKB {
		duringXKB, ok := x11CrashXKBCompSnapshot(t)
		if !ok {
			t.Fatal("xkbcomp became unavailable after the before-state snapshot")
		}
		if bytes.Equal(duringXKB, beforeXKB) {
			t.Fatal("xkbcomp snapshot did not observe the confirmed server-side scratch mapping")
		}
	}

	// Kill only the workload PID. A crash-cleanup guardian must remain alive long
	// enough to observe the workload death, restore server-global state, and exit.
	if err := command.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("send SIGKILL to crash workload pid %d: %v; log:\n%s", command.Process.Pid, err, x11CrashHelperLog(logPath))
	}
	waitErr, ok := waitForHelper(x11CrashWaitTimeout)
	if !ok {
		t.Fatalf("crash workload pid %d did not report exit within %s", command.Process.Pid, x11CrashWaitTimeout)
	}
	if err := x11CrashAssertSIGKILL(waitErr); err != nil {
		t.Fatalf("crash workload termination: %v; log:\n%s", err, x11CrashHelperLog(logPath))
	}
	guardianStatus, guardianExited, guardianErr := x11CrashWaitAdoptedChild(guardianPID, x11CrashGuardianTimeout)
	if guardianErr != nil {
		terminateErr := x11CrashKillAndReapGuardian(guardianPID)
		guardianWaited = terminateErr == nil
		t.Fatalf("wait for adopted guardian pid %d: %v; forced cleanup: %v", guardianPID, guardianErr, terminateErr)
	}
	if !guardianExited {
		terminateErr := x11CrashKillAndReapGuardian(guardianPID)
		guardianWaited = terminateErr == nil
		t.Fatalf("guardian pid %d did not exit within %s; forced cleanup: %v", guardianPID, x11CrashGuardianTimeout, terminateErr)
	}
	guardianWaited = true
	if err := x11CrashAssertGuardianExit(guardianStatus); err != nil {
		t.Fatalf("guardian pid %d termination: %v", guardianPID, err)
	}

	if err := x11CrashAwaitRestoration(
		beforeKeyboard,
		beforeKeyboardBytes,
		beforeInput,
		beforeXKB,
		compareXKB,
	); err != nil {
		t.Fatalf("state was not restored after workload SIGKILL: %v; helper log:\n%s", err, x11CrashHelperLog(logPath))
	}
	restored = true
}

func runPureGoX11CrashWorkload(t *testing.T) {
	readyFile := os.NewFile(x11CrashReadyFD, "robotgo-x11-crash-ready")
	if readyFile == nil {
		t.Fatalf("crash workload has no readiness fd %d", x11CrashReadyFD)
	}
	defer func() { _ = readyFile.Close() }()
	defer func() {
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Errorf("crash workload fallback cleanup: %v", err)
		}
	}()

	info := robotgo.GetRuntimeBackendInfo()
	if info.CGOEnabled || info.BuildImplementation != robotgo.RuntimeImplementationPureGo || info.DisplayServer != robotgo.DisplayServerX11 {
		t.Fatalf("crash workload backend = %+v, want Pure-Go X11", info)
	}
	if err := robotgo.KeyToggle("enter", "down"); err != nil {
		t.Fatalf("hold named key in crash workload: %v", err)
	}
	if err := robotgo.Toggle("right", "down"); err != nil {
		t.Fatalf("hold pointer button in crash workload: %v", err)
	}
	if err := robotgo.TypeStrE("😀"); err != nil {
		t.Fatalf("establish Unicode scratch mapping in crash workload: %v", err)
	}
	guardianPID, ok := robotgo.X11GuardianPIDForIntegrationTest()
	if !ok || guardianPID <= 0 || guardianPID == os.Getpid() {
		t.Fatalf("resolve live Pure-Go X11 guardian pid: pid=%d available=%v", guardianPID, ok)
	}

	connection, err := x11CrashOpenObserver()
	if err != nil {
		t.Fatalf("open crash workload observer: %v", err)
	}
	setup := xproto.Setup(connection)
	if setup == nil {
		connection.Close()
		t.Fatal("crash workload observer has no X11 setup")
	}
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	mapping, err := xproto.GetKeyboardMapping(connection, setup.MinKeycode, byte(count)).Reply()
	if err != nil || mapping == nil || mapping.KeysymsPerKeycode == 0 {
		connection.Close()
		t.Fatalf("query crash workload scratch mapping: reply=%+v err=%v", mapping, err)
	}
	wantKeysym := x11KeysymForRune('😀')
	scratchCode := xproto.Keycode(0)
	per := int(mapping.KeysymsPerKeycode)
	for offset := 0; offset+per <= len(mapping.Keysyms); offset += per {
		if x11CrashMappingOwnedBy(mapping.Keysyms[offset:offset+per], xproto.Keysym(wantKeysym)) {
			scratchCode = setup.MinKeycode + xproto.Keycode(offset/per)
			break
		}
	}
	keys, err := xproto.QueryKeymap(connection).Reply()
	if err != nil || keys == nil {
		connection.Close()
		t.Fatalf("query crash workload held key: reply=%+v err=%v", keys, err)
	}
	heldKey := x11CrashFindPressedKeysym(connection, setup, keys.Keys, x11KeysymEnter)
	connection.Close()
	if scratchCode == 0 {
		t.Fatalf("crash workload cannot find its Unicode scratch keysym %#x", wantKeysym)
	}
	if heldKey == 0 {
		t.Fatal("crash workload cannot find its held Enter key")
	}
	if _, err := fmt.Fprintf(
		readyFile,
		"%s pid=%d guardian=%d held=%d scratch=%d keysym=%d\n",
		x11CrashReadyVersion,
		os.Getpid(),
		guardianPID,
		heldKey,
		scratchCode,
		wantKeysym,
	); err != nil {
		t.Fatalf("write crash workload READY: %v", err)
	}
	if err := readyFile.Close(); err != nil {
		t.Fatalf("close crash workload readiness fd: %v", err)
	}

	// The parent must be the only actor that terminates this workload, using
	// SIGKILL. No normal return or signal-handler cleanup is part of the proof.
	blocked := make(chan struct{})
	<-blocked
}
