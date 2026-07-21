//go:build windows && !cgo

package robotgo

import "testing"

const (
	windowsIntegrationStatusForeground  uintptr = 1 << 0
	windowsIntegrationStatusEditFocused uintptr = 1 << 1
	windowsIntegrationStatusReady               = windowsIntegrationStatusForeground |
		windowsIntegrationStatusEditFocused
)

type windowsPasteRecoveryReason uint8

const (
	windowsPasteRecoveryNone windowsPasteRecoveryReason = iota
	windowsPasteRecoveryFocusLoss
	windowsPasteRecoveryNoEditAcknowledgement
)

func (reason windowsPasteRecoveryReason) String() string {
	switch reason {
	case windowsPasteRecoveryFocusLoss:
		return "focus_loss"
	case windowsPasteRecoveryNoEditAcknowledgement:
		return "no_edit_acknowledgement"
	default:
		return "none"
	}
}

func windowsPasteFocusLossObserved(status, currentEvents, baselineEvents uintptr) bool {
	return status != windowsIntegrationStatusReady || currentEvents > baselineEvents
}

func windowsPasteFocusLossDelta(currentEvents, baselineEvents uintptr) uintptr {
	if currentEvents <= baselineEvents {
		return 0
	}
	return currentEvents - baselineEvents
}

func windowsPasteRecoveryFor(
	status, currentFocusEvents, baselineFocusEvents,
	currentEditChanges, baselineEditChanges uintptr,
) windowsPasteRecoveryReason {
	if currentEditChanges > baselineEditChanges {
		return windowsPasteRecoveryNone
	}
	if windowsPasteFocusLossObserved(status, currentFocusEvents, baselineFocusEvents) {
		return windowsPasteRecoveryFocusLoss
	}
	return windowsPasteRecoveryNoEditAcknowledgement
}

func windowsPasteEditChangeDelta(currentEvents, baselineEvents uintptr) uintptr {
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

func TestWindowsPasteRecoveryRequiresObservableTransient(t *testing.T) {
	tests := []struct {
		name                string
		status              uintptr
		currentFocusEvents  uintptr
		baselineFocusEvents uintptr
		currentEditChanges  uintptr
		baselineEditChanges uintptr
		want                windowsPasteRecoveryReason
		wantEditChangeDelta uintptr
	}{
		{
			name:                "focus loss without edit acknowledgement remains recoverable",
			status:              windowsIntegrationStatusForeground,
			currentFocusEvents:  2,
			baselineFocusEvents: 1,
			currentEditChanges:  4,
			baselineEditChanges: 4,
			want:                windowsPasteRecoveryFocusLoss,
		},
		{
			name:                "focus loss does not excuse unexpected edit mutation",
			status:              windowsIntegrationStatusForeground,
			currentFocusEvents:  2,
			baselineFocusEvents: 1,
			currentEditChanges:  5,
			baselineEditChanges: 4,
			want:                windowsPasteRecoveryNone,
			wantEditChangeDelta: 1,
		},
		{
			name:                "stable target without edit acknowledgement",
			status:              windowsIntegrationStatusReady,
			currentFocusEvents:  3,
			baselineFocusEvents: 3,
			currentEditChanges:  7,
			baselineEditChanges: 7,
			want:                windowsPasteRecoveryNoEditAcknowledgement,
		},
		{
			name:                "counter rollback is not an acknowledgement",
			status:              windowsIntegrationStatusReady,
			currentFocusEvents:  3,
			baselineFocusEvents: 3,
			currentEditChanges:  6,
			baselineEditChanges: 7,
			want:                windowsPasteRecoveryNoEditAcknowledgement,
		},
		{
			name:                "unexpected edit mutation is not retried",
			status:              windowsIntegrationStatusReady,
			currentFocusEvents:  3,
			baselineFocusEvents: 3,
			currentEditChanges:  8,
			baselineEditChanges: 7,
			want:                windowsPasteRecoveryNone,
			wantEditChangeDelta: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := windowsPasteRecoveryFor(
				test.status,
				test.currentFocusEvents,
				test.baselineFocusEvents,
				test.currentEditChanges,
				test.baselineEditChanges,
			)
			if got != test.want {
				t.Fatalf("windowsPasteRecoveryFor() = %s, want %s", got, test.want)
			}
			if got := windowsPasteEditChangeDelta(test.currentEditChanges, test.baselineEditChanges); got != test.wantEditChangeDelta {
				t.Fatalf("windowsPasteEditChangeDelta() = %d, want %d", got, test.wantEditChangeDelta)
			}
		})
	}
}
