package builder

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/directory"
	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
)

// Build creates a new network from the provided configuration.
func Build(cfg config.EntityConfig) (*network.Network, error) {
	net := network.New()

	// 1. Create Nodes
	for _, nCfg := range cfg.Nodes {
		var newNode node.Node

		// Specialized Node Types via switch
		// For now we use WorkerNode as the generic base, but in future we should support others
		switch nCfg.Type {
		case "CentralSwitch":
			// TODO: Add support for CentralSwitch if needed, for now treat as WorkerNode
			newNode = node.NewWorkerNode(nCfg.ID)
		default:
			newNode = node.NewWorkerNode(nCfg.ID)
		}

		// Attach Components
		if nCfg.Cache.Capacity > 0 {
			// Only FullyAssociativeCache supported by this simple builder for now
			c := cache.NewFullyAssociativeCache(nCfg.Cache.Capacity)
			newNode.AddCache(c)
		}
		if nCfg.Directory.Capacity > 0 {
			d := directory.NewFullyAssociativeDirectory(nCfg.Directory.Capacity)
			newNode.AddDirectory(d)
		}

		// Create default queues (if ports not specified, use default 1 input 1 output)
		// This is critical for compatibility with simple JSONs
		if len(nCfg.InPorts) == 0 && len(nCfg.OutPorts) == 0 {
			// Minimal fallback: 8 inputs, 8 outputs (safe default)
			for i := 0; i < 8; i++ {
				newNode.AddInputQueue(queue.NewInputQueue(128, 1))
				newNode.AddOutputQueue(queue.NewOutputQueue(128, 1))
			}
		} else {
			// Use configured ports
			for _, p := range nCfg.InPorts {
				newNode.AddInputQueue(queue.NewInputQueue(p.BufferSize, p.InBandwidth))
			}
			for _, p := range nCfg.OutPorts {
				newNode.AddOutputQueue(queue.NewOutputQueue(p.BufferSize, p.OutBandwidth))
			}
		}

		// Add to network
		// We need to create a NodeHandle. We have to retrieve the queues back from the node.
		// node.Node doesn't expose queues easily directly without casting?
		// Actually network.NodeHandle requires explicit slice of queues.
		// Let's assume the node stores them in order.

		// HACK: Since we just added them, we can't easily get them back cleanly without accessors.
		// But network.AddNode takes handle which contains the queues.
		// For this minimal implementation, let's just make new slices and rely on the fact that
		// NewWorkerNode stores them. Wait, Node interface matches.
		// Let's check NodeHandle definition in network.go
		/*
			type NodeHandle struct {
				Node    node.Node
				Inputs  []*queue.InputQueue
				Outputs []*queue.OutputQueue
			}
		*/

		// Since we don't have easy getters on the interface `node.Node`, we should have kept references
		// when we created them.

		// Re-doing queue creation to keep references
		var inputs []*queue.InputQueue
		var outputs []*queue.OutputQueue

		// Use configured ports
		if len(nCfg.InPorts) == 0 && len(nCfg.OutPorts) == 0 {
			for i := 0; i < 8; i++ {
				q := queue.NewInputQueue(128, 1)
				inputs = append(inputs, q)
				newNode.AddInputQueue(q) // Ignore error
			}
			for i := 0; i < 8; i++ {
				q := queue.NewOutputQueue(128, 1)
				outputs = append(outputs, q)
				newNode.AddOutputQueue(q) // Ignore error
			}
		} else {
			for _, p := range nCfg.InPorts {
				q := queue.NewInputQueue(p.BufferSize, p.InBandwidth)
				inputs = append(inputs, q)
				newNode.AddInputQueue(q)
			}
			for _, p := range nCfg.OutPorts {
				q := queue.NewOutputQueue(p.BufferSize, p.OutBandwidth)
				outputs = append(outputs, q)
				newNode.AddOutputQueue(q)
			}
		}

		handle := &network.NodeHandle{
			Node:    newNode,
			Inputs:  inputs,
			Outputs: outputs,
		}

		if err := net.AddNode(handle); err != nil {
			return nil, fmt.Errorf("failed to add node %d: %w", nCfg.ID, err)
		}
	}

	// 2. Create Links
	for _, eCfg := range cfg.Edges {
		// Use default latency/bandwidth if not in config
		latency := 1
		bandwidth := 1

		// We default to port 0 if not specified (EdgeConfig is simple src/dst)
		// But complex topology needs ports.
		// Current simple EdgeConfig in entity.go only has Src and Dst.
		// That's a limitation. CyEdge has ports. visualization/builder maps CyEdge fields to... checks builder.go
		// builder.go: Src: e.SrcNodeID, Dst: e.DstNodeID. It ignores ports!
		// We need to fix builder.go to map ports if EntityConfig supports it.
		// But EntityConfig EdgeConfig currently doesn't supported ports.
		// I recall seeing EdgeConfig in Step 38. It only has Src and Dst.

		// Minimal fix: Assume port 0 for now or find free port?
		// For a minimal prototype, let's assume Port 0 -> Port 0.
		// Or, strictly speaking, we should have updated EdgeConfig to support ports.

		// Let's assume we map port 0 for now.
		if _, err := net.Connect(eCfg.Src, eCfg.SrcPort, eCfg.Dst, eCfg.DstPort, latency, bandwidth); err != nil {
			// Try next port? No, too complex.
			return nil, fmt.Errorf("failed to connect %d:%d->%d:%d: %w", eCfg.Src, eCfg.SrcPort, eCfg.Dst, eCfg.DstPort, err)
		}
	}

	return net, nil
}
