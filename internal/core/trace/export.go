//go:build trace

package trace

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TraceOutput Chrome trace 输出格式
type TraceOutput struct {
	// Trace 事件列表
	TraceEvents []TraceEvent `json:"traceEvents"`

	// 显示时间单位（虽然我们用 cycle，但告诉 Chrome 是纳秒）
	DisplayTimeUnit string `json:"displayTimeUnit"`

	// 其他元数据
	OtherData map[string]interface{} `json:"otherData,omitempty"`
}

// Export 导出 trace 到 JSON 文件
// 支持自动 gzip 压缩（文件名以 .gz 结尾）
func (tr *TraceRecorder) Export(filename string) error {
	events := tr.GetEvents()

	output := TraceOutput{
		TraceEvents:     events,
		DisplayTimeUnit: "ns", // Chrome 会按纳秒显示
		OtherData: map[string]interface{}{
			"version":     "flow_sim v1.0",
			"event_count": len(events),
			"config": map[string]interface{}{
				"max_cycles":      tr.config.MaxCycles,
				"sample_rate":     tr.config.SampleRate,
				"min_duration":    tr.config.MinDuration,
				"block_threshold": tr.config.BlockThreshold,
			},
		},
	}

	// 创建目录（如果不存在）
	dir := filepath.Dir(filename)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// 检查是否需要 gzip 压缩
	useGzip := strings.HasSuffix(filename, ".gz")

	// 创建文件
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// 编码 JSON
	var encoder *json.Encoder
	if useGzip {
		// 使用 gzip 压缩
		gzWriter := gzip.NewWriter(file)
		defer gzWriter.Close()
		encoder = json.NewEncoder(gzWriter)
	} else {
		encoder = json.NewEncoder(file)
	}

	// 格式化输出（方便调试）
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return nil
}

// ExportWithMetadata 导出 trace 并添加进程/线程元数据
// nodeNames: map[nodeID]nodeName
// threadNames: map[tid]threadName
func (tr *TraceRecorder) ExportWithMetadata(
	filename string,
	nodeNames map[int]string,
	threadNames map[int]string,
) error {
	// 添加进程名称元数据
	for nodeID, name := range nodeNames {
		tr.RecordMetadata("process_name", nodeID, 0, map[string]interface{}{
			"name": name,
		})
	}

	// 添加线程名称元数据
	for tid, name := range threadNames {
		// 为每个节点添加线程名称
		for nodeID := range nodeNames {
			tr.RecordMetadata("thread_name", nodeID, tid, map[string]interface{}{
				"name": name,
			})
		}
	}

	return tr.Export(filename)
}

// AddStatistics 添加统计信息到 OtherData
func (tr *TraceRecorder) AddStatistics(stats map[string]interface{}) {
	// 这个方法需要在 Export 之前调用
	// 但由于我们在 Export 中直接构建 TraceOutput，这里暂时不实现
	// 可以作为未来扩展
}
