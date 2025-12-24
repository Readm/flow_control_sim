package cpu

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/Readm/flow_sim/internal/champsim/trace"
)

// createTestTraceFile 创建测试用的 trace 文件
func createTestTraceFile(t *testing.T, instrs []trace.InputInstr) string {
	tmpfile, err := os.CreateTemp("", "cpu_test_*.champsimtrace")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	for _, instr := range instrs {
		if err := binary.Write(tmpfile, binary.LittleEndian, &instr); err != nil {
			tmpfile.Close()
			os.Remove(tmpfile.Name())
			t.Fatalf("Failed to write instruction: %v", err)
		}
	}

	tmpfile.Close()
	return tmpfile.Name()
}

// deleteTestTraceFile 删除测试 trace 文件
func deleteTestTraceFile(t *testing.T, filename string) {
	if err := os.Remove(filename); err != nil {
		t.Logf("Warning: Failed to remove test file %s: %v", filename, err)
	}
}
