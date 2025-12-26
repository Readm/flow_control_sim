package memory

// MemoryInterface 下级存储接口（抽象层）
//
// 设计目的：
// - Phase 1 (现在): DRAM直接实现此接口
// - Phase 2 (未来): 可替换为基于flow_sim Port/Link的实现
//
// 这样Cache只依赖接口，不依赖具体实现，便于后续架构演进
type MemoryInterface interface {
	// SendRequest 发送请求到下级存储
	//
	// 返回：
	// - true: 请求成功发送
	// - false: 队列满，请求被拒绝
	SendRequest(req *MemoryRequest) bool

	// Tick 时钟推进（每个cycle调用一次）
	Tick()

	// SetCycle 设置当前周期
	SetCycle(cycle uint64)
}

// MemoryRequest 内存请求（统一格式）
//
// 这个结构设计为通用格式，可以：
// - 直接映射到DRAM的DRAMPacket
// - 也可以封装为flow_sim的Packet
type MemoryRequest struct {
	// ==================== 地址信息 ====================

	// Address 物理地址
	Address uint64

	// VAddress 虚拟地址（可选）
	VAddress uint64

	// ==================== 请求信息 ====================

	// InstrID 指令ID（用于跟踪）
	InstrID uint64

	// IsWrite 是否为写请求
	// true: Write, false: Read
	IsWrite bool

	// Data 写数据（仅当IsWrite=true时有效）
	Data uint64

	// ==================== 回调 ====================

	// Callback 请求完成时的回调函数
	//
	// 参数：
	// - addr: 完成的地址
	// - data: 返回的数据
	// - cycle: 完成时的周期数
	//
	// Cache会在回调中调用HandleFill()来填充数据
	Callback func(addr uint64, data uint64, cycle uint64)
}
