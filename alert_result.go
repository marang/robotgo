package robotgo

import "fmt"

const (
	nativeAlertAccepted = 0
	nativeAlertRejected = 1
	nativeAlertFailed   = -1
)

func nativeAlertResult(status int) (bool, error) {
	switch status {
	case nativeAlertAccepted:
		return true, nil
	case nativeAlertRejected:
		return false, nil
	default:
		return false, fmt.Errorf("native alert backend failed with status %d", status)
	}
}
