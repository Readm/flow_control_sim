package loadbench

import (
	"fmt"
	"math/rand"
	"sync/atomic"

	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// BuildBidirectionalRing creates a bidirectional ring network with the specified number of nodes.
// It replicates the logic from BenchmarkBidirectionalRingCoreScaling.
func BuildBidirectionalRing(nodeCount int) (*network.Network, error) {
	const (
		linkLatency = 10
	)

	// Inject interval: every N/2 cycles
	injectInterval := nodeCount / 2
	if injectInterval < 1 {
		injectInterval = 1
	}

	// Spin wait parameters
	// We use hardcoded values similar to the benchmark for consistency
	// In a real app these might be configurable or calibrated
	// In a real app these might be configurable or calibrated
	// cyclesPerUS := 2.0 // Unused for fixed constants
	// Attempt simple calibration if possible, or just use constants
	// For stability in server mode, let's use fixed constants to avoid runtime jitter during build
	minSpinCycles := int(10)
	maxSpinCycles := int(40)

	net := network.New()
	nodeHandles := make([]*network.NodeHandle, nodeCount)
	var allOutputs [][]*queue.OutputQueue

	// Atomic counters for statistics (optional, currently not exposed API, but good for internal tracking)
	var injectedCount int64
	var receivedCount int64

	// Create nodes with 2 inputs and 2 outputs
	for i := 0; i < nodeCount; i++ {
		n := node.NewWorkerNode(i)
		// Input 0: from CCW neighbor (Clockwise link)
		// Input 1: from CW neighbor (Counter-Clockwise link)
		input0 := queue.NewInputQueue(64, 1) // From (i-1)
		input1 := queue.NewInputQueue(64, 1) // From (i+1)

		// Output 0: to CW neighbor (i+1)
		// Output 1: to CCW neighbor (i-1)
		output0 := queue.NewOutputQueue(64, 1)
		output1 := queue.NewOutputQueue(64, 1)

		if err := n.AddInputQueue(input0); err != nil {
			return nil, fmt.Errorf("Node%d AddInputQueue 0: %v", i, err)
		}
		if err := n.AddInputQueue(input1); err != nil {
			return nil, fmt.Errorf("Node%d AddInputQueue 1: %v", i, err)
		}
		if err := n.AddOutputQueue(output0); err != nil {
			return nil, fmt.Errorf("Node%d AddOutputQueue 0: %v", i, err)
		}
		if err := n.AddOutputQueue(output1); err != nil {
			return nil, fmt.Errorf("Node%d AddOutputQueue 1: %v", i, err)
		}

		nodeHandles[i] = &network.NodeHandle{
			Node:    n,
			Inputs:  []*queue.InputQueue{input0, input1},
			Outputs: []*queue.OutputQueue{output0, output1},
		}
		allOutputs = append(allOutputs, []*queue.OutputQueue{output0, output1})

		if err := net.AddNode(nodeHandles[i]); err != nil {
			return nil, fmt.Errorf("AddNode %d: %v", i, err)
		}
	}

	// Helper to calc shortest path direction
	// Returns: 0 for CW (via output 0), 1 for CCW (via output 1)
	getDirection := func(src, dst int) int {
		// Distance CW: (dst - src + N) % N
		// Distance CCW: (src - dst + N) % N
		cwDist := (dst - src + nodeCount) % nodeCount
		ccwDist := (src - dst + nodeCount) % nodeCount
		if cwDist <= ccwDist {
			return 0 // CW
		}
		return 1 // CCW
	}

	// Setup process hook for all nodes
	for i := 0; i < nodeCount; i++ {
		nodeIdx := i
		outputs := allOutputs[i]

		nodeHandles[i].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
			// Simulate processing load
			cycles := minSpinCycles
			if maxSpinCycles > minSpinCycles {
				cycles += rand.Intn(maxSpinCycles - minSpinCycles)
			}
			node.SpinWaitCycles(uint64(cycles))

			// 1. Process Inputs (Forwarding)
			for _, q := range inputs {
				for _, ref := range q {
					pkt := ref.Packet
					if pkt.TargetID == nodeIdx {
						// Reached destination: Consume
						atomic.AddInt64(&receivedCount, 1)
						ref.Queue.Free(ref.Slot)
					} else {
						// Forwarding
						dir := getDirection(nodeIdx, pkt.TargetID)
						outQ := outputs[dir]

						// Backpressure check: Try to inject
						if !outQ.IsFull() {
							if err := outQ.InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
								ref.Queue.Free(ref.Slot) // Successfully forwarded
							} else {
								// Error, don't free
							}
						} else {
							// Full, do nothing. Packet remains in input queue.
						}
					}
				}
			}

			// 2. Traffic Generation
			// Every N/2 cycles, try to inject ONE packet
			if cycle%uint64(injectInterval) == 0 {
				// Pick random target != self
				target := rand.Intn(nodeCount)
				if target == nodeIdx {
					target = (target + 1) % nodeCount
				}

				dir := getDirection(nodeIdx, target)
				outQ := outputs[dir]

				// Check if we can inject
				if !outQ.IsFull() {
					pkt := packet.Packet{
						SourceID: nodeIdx,
						TargetID: target,
						Payload:  fmt.Sprintf("bi-data-%d-%d", nodeIdx, cycle),
					}
					if err := outQ.InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
						atomic.AddInt64(&injectedCount, 1)
					}
				}
			}

			return nil
		})
	}

	// Connect nodes
	// Link 0: Node i Output 0 -> Node (i+1) Input 0 (CW)
	// Link 1: Node i Output 1 -> Node (i-1) Input 1 (CCW)
	for i := 0; i < nodeCount; i++ {
		cwNext := (i + 1) % nodeCount
		// Connect CW: i:Out0 -> cwNext:In0
		if _, err := net.Connect(i, 0, cwNext, 0, linkLatency, 1); err != nil {
			return nil, fmt.Errorf("Connect CW %d->%d: %v", i, cwNext, err)
		}

		ccwNext := (i - 1 + nodeCount) % nodeCount
		// Connect CCW: i:Out1 -> ccwNext:In1
		if _, err := net.Connect(i, 1, ccwNext, 1, linkLatency, 1); err != nil {
			return nil, fmt.Errorf("Connect CCW %d->%d: %v", i, ccwNext, err)
		}
	}

	return net, nil
}
