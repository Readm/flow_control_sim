//go:build !debug
// +build !debug

package debug

// Enabled returns whether debug logging is enabled.
// In release build, always returns false.
func Enabled() bool {
	return false
}

// Logf logs a formatted message with goroutine ID and timestamp.
// In release build, this is a no-op and will be optimized away by the compiler.
func Logf(format string, args ...interface{}) {
	// Empty implementation - compiler will optimize this away
}
