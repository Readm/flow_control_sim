package trace

import (
	_ "unsafe" // for go:linkname
)

// GetCPUCycles returns the current time in microseconds.
// Ideally usage of "Cycles" name is legacy from when it was RDTSC.
// Now we use nanotime() converted to float64 microseconds for Trace Viewer.
func GetCPUCycles() float64 {
	return float64(nanotime()) / 1000.0
}

//go:linkname nanotime runtime.nanotime
func nanotime() int64
