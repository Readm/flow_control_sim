package node

// GetCPUCycles returns the current value of the CPU time-stamp counter (RDTSC).
// Implemented in cpu_amd64.s
func GetCPUCycles() uint64
