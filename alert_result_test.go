package robotgo

import "testing"

func TestNativeAlertResult(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantAccepted bool
		wantError    bool
	}{
		{name: "accepted", status: nativeAlertAccepted, wantAccepted: true},
		{name: "rejected", status: nativeAlertRejected},
		{name: "failed", status: nativeAlertFailed, wantError: true},
		{name: "unknown", status: 42, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			accepted, err := nativeAlertResult(test.status)
			if accepted != test.wantAccepted || (err != nil) != test.wantError {
				t.Fatalf(
					"nativeAlertResult(%d) = accepted %t, error %v; want accepted %t, error %t",
					test.status, accepted, err, test.wantAccepted, test.wantError,
				)
			}
		})
	}
}
