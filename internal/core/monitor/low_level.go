package monitor

import (
	_ "unsafe" // for go:linkname
)

// GetCPUCycles uses runtime.nanotime to get high-precision timestamp.
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// GetCPUCycles returns current CPU cycles (nanosecond precision).
func GetCPUCycles() uint64 {
	return uint64(nanotime())
}
