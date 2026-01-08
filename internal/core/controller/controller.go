package controller

import (
	"context"
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/builder"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/state"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
)

// SimulationController manages the network simulation.
type SimulationController struct {
	mu             sync.Mutex
	currentNetwork *network.Network
}

// New creates a new SimulationController.
func New() *SimulationController {
	return &SimulationController{}
}

// Rebuild creates a new network from the provided configuration.
// DEPRECATED: Use RebuildFromFlowSimNetwork instead
func (c *SimulationController) Rebuild(cfg config.EntityConfig) error {
	return fmt.Errorf("Rebuild() is deprecated, use RebuildFromFlowSimNetwork() instead")
}

// Run advances the simulation to the specified cycle.
func (c *SimulationController) Run(ctx context.Context, _ config.EntityConfig, targetCycle uint64) error {
	c.mu.Lock()
	net := c.currentNetwork
	c.mu.Unlock()

	if net == nil {
		return fmt.Errorf("no network loaded")
	}

	// TODO: Handle ctx for cancellation if AdvanceTo supports it
	// Assuming AdvanceTo is blocking and we want to allow GetState in parallel?
	// If we lock here, GetState will block.
	// For safety, we should validly allow concurrent read if components support it.
	// But let's lock for now to ensure consistency, acknowledging it blocks UI updates during simulation.
	// Actually, if we lock, we can't observe progress.
	// Let's NOT lock the whole duration, only the retrieval of the network object.
	// Note: internal/core/network implementation implies single-threaded Advance (or managed workers).
	// Concurrent ExportState might race.

	// For minimal prototype, we accept potential data race on viewing processing state,
	// or we assume users run small steps.

	return net.AdvanceTo(int(targetCycle))
}

// GetState returns the current state of the network.
func (c *SimulationController) GetState() *state.NetworkState {
	c.mu.Lock()
	net := c.currentNetwork
	c.mu.Unlock()

	if net == nil {
		return nil
	}

	// Export with summary level
	s := net.ExportState(state.ExportConfig{DetailLevel: state.DetailLevelSummary})
	return &s
}

// LoadPreset loads a predefined network configuration.
func (c *SimulationController) LoadPreset(name string, params map[string]int) error {
	// Minimal implementation: just support a "Ring" preset or similar if hardcoded helpers exist.
	// Or leave unimplemented for now as we focus on JSON build.
	return fmt.Errorf("presets not implemented in minimal controller")
}

// Subscribe is a mock stub for now, as real broadcasting isn't fully implemented in Network
func (c *SimulationController) Subscribe() <-chan state.NetworkState {
	// TODO: Implement real event bus
	return make(chan state.NetworkState)
}

// RebuildFromFlowSimNetwork 从 FlowSimNetwork 构建网络（新架构）
func (c *SimulationController) RebuildFromFlowSimNetwork(flowNet protocol.FlowSimNetwork) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	net, err := builder.BuildFromFlowSimNetwork(flowNet)
	if err != nil {
		return fmt.Errorf("failed to build network from FlowSimNetwork: %w", err)
	}

	c.currentNetwork = net
	return nil
}
