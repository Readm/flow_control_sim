package trace

import (
	"io"

	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction"
)

// BufferedTraceReader 大缓冲区trace读取器
// 预加载大量指令到内存，减少实时I/O和解压开销
type BufferedTraceReader struct {
	cpuID        uint8
	instrCounter uint64
	
	// 底层reader（用于补充缓冲区）
	underlying TraceReader
	
	// 大缓冲区
	instrBuffer []*instruction.OOOModelInstr
	bufferIndex int // 当前读取位置
	
	// 预加载配置
	preloadSize int // 预加载大小（默认100000条）
	refillSize  int // 补充大小（默认50000条）
	
	eof bool
}

// NewBufferedTraceReader 创建大缓冲区trace读取器
// preloadSize: 初始预加载指令数（默认100000）
// refillSize: 缓冲区低于此值时补充（默认50000）
func NewBufferedTraceReader(filename string, cpuID uint8, format TraceFormat, preloadSize, refillSize int) (TraceReader, error) {
	if preloadSize <= 0 {
		preloadSize = 100000 // 默认10万条
	}
	if refillSize <= 0 {
		refillSize = preloadSize / 2 // 默认一半
	}
	
	// 创建底层reader
	underlying, err := NewTraceReader(filename, cpuID, format)
	if err != nil {
		return nil, err
	}
	
	reader := &BufferedTraceReader{
		cpuID:       cpuID,
		underlying:  underlying,
		instrBuffer: make([]*instruction.OOOModelInstr, 0, preloadSize),
		bufferIndex: 0,
		preloadSize: preloadSize,
		refillSize:  refillSize,
		eof:         false,
	}

	// 不进行初始预加载，改为懒加载（第一次读取时才加载）
	// 这样可以避免初始化时的巨大开销

	return reader, nil
}

// preload 预加载指令到缓冲区
func (r *BufferedTraceReader) preload() error {
	if r.eof {
		return io.EOF
	}
	
	// 计算需要加载的数量
	remaining := len(r.instrBuffer) - r.bufferIndex
	toLoad := r.preloadSize - remaining
	
	if toLoad <= 0 {
		return nil
	}
	
	// 从底层reader读取
	loaded := 0
	for loaded < toLoad {
		instr, err := r.underlying.ReadInstruction()
		if err == io.EOF {
			r.eof = true
			break
		}
		if err != nil {
			return err
		}
		
		r.instrBuffer = append(r.instrBuffer, instr)
		loaded++
	}
	
	return nil
}

// ReadInstruction 读取下一条指令
func (r *BufferedTraceReader) ReadInstruction() (*instruction.OOOModelInstr, error) {
	// 检查是否需要加载缓冲区
	remaining := len(r.instrBuffer) - r.bufferIndex

	// 如果缓冲区为空（首次读取）或剩余少于refillSize，则补充
	if remaining == 0 || (remaining < r.refillSize && !r.eof) {
		if err := r.refill(); err != nil && err != io.EOF {
			return nil, err
		}
	}

	// 从缓冲区读取
	if r.bufferIndex >= len(r.instrBuffer) {
		return nil, io.EOF
	}

	instr := r.instrBuffer[r.bufferIndex]
	r.bufferIndex++

	// 重新分配指令ID（保持连续性）
	instr.InstrID = r.instrCounter
	r.instrCounter++

	return instr, nil
}

// refill 补充缓冲区
func (r *BufferedTraceReader) refill() error {
	if r.eof {
		return io.EOF
	}
	
	// 清理已读取的指令（节省内存）
	if r.bufferIndex > 0 {
		// 保留未读部分
		r.instrBuffer = r.instrBuffer[r.bufferIndex:]
		r.bufferIndex = 0
	}
	
	// 预加载更多指令
	return r.preload()
}

// EOF 返回是否已到达trace末尾
func (r *BufferedTraceReader) EOF() bool {
	return r.bufferIndex >= len(r.instrBuffer) && r.eof
}

// Close 关闭reader
func (r *BufferedTraceReader) Close() error {
	if r.underlying != nil {
		return r.underlying.Close()
	}
	return nil
}

// Warmup 预热 trace reader，代理到底层 reader
func (r *BufferedTraceReader) Warmup() error {
	if r.underlying != nil {
		return r.underlying.Warmup()
	}
	return nil
}
