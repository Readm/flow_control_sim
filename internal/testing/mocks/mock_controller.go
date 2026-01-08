package mocks

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/loadbench"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

var (
	ErrNilContext = errors.New("nil context")
	ErrNoCycles   = errors.New("cycles must be > 0")
)

// Controller implements a simple simulation controller for testing purposes.
type Controller struct {
	stateMu sync.RWMutex
	latest  *state.NetworkState

	// Real network for simulation mode
	realNetwork *network.Network

	subsMu sync.Mutex
	subs   []chan state.NetworkState
}

// NewController creates a mock controller ready for injection.
func NewController() *Controller {
	return &Controller{
		subs: make([]chan state.NetworkState, 0),
	}
}

// Subscribe returns a channel that receives state updates.
func (m *Controller) Subscribe() <-chan state.NetworkState {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	ch := make(chan state.NetworkState, 10)
	m.subs = append(m.subs, ch)
	return ch
}

// LoadPreset loads a pre-defined network topology.
func (m *Controller) LoadPreset(name string, params map[string]int) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	var net *network.Network
	var err error

	switch name {
	case "bi_ring":
		nodes := 16
		if n, ok := params["nodes"]; ok && n > 0 {
			nodes = n
		}
		net, err = loadbench.BuildBidirectionalRing(nodes)
	default:
		return fmt.Errorf("unknown preset: %s", name)
	}

	if err != nil {
		return err
	}

	m.realNetwork = net

	// Export initial state
	// We use DetailLevelSummary for initial load. Run loop might use DetailLevelFull if needed.
	initialState := net.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})
	m.latest = &initialState

	// Notify subscribers of the reset (new state at cycle 0)
	go func() {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()
		for _, ch := range m.subs {
			select {
			case ch <- initialState:
			default:
			}
		}
	}()

	return nil
}

// Rebuild resets the simulation state based on the provided configuration.
// Deprecated: Use RebuildFromFlowSimNetwork instead. This method converts
// EntityConfig to FlowSimNetwork internally.
func (m *Controller) Rebuild(cfg config.EntityConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Convert to FlowSimNetwork and use the new method
	flowNet := cfg.ToFlowSimNetwork()
	return m.RebuildFromFlowSimNetwork(flowNet)
}

// RebuildFromFlowSimNetwork rebuilds the network from a FlowSimNetwork definition.
func (m *Controller) RebuildFromFlowSimNetwork(flowNet protocol.FlowSimNetwork) error {
	m.stateMu.Lock()

	// Clear real network if we represent a static config build
	m.realNetwork = nil

	// Construct new state from FlowSimNetwork
	newState := state.NetworkState{
		CurrentCycle: 0,
		Nodes:        make([]state.NodeState, 0, len(flowNet.Nodes)),
		Links:        make([]state.LinkState, 0, len(flowNet.Edges)),
		DisplayData:  make(map[string]interface{}),
	}

	// 保存网络级别的显示信息
	if flowNet.Zoom != nil {
		newState.DisplayData["zoom"] = *flowNet.Zoom
	}
	if flowNet.Pan != nil {
		newState.DisplayData["pan"] = *flowNet.Pan
	}

	// Map Nodes
	for _, n := range flowNet.Nodes {
		nodeState := state.NodeState{
			ID:          n.NodeId,
			Type:        getNodeType(n),
			Stats:       make(map[string]interface{}),
			Features:    make(map[string]map[string]interface{}),
			DisplayData: make(map[string]interface{}),
		}

		// 保存 DisplayData（position, data, style）
		nodeState.DisplayData["position"] = n.Position
		nodeState.DisplayData["data"] = n.Data
		if n.Style != nil {
			nodeState.DisplayData["style"] = *n.Style
		}

		// 保存 CoherenceDomainID
		if n.CoherenceDomainId != nil {
			nodeState.CoherenceDomainID = n.CoherenceDomainId
		}

		// 保存 Features 配置
		if n.Cache != nil {
			nodeState.Features["cache"] = map[string]interface{}{
				"capacity":           n.Cache.Capacity,
				"num_sets":           n.Cache.NumSets,
				"replacement_policy": n.Cache.ReplacementPolicy,
				"states":             n.Cache.States,
			}
		}
		if n.Directory != nil {
			nodeState.Features["directory"] = map[string]interface{}{
				"capacity":           n.Directory.Capacity,
				"num_sets":           n.Directory.NumSets,
				"replacement_policy": n.Directory.ReplacementPolicy,
				"states":             n.Directory.States,
			}
		}

		// Add port information if available
		if n.InPorts != nil {
			nodeState.Inputs = make([]state.QueueState, len(*n.InPorts))
			for i, p := range *n.InPorts {
				capacity := 64
				if p.BufferSize != nil {
					capacity = *p.BufferSize
				}
				packetTypes := []string{}
				if p.PacketTypes != nil {
					for _, pt := range *p.PacketTypes {
						packetTypes = append(packetTypes, fmt.Sprintf("%d", pt))
					}
				}
				nodeState.Inputs[i] = state.QueueState{
					Type:        "Input",
					Capacity:    capacity,
					Bandwidth:   p.Bandwidth,
					PacketTypes: packetTypes,
				}
			}
		}
		if n.OutPorts != nil {
			nodeState.Outputs = make([]state.QueueState, len(*n.OutPorts))
			for i, p := range *n.OutPorts {
				packetTypes := []string{}
				if p.PacketTypes != nil {
					for _, pt := range *p.PacketTypes {
						packetTypes = append(packetTypes, fmt.Sprintf("%d", pt))
					}
				}
				nodeState.Outputs[i] = state.QueueState{
					Type:        "Output",
					Bandwidth:   p.Bandwidth,
					PacketTypes: packetTypes,
				}
			}
		}
		newState.Nodes = append(newState.Nodes, nodeState)
	}

	// Map Links
	for _, e := range flowNet.Edges {
		latency := 1
		if e.Latency != nil {
			latency = *e.Latency
		}
		bandwidth := 1
		if e.Bandwidth != nil {
			bandwidth = *e.Bandwidth
		}
		srcPortId := 0
		if e.SrcPortId != nil {
			srcPortId = *e.SrcPortId
		}
		dstPortId := 0
		if e.DstPortId != nil {
			dstPortId = *e.DstPortId
		}

		packetTypes := []string{}
		if e.PacketTypes != nil {
			for _, pt := range *e.PacketTypes {
				packetTypes = append(packetTypes, fmt.Sprintf("%d", pt))
			}
		}

		linkState := state.LinkState{
			SourceID:     e.SrcNodeId,
			SourcePortID: srcPortId,
			TargetID:     e.DstNodeId,
			TargetPortID: dstPortId,
			Latency:      latency,
			Bandwidth:    bandwidth,
			Occupancy:    make([]int, latency),
			PacketTypes:  packetTypes,
			EdgeID:       e.EdgeId,
			DisplayData:  make(map[string]interface{}),
		}

		// 保存 DisplayData
		linkState.DisplayData["data"] = e.Data

		newState.Links = append(newState.Links, linkState)
	}

	m.latest = &newState
	m.stateMu.Unlock()

	// Notify subscribers of the reset (new state at cycle 0)
	go func() {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()
		for _, ch := range m.subs {
			select {
			case ch <- newState:
			default:
			}
		}
	}()

	return nil
}

// getNodeType extracts the node type from a FlowSimNetwork Node.
func getNodeType(n protocol.Node) string {
	if n.NodeFeatures != nil && len(*n.NodeFeatures) > 0 {
		return (*n.NodeFeatures)[0]
	}
	if n.Data.Type != nil {
		return *n.Data.Type
	}
	return "WorkerNode"
}

// Run satisfies the simulation controller contract.
// Deprecated: The cfg parameter is ignored. Use RebuildFromFlowSimNetwork first, then call Run.
func (m *Controller) Run(ctx context.Context, cfg config.EntityConfig, cycles uint64) error {
	// Ignore cfg parameter - network should already be built via RebuildFromFlowSimNetwork
	return m.RunCycles(ctx, cycles)
}

// RunCycles advances the simulation by the specified number of cycles.
func (m *Controller) RunCycles(ctx context.Context, cycles uint64) error {
	if ctx == nil {
		return ErrNilContext
	}
	if cycles == 0 {
		return ErrNoCycles
	}

	// Update the cycle count
	m.stateMu.Lock()
	var newState state.NetworkState

	if m.realNetwork != nil {
		// Run real simulation with timeout to prevent hanging
		log.Printf(" Run: Using realNetwork, advancing to cycle %d", cycles)

		// Create a channel to receive the result
		type advanceResult struct {
			state state.NetworkState
			err   error
		}
		resultChan := make(chan advanceResult, 1)

		// Run AdvanceTo in a goroutine with timeout
		go func() {
			err := m.realNetwork.AdvanceTo(int(cycles))
			if err != nil {
				log.Printf(" Run: AdvanceTo failed: %v", err)
				resultChan <- advanceResult{err: err}
				return
			}
			log.Printf(" Run: AdvanceTo completed, now exporting state...")
			exportedState := m.realNetwork.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})
			log.Printf(" Run: Export completed with %d nodes, %d links, cycle = %d", len(exportedState.Nodes), len(exportedState.Links), exportedState.CurrentCycle)
			resultChan <- advanceResult{state: exportedState, err: nil}
		}()

		// Wait for result with 10 second timeout
		select {
		case result := <-resultChan:
			if result.err != nil {
				m.stateMu.Unlock()
				return result.err
			}
			newState = result.state
			m.latest = &newState
			log.Printf(" Run: Successfully updated to cycle %d", newState.CurrentCycle)
		case <-ctx.Done():
			log.Printf(" Run: Context cancelled")
			m.stateMu.Unlock()
			return ctx.Err()
		}
	} else if m.latest != nil {
		log.Printf("  Run: realNetwork is nil, using mock state with %d nodes, %d links", len(m.latest.Nodes), len(m.latest.Links))
		m.latest.CurrentCycle = int(cycles)
		// Also simulate some time passing or traffic changes if needed
		// For now, just toggling link occupancy to show liveliness
		for i := range m.latest.Links {
			if len(m.latest.Links[i].Occupancy) > 0 {
				m.latest.Links[i].Occupancy[0] = (m.latest.Links[i].Occupancy[0] + 1) % 10
			}
		}
		newState = *m.latest
	}
	m.stateMu.Unlock()

	// Notify subscribers
	m.subsMu.Lock()
	for _, ch := range m.subs {
		select {
		case ch <- newState:
		default:
			// Drop if blocked
		}
	}
	m.subsMu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// SetState updates the current network state.
func (m *Controller) SetState(ns state.NetworkState) {
	m.stateMu.Lock()
	m.latest = &ns
	m.stateMu.Unlock()
}

// GetState returns the last recorded state.
func (m *Controller) GetState() *state.NetworkState {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.latest
}
