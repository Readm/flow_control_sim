package cache

// coherence.go 定义缓存一致性协议相关的常量和类型
// 支持 MESI、MOESI 等常见协议

// Snoop 操作码定义
// 这些操作码在 HandleSnoop() 方法中使用，用于处理来自其他缓存的请求
const (
	// SnoopRead 表示其他缓存想要读取该缓存行
	// 响应：如果有数据，可能需要降级状态（M/E -> S）
	SnoopRead = 1

	// SnoopReadX 表示其他缓存想要独占该缓存行（写操作）
	// 响应：如果有数据，需要提供并 invalidate 本地副本
	SnoopReadX = 2

	// SnoopInvalidate 表示其他缓存要写入，需要 invalidate 所有其他副本
	// 响应：Invalidate 本地缓存行
	SnoopInvalidate = 3

	// SnoopUpgrade 表示从 Shared 状态升级到 Modified 状态
	// 响应：Invalidate 本地缓存行（如果在 Shared 状态）
	SnoopUpgrade = 4

	// SnoopWriteback 表示其他缓存正在写回脏数据
	// 响应：更新目录状态（通常在 directory 中处理）
	SnoopWriteback = 5
)

// Snoop 响应操作码
// 在 SnoopResponse.ResponseOpcode 中使用
const (
	// SnoopResponseNoData 表示本缓存没有该数据或无需提供数据
	SnoopResponseNoData = 0

	// SnoopResponseData 表示本缓存提供数据
	SnoopResponseData = 1

	// SnoopResponseShared 表示本缓存有共享副本（但不提供数据）
	SnoopResponseShared = 2

	// SnoopResponseExclusive 表示本缓存有独占副本
	SnoopResponseExclusive = 3

	// SnoopResponseModified 表示本缓存有修改过的副本（脏数据）
	SnoopResponseModified = 4
)

// MESI 状态转换辅助函数

// MESIHandleReadSnoop 处理 MESI 协议的读 snoop
// 返回：(newState, shouldProvideData)
func MESIHandleReadSnoop(currentState State) (State, bool) {
	switch currentState {
	case StateModified:
		// M -> S, 提供数据
		return StateShared, true
	case StateExclusive:
		// E -> S, 可以提供数据（取决于实现）
		return StateShared, true
	case StateShared:
		// S -> S, 不提供数据（内存/其他缓存提供）
		return StateShared, false
	case StateInvalid:
		// I -> I, 无数据
		return StateInvalid, false
	default:
		return StateInvalid, false
	}
}

// MESIHandleWriteSnoop 处理 MESI 协议的写 snoop (ReadX/Invalidate)
// 返回：(newState, shouldProvideData)
func MESIHandleWriteSnoop(currentState State) (State, bool) {
	switch currentState {
	case StateModified:
		// M -> I, 提供脏数据
		return StateInvalid, true
	case StateExclusive:
		// E -> I, 提供数据
		return StateInvalid, true
	case StateShared:
		// S -> I, 不提供数据
		return StateInvalid, false
	case StateInvalid:
		// I -> I, 无数据
		return StateInvalid, false
	default:
		return StateInvalid, false
	}
}

// MOESIHandleReadSnoop 处理 MOESI 协议的读 snoop
// 返回：(newState, shouldProvideData)
func MOESIHandleReadSnoop(currentState State) (State, bool) {
	switch currentState {
	case StateModified:
		// M -> O, 提供数据并保持 Owned 状态
		return StateOwned, true
	case StateExclusive:
		// E -> S
		return StateShared, true
	case StateOwned:
		// O -> O, 提供数据
		return StateOwned, true
	case StateShared:
		// S -> S
		return StateShared, false
	case StateInvalid:
		// I -> I
		return StateInvalid, false
	default:
		return StateInvalid, false
	}
}

// MOESIHandleWriteSnoop 处理 MOESI 协议的写 snoop
// 返回：(newState, shouldProvideData)
func MOESIHandleWriteSnoop(currentState State) (State, bool) {
	switch currentState {
	case StateModified:
		// M -> I, 提供数据
		return StateInvalid, true
	case StateExclusive:
		// E -> I
		return StateInvalid, false
	case StateOwned:
		// O -> I, 提供数据
		return StateInvalid, true
	case StateShared:
		// S -> I
		return StateInvalid, false
	case StateInvalid:
		// I -> I
		return StateInvalid, false
	default:
		return StateInvalid, false
	}
}
