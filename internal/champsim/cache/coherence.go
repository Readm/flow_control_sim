package cache

// coherence.go 定义 MESI 缓存一致性协议

// MESIState 表示 MESI 协议的四种状态
type MESIState int

const (
	// Invalid: 缓存行无效
	StateInvalid MESIState = iota

	// Shared: 缓存行有效，可能被多个核心共享，只读
	StateShared

	// Exclusive: 缓存行有效，独占，数据与内存一致，可读可写
	StateExclusive

	// Modified: 缓存行有效，独占，已修改，数据与内存不一致
	StateModified
)

func (s MESIState) String() string {
	switch s {
	case StateInvalid:
		return "I"
	case StateShared:
		return "S"
	case StateExclusive:
		return "E"
	case StateModified:
		return "M"
	default:
		return "Unknown"
	}
}

// CoherenceMessageType 缓存一致性消息类型
type CoherenceMessageType int

const (
	// 来自 CPU 的请求
	CoherenceRead  CoherenceMessageType = iota // 读请求
	CoherenceWrite                              // 写请求

	// 来自其他 Cache 的消息（通过 L2/Directory）
	CoherenceInvalidate      // 使其他核心的副本无效
	CoherenceInvalidateAck   // Invalidate 确认
	CoherenceSharedRequest   // 请求共享数据
	CoherenceSharedResponse  // 共享数据响应
	CoherenceExclusiveGrant  // 授予独占权限

	// 来自内存的响应
	CoherenceDataFromMemory // 来自内存的数据
	CoherenceWriteback      // 写回内存
)

func (t CoherenceMessageType) String() string {
	switch t {
	case CoherenceRead:
		return "Read"
	case CoherenceWrite:
		return "Write"
	case CoherenceInvalidate:
		return "Invalidate"
	case CoherenceInvalidateAck:
		return "InvAck"
	case CoherenceSharedRequest:
		return "SharedReq"
	case CoherenceSharedResponse:
		return "SharedResp"
	case CoherenceExclusiveGrant:
		return "ExclusiveGrant"
	case CoherenceDataFromMemory:
		return "DataFromMem"
	case CoherenceWriteback:
		return "Writeback"
	default:
		return "Unknown"
	}
}

// CoherenceMessage 一致性协议消息
type CoherenceMessage struct {
	Type      CoherenceMessageType
	Address   uint64
	Data      uint64
	RequestorID int  // 请求方的 Core ID
	SharedCount int  // 有多少个核心共享这个地址
}

// MESIController MESI 协议控制器
// 用于 L2 Cache 或 Directory 来管理多个 L1 Cache 的一致性
type MESIController struct {
	// directory: address -> (state, sharers)
	// sharers 是一个 bitmap，表示哪些核心持有这个地址
	directory map[uint64]*DirectoryEntry
}

// DirectoryEntry 目录条目
type DirectoryEntry struct {
	State   MESIState
	Sharers []int // Core IDs that have this cache line
	Owner   int   // Core ID that owns this line (for M/E state), -1 if none
}

// NewMESIController 创建 MESI 控制器
func NewMESIController() *MESIController {
	return &MESIController{
		directory: make(map[uint64]*DirectoryEntry),
	}
}

// HandleRequest 处理来自 L1 Cache 的请求
// 返回需要发送给其他 Cache 的消息列表
func (mc *MESIController) HandleRequest(msg CoherenceMessage) []CoherenceMessage {
	address := msg.Address
	requestorID := msg.RequestorID

	entry, exists := mc.directory[address]
	if !exists {
		entry = &DirectoryEntry{
			State:   StateInvalid,
			Sharers: []int{},
			Owner:   -1,
		}
		mc.directory[address] = entry
	}

	var responses []CoherenceMessage

	switch msg.Type {
	case CoherenceRead:
		responses = mc.handleRead(entry, address, requestorID)

	case CoherenceWrite:
		responses = mc.handleWrite(entry, address, requestorID)
	}

	return responses
}

// handleRead 处理读请求
func (mc *MESIController) handleRead(entry *DirectoryEntry, address uint64, requestorID int) []CoherenceMessage {
	var responses []CoherenceMessage

	switch entry.State {
	case StateInvalid:
		// 没有任何核心持有，授予 Exclusive
		entry.State = StateExclusive
		entry.Owner = requestorID
		entry.Sharers = []int{requestorID}

		responses = append(responses, CoherenceMessage{
			Type:        CoherenceExclusiveGrant,
			Address:     address,
			RequestorID: requestorID,
		})

	case StateShared:
		// 已经有核心共享，添加到共享者列表
		if !contains(entry.Sharers, requestorID) {
			entry.Sharers = append(entry.Sharers, requestorID)
		}

		responses = append(responses, CoherenceMessage{
			Type:        CoherenceSharedResponse,
			Address:     address,
			RequestorID: requestorID,
			SharedCount: len(entry.Sharers),
		})

	case StateExclusive, StateModified:
		// 有一个核心独占，需要降级为 Shared
		owner := entry.Owner

		// 通知 owner 降级为 Shared
		responses = append(responses, CoherenceMessage{
			Type:        CoherenceSharedRequest,
			Address:     address,
			RequestorID: owner,
		})

		// 更新状态
		entry.State = StateShared
		entry.Sharers = []int{owner, requestorID}
		entry.Owner = -1

		// 授予 requestor Shared 权限
		responses = append(responses, CoherenceMessage{
			Type:        CoherenceSharedResponse,
			Address:     address,
			RequestorID: requestorID,
			SharedCount: len(entry.Sharers),
		})
	}

	return responses
}

// handleWrite 处理写请求
func (mc *MESIController) handleWrite(entry *DirectoryEntry, address uint64, requestorID int) []CoherenceMessage {
	var responses []CoherenceMessage

	// 写操作需要 Invalidate 所有其他副本
	for _, sharerID := range entry.Sharers {
		if sharerID != requestorID {
			responses = append(responses, CoherenceMessage{
				Type:        CoherenceInvalidate,
				Address:     address,
				RequestorID: sharerID,
			})
		}
	}

	// 更新状态为 Modified
	entry.State = StateModified
	entry.Owner = requestorID
	entry.Sharers = []int{requestorID}

	return responses
}

// HandleInvalidateAck 处理 Invalidate 确认
func (mc *MESIController) HandleInvalidateAck(address uint64, coreID int) {
	entry, exists := mc.directory[address]
	if !exists {
		return
	}

	// 从共享者列表中移除
	entry.Sharers = removeFromSlice(entry.Sharers, coreID)
}

// GetDirectoryState 获取某个地址的目录状态（用于调试和统计）
func (mc *MESIController) GetDirectoryState(address uint64) *DirectoryEntry {
	return mc.directory[address]
}

// CoherenceStats 一致性协议统计
type CoherenceStats struct {
	InvalidatesSent     uint64 // 发送的 Invalidate 消息数
	SharedReads         uint64 // 共享读次数
	ExclusiveReads      uint64 // 独占读次数
	Upgrades            uint64 // S → M 升级次数
	Writebacks          uint64 // 写回次数
	CoherenceMisses     uint64 // 由于一致性导致的 miss
}

// 辅助函数
func contains(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

func removeFromSlice(slice []int, val int) []int {
	result := make([]int, 0, len(slice))
	for _, v := range slice {
		if v != val {
			result = append(result, v)
		}
	}
	return result
}
