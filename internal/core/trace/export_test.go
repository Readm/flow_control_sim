//go:build trace

package trace

import "sync"

// ResetGlobalTracer 重置全局 tracer 状态（仅用于测试）
func ResetGlobalTracer() {
	globalTracer = nil
	globalOnce = sync.Once{}
}
