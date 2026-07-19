//go:build windows && !cgo

package robotgo

import "testing"

const (
	windowsIntegrationStatusForeground  uintptr = 1 << 0
	windowsIntegrationStatusEditFocused uintptr = 1 << 1
	windowsIntegrationStatusReady               = windowsIntegrationStatusForeground |
		windowsIntegrationStatusEditFocused
)

func windowsPasteFocusLossObserved(status, currentEvents, baselineEvents uintptr) bool {
	return status != windowsIntegrationStatusReady || currentEvents > baselineEvents
}

func windowsPasteFocusLossDelta(currentEvents, baselineEvents uintptr) uintptr {
	if currentEvents <= baselineEvents {
		return 0
	}
	return currentEvents - baselineEvents
}

func TestWindowsPasteRecoveryRequiresObservedFocusLoss(t *testing.T) {
	tests := []struct {
		name          string
		status        uintptr
		currentEvents uintptr
		baseline      uintptr
		want          bool
		wantDelta     uintptr
	}{
		{
			name:          "stable ready target",
			status:        windowsIntegrationStatusReady,
			currentEvents: 4,
			baseline:      4,
		},
		{
			name:          "foreground lost",
			status:        windowsIntegrationStatusEditFocused,
			currentEvents: 4,
			baseline:      4,
			want:          true,
		},
		{
			name:          "edit focus lost",
			status:        windowsIntegrationStatusForeground,
			currentEvents: 4,
			baseline:      4,
			want:          true,
		},
		{
			name:          "transient focus loss recovered",
			status:        windowsIntegrationStatusReady,
			currentEvents: 6,
			baseline:      4,
			want:          true,
			wantDelta:     2,
		},
		{
			name:          "counter does not underflow",
			status:        windowsIntegrationStatusReady,
			currentEvents: 3,
			baseline:      4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := windowsPasteFocusLossObserved(test.status, test.currentEvents, test.baseline); got != test.want {
				t.Fatalf("windowsPasteFocusLossObserved() = %t, want %t", got, test.want)
			}
			if got := windowsPasteFocusLossDelta(test.currentEvents, test.baseline); got != test.wantDelta {
				t.Fatalf("windowsPasteFocusLossDelta() = %d, want %d", got, test.wantDelta)
			}
		})
	}
}
