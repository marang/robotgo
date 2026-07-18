//go:build !cgo

package robotgo

import (
	"fmt"
	"math"

	"github.com/shirou/gopsutil/v4/process"
)

func closeWindowProcessIdentity(pid int) (int64, error) {
	if pid <= 0 || int64(pid) > math.MaxInt32 {
		return 0, fmt.Errorf("invalid process id %d", pid)
	}
	instance, err := process.NewProcess(int32(pid))
	if err != nil {
		return 0, fmt.Errorf("open process %d for identity: %w", pid, err)
	}
	createdAt, err := instance.CreateTime()
	if err != nil {
		return 0, fmt.Errorf("read process %d creation time: %w", pid, err)
	}
	if createdAt <= 0 {
		return 0, fmt.Errorf("process %d returned invalid creation time %d", pid, createdAt)
	}
	return createdAt, nil
}
