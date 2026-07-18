//go:build !windows

package robotgo

import "math"

// Unix pid_t is signed. Passing a value that narrows to a negative pid can
// target a process group instead of one process.
const maxProcessIDForKill = uint64(math.MaxInt32)
