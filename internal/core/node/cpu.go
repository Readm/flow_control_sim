package node

// Pause executes the PAUSE instruction to hint the processor that this is a spin-wait loop.
// Implemented in cpu_amd64.s
func Pause()
