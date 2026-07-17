//go:build !linux

package robotgo

import "testing"

func TestPermissionFromCapabilityDoesNotOverclaimAmbiguousPreflight(t *testing.T) {
	tests := []struct {
		name       string
		capability FeatureCapability
		want       RuntimePermissionState
	}{
		{
			name: "denied",
			capability: FeatureCapability{
				Reason: ErrPermissionDenied.Error(),
			},
			want: RuntimePermissionDenied,
		},
		{
			name: "granted",
			capability: FeatureCapability{
				Available: true,
				Notes:     "Screen Recording permission is granted",
			},
			want: RuntimePermissionGranted,
		},
		{
			name: "preflight unavailable",
			capability: FeatureCapability{
				Available: true,
				Notes:     "Screen Recording permission is granted or cannot be preflighted",
			},
			want: RuntimePermissionUnknown,
		},
		{
			name: "unsupported",
			capability: FeatureCapability{
				Reason: ErrNotSupported.Error(),
			},
			want: RuntimePermissionUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := permissionFromCapability(test.capability, "permission is granted"); got != test.want {
				t.Fatalf("permission = %q, want %q", got, test.want)
			}
		})
	}
}
