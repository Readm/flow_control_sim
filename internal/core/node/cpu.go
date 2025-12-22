package node

// GetCPUCycles returns the current value of the CPU time-stamp counter (RDTSC).
// Implemented in cpu_amd64.s
func GetCPUCycles() uint64

// Pause executes the PAUSE instruction to hint the processor that this is a spin-wait loop.
// Implemented in cpu_amd64.s
func Pause()
