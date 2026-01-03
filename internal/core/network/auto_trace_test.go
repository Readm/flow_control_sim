//go:build trace

package network

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/trace"
)

// SimpleNode for testing
type SimpleNode struct {
	*node.BaseNode
}

func (n *SimpleNode) Process() {} // No-op

func NewSimpleNode(id int) *SimpleNode {
	return &SimpleNode{
		BaseNode: node.NewBaseNode(id, nil),
	}
}

func TestConcurrentTracePerformance(t *testing.T) {
	// 1. Setup
	traceFile := "/tmp/flow_sim_concurrent_trace.json"
	_ = os.Remove(traceFile)
	defer os.Remove(traceFile)

	// Simulate flag setting
	flag.Set("flow_trace", traceFile)

	// 2. Initialize Network
	// network.New() should pick up the flag (via GetGlobalTracer) and inject tracer
	net := New()
	tracer := trace.GetGlobalTracer()
	if tracer == nil {
		t.Fatal("Global tracer should be initialized")
	}

	// 3. Create Nodes (Simulate 64 Cores)
	nodeCount := 64
	for i := 0; i < nodeCount; i++ {
		n := NewSimpleNode(i)
		n.SetName(fmt.Sprintf("Worker-%d", i))

		// Wrap in NodeHandle
		handle := &NodeHandle{
			Node:    n,
			Inputs:  nil,
			Outputs: nil,
		}

		net.AddNode(handle) // This should propagate tracer to node
	}

	// Verify tracer injection
	for i := 0; i < nodeCount; i++ {
		// Access private map directly (allowed in same package)
		h := net.nodes[i]
		bn := h.Node.(*SimpleNode).BaseNode
		if bn.GetTracer() == nil {
			t.Fatalf("Node %d tracer not injected", i)
		}
	}

	// 4. Verify Flush capability and Metadata generation
	// Even without simulation events, FlushGlobal should gather Metadata from registered sources (Nodes)

	err := trace.FlushGlobal()
	if err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Check file content
	content, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("Read trace file failed: %v", err)
	}

	t.Logf("Trace file size: %d bytes", len(content))
	if len(content) < 10 {
		t.Fatalf("Trace file too small: %s", string(content))
	}
}
