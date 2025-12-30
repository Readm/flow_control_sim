package trace

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Readm/flow_sim/internal/champsim/instruction"
)

const (
	// BlockSize 每个trace块的大小（2MB）
	BlockSize = 2 * 1024 * 1024

	// EstimatedInstrSize 每条指令的估计大小（字节）
	EstimatedInstrSize = 100

	// InstrsPerBlock 每个块包含的指令数（约20000条）
	InstrsPerBlock = BlockSize / EstimatedInstrSize

	// LoadAhead 预加载块数
	LoadAhead = 2

	// MaxCachedBlocks 最大缓存块数
	MaxCachedBlocks = 100
)

var (
	// globalTracePool 全局trace池单例
	globalTracePool = &SharedTracePool{
		traces: make(map[string]*SharedTraceData),
	}
)

// TraceBlock 表示一个2MB的trace数据块（只读，可共享）
type TraceBlock struct {
	blockID      uint64                         // 块ID（第几个2MB块）
	startOffset  uint64                         // 起始指令ID
	instructions []*instruction.OOOModelInstr   // 只读指令数据
}

// SharedTracePool 全局trace池
type SharedTracePool struct {
	traces map[string]*SharedTraceData
	mu     sync.Mutex
}

// SharedTraceData 共享的trace数据
type SharedTraceData struct {
	filename string
	format   TraceFormat
	reader   io.ReadCloser
	eof      bool

	// 块管理
	blocks    []*TraceBlock // 有序的块列表
	blocksMu  sync.Mutex    // 保护blocks数组的修改
	nextBlock uint64        // 下一个要加载的块ID

	// 全局位置追踪（仅在块边界更新）
	minBlockID atomic.Uint64 // 最慢reader所在的块ID
	maxBlockID atomic.Uint64 // 最快reader所在的块ID

	// 自适应预取
	prefetchDist atomic.Uint64 // 预取距离（领先maxBlockID多少个块）
	stopPrefetch chan struct{}  // 停止预取信号

	// Reader管理
	readers   []*SharedTraceReader
	readersMu sync.Mutex
}

// SharedTraceReader 每个CPU的trace读取器
type SharedTraceReader struct {
	cpuID        uint8
	shared       *SharedTraceData

	// 完全本地的变量（无共享，无原子操作）
	readPosition     uint64 // 当前读取位置（全局指令ID）
	instrCounter     uint64 // 指令ID计数器
	lastReportedBlock uint64 // 上次报告的块ID

	// 本地缓存（指向共享的只读数据）
	cachedBlocks       []*TraceBlock // 当前可用的块
	currentBlock       *TraceBlock   // 当前正在读取的块（优化热路径）
	currentBlockIndex  int           // 当前块在数组中的索引
	currentInstrIndex  int           // 当前块内的指令索引
	cachedStartOffset  uint64        // 缓存的起始偏移
	cachedEndOffset    uint64        // 缓存的结束偏移
}

// GetOrCreateSharedTrace 获取或创建共享trace
func (p *SharedTracePool) GetOrCreateSharedTrace(filename string, format TraceFormat) (*SharedTraceData, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if shared, exists := p.traces[filename]; exists {
		return shared, nil
	}

	reader, err := openTraceFile(filename, format)
	if err != nil {
		return nil, err
	}

	shared := &SharedTraceData{
		filename:     filename,
		format:       format,
		reader:       reader,
		eof:          false,
		blocks:       make([]*TraceBlock, 0, MaxCachedBlocks),
		nextBlock:    0,
		readers:      make([]*SharedTraceReader, 0),
		stopPrefetch: make(chan struct{}),
	}
	shared.minBlockID.Store(0)
	shared.maxBlockID.Store(0)
	shared.prefetchDist.Store(1) // 初始预取距离为1

	// 启动后台预取goroutine
	go shared.prefetchLoop()

	p.traces[filename] = shared
	return shared, nil
}

// ReleaseSharedTrace 释放共享trace引用
func (p *SharedTracePool) ReleaseSharedTrace(filename string, reader *SharedTraceReader) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	shared, exists := p.traces[filename]
	if !exists {
		return nil
	}

	shared.readersMu.Lock()
	for i, r := range shared.readers {
		if r == reader {
			shared.readers = append(shared.readers[:i], shared.readers[i+1:]...)
			break
		}
	}
	readerCount := len(shared.readers)
	shared.readersMu.Unlock()

	if readerCount == 0 {
		// 停止预取goroutine
		close(shared.stopPrefetch)

		if shared.reader != nil {
			shared.reader.Close()
		}
		delete(p.traces, filename)
	}

	return nil
}

// openTraceFile 打开trace文件
func openTraceFile(filename string, format TraceFormat) (io.ReadCloser, error) {
	return openXZFile(filename)
}

// RegisterReader 注册reader
func (s *SharedTraceData) RegisterReader(reader *SharedTraceReader) {
	s.readersMu.Lock()
	defer s.readersMu.Unlock()
	s.readers = append(s.readers, reader)
}

// loadBlock 加载一个新的2MB块
// 必须在持有blocksMu的情况下调用
func (s *SharedTraceData) loadBlock(blockID uint64) (*TraceBlock, error) {
	if s.eof && blockID >= s.nextBlock {
		return nil, io.EOF
	}

	// 检查块是否已存在
	for _, block := range s.blocks {
		if block.blockID == blockID {
			return block, nil
		}
	}

	// 确保按顺序加载
	if blockID != s.nextBlock {
		return nil, fmt.Errorf("cannot load block %d, next expected is %d", blockID, s.nextBlock)
	}

	// 加载约InstrsPerBlock条指令
	block := &TraceBlock{
		blockID:      blockID,
		startOffset:  blockID * InstrsPerBlock,
		instructions: make([]*instruction.OOOModelInstr, 0, InstrsPerBlock),
	}

	loaded := 0
	for loaded < InstrsPerBlock && !s.eof {
		var rawInstr InputInstr
		err := binary.Read(s.reader, binary.LittleEndian, &rawInstr)
		if err == io.EOF {
			s.eof = true
			break
		}
		if err != nil {
			return nil, err
		}

		instr := instruction.NewOOOModelInstrFromInput(
			0,
			rawInstr.IP,
			rawInstr.IsBranch,
			rawInstr.BranchTaken,
			rawInstr.DestRegisters[:],
			rawInstr.SrcRegisters[:],
			rawInstr.DestMemory[:],
			rawInstr.SrcMemory[:],
		)

		block.instructions = append(block.instructions, instr)
		loaded++
	}

	if len(block.instructions) == 0 {
		return nil, io.EOF
	}

	s.blocks = append(s.blocks, block)
	s.nextBlock = blockID + 1

	return block, nil
}

// cleanupOldBlocks 清理所有reader都已通过的旧块
func (s *SharedTraceData) cleanupOldBlocks() {
	minBlock := s.minBlockID.Load()

	s.blocksMu.Lock()
	defer s.blocksMu.Unlock()

	// 保留minBlock及之后的块
	newBlocks := make([]*TraceBlock, 0, len(s.blocks))
	for _, block := range s.blocks {
		if block.blockID >= minBlock {
			newBlocks = append(newBlocks, block)
		}
	}
	s.blocks = newBlocks
}

// ensureBlocksLoaded 确保指定范围的块已加载
func (s *SharedTraceData) ensureBlocksLoaded(startBlock, endBlock uint64) error {
	s.blocksMu.Lock()
	defer s.blocksMu.Unlock()

	for blockID := startBlock; blockID <= endBlock; blockID++ {
		// 检查是否已存在
		found := false
		for _, block := range s.blocks {
			if block.blockID == blockID {
				found = true
				break
			}
		}

		if !found {
			if _, err := s.loadBlock(blockID); err != nil {
				if err == io.EOF {
					return err
				}
				return err
			}
		}
	}

	return nil
}

// getBlock 获取指定的块
func (s *SharedTraceData) getBlock(blockID uint64) *TraceBlock {
	s.blocksMu.Lock()
	defer s.blocksMu.Unlock()

	for _, block := range s.blocks {
		if block.blockID == blockID {
			return block
		}
	}
	return nil
}

// NewSharedTraceReader 创建共享trace读取器
func NewSharedTraceReader(filename string, cpuID uint8, format TraceFormat) (TraceReader, error) {
	shared, err := globalTracePool.GetOrCreateSharedTrace(filename, format)
	if err != nil {
		return nil, err
	}

	reader := &SharedTraceReader{
		cpuID:             cpuID,
		shared:            shared,
		readPosition:      0,
		instrCounter:      0,
		lastReportedBlock: 0,
		cachedBlocks:      make([]*TraceBlock, 0, LoadAhead+1),
		currentBlock:      nil,
		currentBlockIndex: 0,
		currentInstrIndex: 0,
		cachedStartOffset: 0,
		cachedEndOffset:   0,
	}

	shared.RegisterReader(reader)
	return reader, nil
}

// ReadInstruction 读取下一条指令
// 热路径：完全本地操作，无锁，无原子操作，无循环，无除法
func (r *SharedTraceReader) ReadInstruction() (*instruction.OOOModelInstr, error) {
	// === 超快速路径：直接从当前块读取 ===
	if r.currentBlock != nil && r.currentInstrIndex < len(r.currentBlock.instructions) {
		instr := r.currentBlock.instructions[r.currentInstrIndex]
		r.currentInstrIndex++
		r.readPosition++

		// 检查是否读完当前块（块边界检查）
		if r.currentInstrIndex >= len(r.currentBlock.instructions) {
			// 尝试切换到下一个块
			r.currentBlockIndex++
			if r.currentBlockIndex < len(r.cachedBlocks) {
				r.currentBlock = r.cachedBlocks[r.currentBlockIndex]
				r.currentInstrIndex = 0
			} else {
				r.currentBlock = nil // 需要refill
			}

			// 跨块边界，更新全局指针（每2万条指令一次）
			currentBlockID := r.readPosition / InstrsPerBlock
			if currentBlockID != r.lastReportedBlock {
				r.updateGlobalPointers(currentBlockID)
				r.lastReportedBlock = currentBlockID
			}
		}

		// 返回副本
		return r.copyInstruction(instr), nil
	}

	// === 冷路径：需要重新填充缓存 ===
	if err := r.refillCache(); err != nil {
		return nil, err
	}

	// 重试
	return r.ReadInstruction()
}

// updateGlobalPointers 更新全局最小/最大块指针
// 仅在跨块边界时调用（约每2万条指令一次）
func (r *SharedTraceReader) updateGlobalPointers(currentBlock uint64) {
	// 更新最小块ID（最慢的reader）
	for {
		oldMin := r.shared.minBlockID.Load()
		if currentBlock >= oldMin {
			break // 我们不是最慢的
		}
		if r.shared.minBlockID.CompareAndSwap(oldMin, currentBlock) {
			break
		}
	}

	// 更新最大块ID（最快的reader）
	for {
		oldMax := r.shared.maxBlockID.Load()
		if currentBlock <= oldMax {
			break // 我们不是最快的
		}
		if r.shared.maxBlockID.CompareAndSwap(oldMax, currentBlock) {
			break
		}
	}

	// 触发清理（如果最小块前进了很多）
	minBlock := r.shared.minBlockID.Load()
	if minBlock > 0 && len(r.shared.blocks) > MaxCachedBlocks/2 {
		go r.shared.cleanupOldBlocks()
	}
}

// refillCache 重新填充本地缓存
func (r *SharedTraceReader) refillCache() error {
	currentBlockID := r.readPosition / InstrsPerBlock
	endBlock := currentBlockID + LoadAhead

	// 检测预取miss：需要的块是否已加载
	prefetchMiss := false
	r.shared.blocksMu.Lock()
	nextAvailable := r.shared.nextBlock
	r.shared.blocksMu.Unlock()

	if currentBlockID >= nextAvailable {
		// 预取miss！需要的块还未加载
		prefetchMiss = true

		// 增加预取距离（自适应）
		oldDist := r.shared.prefetchDist.Load()
		newDist := oldDist + 1
		r.shared.prefetchDist.Store(newDist)
	}

	// 加载当前块及预加载块
	if err := r.shared.ensureBlocksLoaded(currentBlockID, endBlock); err != nil {
		if err == io.EOF {
			// 尝试加载现有的块
			if currentBlockID < r.shared.nextBlock {
				// 至少有当前块
			} else {
				return io.EOF
			}
		} else {
			return err
		}
	}

	_ = prefetchMiss // 标记使用（避免编译警告）

	// 更新本地缓存
	r.cachedBlocks = r.cachedBlocks[:0]
	r.cachedStartOffset = currentBlockID * InstrsPerBlock
	r.cachedEndOffset = r.cachedStartOffset

	for blockID := currentBlockID; blockID <= endBlock; blockID++ {
		block := r.shared.getBlock(blockID)
		if block == nil {
			break
		}
		r.cachedBlocks = append(r.cachedBlocks, block)
		r.cachedEndOffset = block.startOffset + uint64(len(block.instructions))
	}

	if len(r.cachedBlocks) == 0 {
		return io.EOF
	}

	// 设置当前块指针（优化热路径）
	r.currentBlock = r.cachedBlocks[0]
	r.currentBlockIndex = 0
	// 计算当前块内的索引
	r.currentInstrIndex = int(r.readPosition - r.currentBlock.startOffset)

	return nil
}

// copyInstruction 拷贝指令（浅拷贝，共享只读切片）
// trace数据字段（DestRegisters, SrcRegisters, DestMemory, SrcMemory）是只读的，
// 可以安全地在多个CPU之间共享，无需深拷贝
func (r *SharedTraceReader) copyInstruction(src *instruction.OOOModelInstr) *instruction.OOOModelInstr {
	// 浅拷贝：切片共享底层数组（只读数据，安全）
	dst := *src

	// 只设置CPU特定的字段
	// 其他字段（ReadyTime, BranchPrediction等）在src中已经是初始值（0/false）
	// 因为src是从trace加载的，从未被修改过
	dst.InstrID = r.instrCounter
	dst.CPUID = r.cpuID
	r.instrCounter++

	return &dst
}

// EOF 返回是否到达末尾
func (r *SharedTraceReader) EOF() bool {
	return r.readPosition >= r.cachedEndOffset && r.shared.eof
}

// Close 关闭reader
func (r *SharedTraceReader) Close() error {
	return globalTracePool.ReleaseSharedTrace(r.shared.filename, r)
}

// prefetchLoop 后台预取goroutine
// 持续预取 maxBlockID + prefetchDist 的块，避免CPU阻塞等待
func (s *SharedTraceData) prefetchLoop() {
	ticker := time.NewTicker(1 * time.Millisecond) // 高频检查
	defer ticker.Stop()

	for {
		select {
		case <-s.stopPrefetch:
			return

		case <-ticker.C:
			if s.eof {
				continue
			}

			// 计算需要预取的目标块ID
			maxBlock := s.maxBlockID.Load()
			prefetchDist := s.prefetchDist.Load()
			targetBlock := maxBlock + prefetchDist

			// 尝试预取
			s.blocksMu.Lock()
			currentNext := s.nextBlock
			if targetBlock >= currentNext && !s.eof {
				// 需要加载新块
				for blockID := currentNext; blockID <= targetBlock && !s.eof; blockID++ {
					if _, err := s.loadBlock(blockID); err != nil {
						if err == io.EOF {
							// 到达文件末尾
							break
						}
						// 其他错误，稍后重试
						break
					}
				}
			}
			s.blocksMu.Unlock()
		}
	}
}
