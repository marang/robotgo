//go:build windows

package robotgo

import "math"

// Windows process identifiers use the complete uint32 range.
const maxProcessIDForKill = uint64(math.MaxUint32)
