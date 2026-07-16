//go:build linux && x11integration

package x11input

import "time"

// SetKeyHoldDelayForIntegrationTest changes the per-backend key hold while an
// external XKB client observes delivery. It is available only to tagged tests.
func (backend *Backend) SetKeyHoldDelayForIntegrationTest(delay time.Duration) func() {
	backend.mu.Lock()
	previous := backend.config.KeyHoldDelay
	backend.config.KeyHoldDelay = delay
	backend.mu.Unlock()
	return func() {
		backend.mu.Lock()
		backend.config.KeyHoldDelay = previous
		backend.mu.Unlock()
	}
}

// SetBeforeTextTapHookForIntegrationTest installs an adversarial hook between
// scratch reservation and the first text tap. The hook must not call Backend.
func (backend *Backend) SetBeforeTextTapHookForIntegrationTest(hook func()) func() {
	backend.mu.Lock()
	previous := backend.beforeTextTap
	backend.beforeTextTap = hook
	backend.mu.Unlock()
	return func() {
		backend.mu.Lock()
		backend.beforeTextTap = previous
		backend.mu.Unlock()
	}
}

// GuardianPIDForIntegrationTest returns the PID of the live re-executed
// guardian backing this Backend. It is available only to tagged integration
// tests and deliberately exposes no production lifecycle control.
func (backend *Backend) GuardianPIDForIntegrationTest() (int, bool) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	connection, ok := backend.conn.(*guardianConnection)
	if !ok || connection.process == nil || connection.process.Process == nil {
		return 0, false
	}
	return connection.process.Process.Pid, true
}
