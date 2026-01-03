//go:build !trace

package trace

// GetGlobalTracer 禁用版本
func GetGlobalTracer() *TraceRecorder {
	return nil
}

// FlushGlobal 禁用版本
func FlushGlobal() error {
	return nil
}
