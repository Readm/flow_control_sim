package trace

import (
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Readm/flow_sim/internal/nodes/cpu/champsim/instruction"
)

// TraceReader 定义 trace 读取器接口
type TraceReader interface {
	// ReadInstruction 读取下一条指令
	// 返回 nil, io.EOF 表示 trace 结束
	ReadInstruction() (*instruction.OOOModelInstr, error)

	// EOF 返回是否已到达 trace 末尾
	EOF() bool

	// Close 关闭 trace 文件
	Close() error

	// Warmup 预热 trace reader，提前加载数据但不改变读取位置
	// 用于避免第一次读取时的延迟（例如解压缩）
	Warmup() error
}

// BulkTraceReader 批量 trace 读取器
//
// 使用批量读取和缓冲策略提升性能：
// - 一次读取 128 条指令到缓冲区
// - 当缓冲区剩余 <= 1 条时触发下一次读取
// - 自动设置分支目标地址
type BulkTraceReader struct {
	// 配置
	cpuID        uint8
	format       TraceFormat
	instrCounter uint64 // 全局指令计数器

	// 文件 I/O
	file      io.ReadCloser
	eof       bool
	errState  error

	// 缓冲区
	instrBuffer []*instruction.OOOModelInstr
	bufferSize  int // 批量读取大小
	refreshThreshold int // 触发刷新的阈值
}

const (
	// DefaultBufferSize 默认批量读取大小
	DefaultBufferSize = 128

	// DefaultRefreshThreshold 默认刷新阈值
	DefaultRefreshThreshold = 1
)

// NewTraceReader 创建 trace 读取器
//
// 支持的文件格式：
//   - .champsimtrace (未压缩)
//   - .champsimtrace.gz (gzip 压缩)
//   - .champsimtrace.xz (xz 压缩, 需要系统安装 xz)
//
// 参数：
//   - filename: trace 文件路径
//   - cpuID: CPU 核心 ID
//   - format: trace 格式 (FormatStandard 或 FormatCloudSuite)
func NewTraceReader(filename string, cpuID uint8, format TraceFormat) (TraceReader, error) {
	// 打开文件
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open trace file: %w", err)
	}

	// 根据扩展名检测压缩格式
	var reader io.ReadCloser
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".xz":
		// 使用 xz 命令解压
		reader, err = openXZFile(filename)
		if err != nil {
			f.Close()
			return nil, err
		}
		// 关闭原始文件，使用解压后的 reader
		f.Close()

	case ".gz":
		// 使用 gzip 库解压
		gzReader, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		reader = gzReader

	default:
		// 未压缩文件
		reader = f
	}

	return &BulkTraceReader{
		cpuID:            cpuID,
		format:           format,
		instrCounter:     0,
		file:             reader,
		eof:              false,
		errState:         nil,
		instrBuffer:      make([]*instruction.OOOModelInstr, 0, DefaultBufferSize),
		bufferSize:       DefaultBufferSize,
		refreshThreshold: DefaultRefreshThreshold,
	}, nil
}

// openXZFile 使用 xz 命令解压 .xz 文件
func openXZFile(filename string) (io.ReadCloser, error) {
	// 检查 xz 命令是否存在
	if _, err := exec.LookPath("xz"); err != nil {
		return nil, fmt.Errorf("xz command not found, please install xz-utils: %w", err)
	}

	// 启动 xz -dc 进程解压
	cmd := exec.Command("xz", "-dc", filename)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create xz pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start xz process: %w", err)
	}

	// 返回包装的 ReadCloser，确保关闭时终止 xz 进程
	return &xzReadCloser{
		reader: stdout,
		cmd:    cmd,
	}, nil
}

// xzReadCloser 包装 xz 进程的 stdout
type xzReadCloser struct {
	reader io.ReadCloser
	cmd    *exec.Cmd
}

func (xz *xzReadCloser) Read(p []byte) (n int, err error) {
	return xz.reader.Read(p)
}

func (xz *xzReadCloser) Close() error {
	xz.reader.Close()
	// 等待 xz 进程结束
	if err := xz.cmd.Wait(); err != nil {
		// xz 进程可能因为管道关闭而退出，忽略这类错误
		if _, ok := err.(*exec.ExitError); !ok {
			return err
		}
	}
	return nil
}

// ReadInstruction 读取下一条指令
func (r *BulkTraceReader) ReadInstruction() (*instruction.OOOModelInstr, error) {
	// 如果缓冲区需要刷新且未到达文件末尾，批量读取
	if len(r.instrBuffer) <= r.refreshThreshold && !r.eof {
		if err := r.refillBuffer(); err != nil && err != io.EOF {
			r.errState = err
			return nil, err
		}
		// 注意：即使遇到 EOF，缓冲区可能还有指令，继续处理
	}

	// 从缓冲区取出一条指令
	if len(r.instrBuffer) == 0 {
		// 缓冲区为空且已到达文件末尾
		return nil, io.EOF
	}

	instr := r.instrBuffer[0]
	r.instrBuffer = r.instrBuffer[1:]

	// 分配全局指令 ID
	instr.InstrID = r.instrCounter
	r.instrCounter++

	return instr, nil
}

// refillBuffer 批量读取指令填充缓冲区
func (r *BulkTraceReader) refillBuffer() error {
	if r.eof {
		return io.EOF
	}

	// 计算本次读取数量
	toRead := r.bufferSize - len(r.instrBuffer)
	if toRead <= 0 {
		return nil
	}

	// 根据格式选择读取方法
	var newInstrs []*instruction.OOOModelInstr
	var err error

	switch r.format {
	case FormatStandard:
		newInstrs, err = r.readBulkInputInstr(toRead)
	case FormatCloudSuite:
		newInstrs, err = r.readBulkCloudSuiteInstr(toRead)
	default:
		return fmt.Errorf("unknown trace format: %v", r.format)
	}

	if err != nil && err != io.EOF {
		return err
	}

	// 添加到缓冲区
	r.instrBuffer = append(r.instrBuffer, newInstrs...)

	// 设置分支目标
	setBranchTargets(r.instrBuffer)

	if err == io.EOF {
		r.eof = true
	}

	return nil
}

// readBulkInputInstr 批量读取 InputInstr 格式
func (r *BulkTraceReader) readBulkInputInstr(count int) ([]*instruction.OOOModelInstr, error) {
	instrs := make([]*instruction.OOOModelInstr, 0, count)

	for i := 0; i < count; i++ {
		var rawInstr InputInstr
		if err := binary.Read(r.file, binary.LittleEndian, &rawInstr); err != nil {
			if err == io.EOF && i > 0 {
				// 已读取部分指令，返回它们
				return instrs, io.EOF
			}
			return instrs, err
		}

		// 转换为 OOOModelInstr
		instr := instruction.NewOOOModelInstrFromInput(
			r.cpuID,
			rawInstr.IP,
			rawInstr.IsBranch,
			rawInstr.BranchTaken,
			rawInstr.DestRegisters[:],
			rawInstr.SrcRegisters[:],
			rawInstr.DestMemory[:],
			rawInstr.SrcMemory[:],
		)

		instrs = append(instrs, instr)
	}

	return instrs, nil
}

// readBulkCloudSuiteInstr 批量读取 CloudSuiteInstr 格式
func (r *BulkTraceReader) readBulkCloudSuiteInstr(count int) ([]*instruction.OOOModelInstr, error) {
	instrs := make([]*instruction.OOOModelInstr, 0, count)

	for i := 0; i < count; i++ {
		var rawInstr CloudSuiteInstr
		if err := binary.Read(r.file, binary.LittleEndian, &rawInstr); err != nil {
			if err == io.EOF && i > 0 {
				return instrs, io.EOF
			}
			return instrs, err
		}

		// 转换为 OOOModelInstr
		instr := instruction.NewOOOModelInstrFromCloudSuite(
			rawInstr.IP,
			rawInstr.IsBranch,
			rawInstr.BranchTaken,
			rawInstr.DestRegisters[:],
			rawInstr.SrcRegisters[:],
			rawInstr.DestMemory[:],
			rawInstr.SrcMemory[:],
			rawInstr.ASID,
		)

		instrs = append(instrs, instr)
	}

	return instrs, nil
}

// setBranchTargets 设置分支目标地址
//
// ChampSim 使用一个巧妙的算法：
// 反向遍历指令序列，每个分支的目标是下一条指令的 IP（如果跳转）
// 或者是紧接着的指令（如果不跳转）
//
// 算法：
//  1. 从后向前遍历
//  2. 对于每条分支指令：
//     - 如果 BranchTaken，目标是序列中下一条指令的 IP
//     - 否则，目标是当前 IP + 某个偏移（通常不设置）
func setBranchTargets(instrs []*instruction.OOOModelInstr) {
	if len(instrs) == 0 {
		return
	}

	// 从后向前遍历
	for i := len(instrs) - 2; i >= 0; i-- {
		current := instrs[i]
		next := instrs[i+1]

		if current.IsBranch && current.BranchTaken {
			// 分支跳转：目标是下一条指令的 IP
			current.BranchTarget = next.IP
		}
	}
}

// EOF 返回是否已到达 trace 末尾
func (r *BulkTraceReader) EOF() bool {
	return r.eof && len(r.instrBuffer) == 0
}

// Close 关闭 trace 文件
func (r *BulkTraceReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// Warmup 预热 trace reader，提前加载第一批数据
func (r *BulkTraceReader) Warmup() error {
	// 如果 buffer 已经有数据，说明已经预热过了
	if len(r.instrBuffer) > 0 {
		return nil
	}

	// 触发第一次 buffer 填充
	return r.refillBuffer()
}
