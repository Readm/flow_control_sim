package controller_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/config"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/dataflow/flow"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
	"github.com/Readm/flow_sim/pkg/controller"
)

func TestControllerRunEmitsFrames(t *testing.T) {
	builder := newTestBuilder(t, 2*time.Millisecond)
	ctrl := controller.New(builder)

	cfg := config.EntityConfig{
		Nodes: []config.NodeConfig{
			{ID: 1},
		},
		Link: config.LinkConfig{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := ctrl.Run(ctx, cfg, 10); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if ctrl.LatestFrame() == nil {
		t.Fatalf("expected latest frame to be populated")
	}
}

func TestControllerRunRespectsContext(t *testing.T) {
	builder := newTestBuilder(t, 5*time.Millisecond)
	ctrl := controller.New(builder)

	cfg := config.EntityConfig{
		Nodes: []config.NodeConfig{
			{ID: 1},
		},
		Link: config.LinkConfig{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ctrl.Run(ctx, cfg, 5); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestControllerRunRequiresCycles(t *testing.T) {
	ctrl := controller.New(func(cfg config.EntityConfig) (*network.Manager, uint64, error) {
		f := flow.NewFIFO(1, 2)
		n := &mockNode{id: 1, delay: time.Millisecond, flow: f}
		mgr, err := network.NewManager([]node.Node{n}, map[int][]*link.Link{
			n.ID(): nil,
		})
		return mgr, 0, err
	})

	cfg := config.EntityConfig{
		Nodes: []config.NodeConfig{
			{ID: 1},
		},
		Link: config.LinkConfig{},
	}

	if err := ctrl.Run(context.Background(), cfg, 0); err != controller.ErrNoCycles {
		t.Fatalf("expected ErrNoCycles, got %v", err)
	}
}

func newTestBuilder(t *testing.T, tickDelay time.Duration) controller.ManagerBuilder {
	t.Helper()
	return func(cfg config.EntityConfig) (*network.Manager, uint64, error) {
		nodes := make([]node.Node, 0, len(cfg.Nodes))
		edges := make(map[int][]*link.Link)

		for _, nodeCfg := range cfg.Nodes {
			f := flow.NewFIFO(nodeCfg.ID, 8)
			n := &mockNode{
				id:        nodeCfg.ID,
				delay:     tickDelay,
				flow:      f,
				processed: 0,
			}
			nodes = append(nodes, n)
			edges[n.ID()] = []*link.Link{
				link.NewLink(n.ID(), n.Flow(), 1, 0),
			}
		}

		mgr, err := network.NewManager(nodes, edges)
		if err != nil {
			return nil, 0, err
		}

		return mgr, 0, nil
	}
}

type mockNode struct {
	id        int
	delay     time.Duration
	flow      flow.Flow
	processed int
}

func (m *mockNode) ID() int {
	return m.id
}

func (m *mockNode) Flow() flow.Flow {
	return m.flow
}

func (m *mockNode) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	if err := m.flow.Tick(ctx, cycle); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(m.delay):
	}

	m.flow.Emit(packet.Packet{
		SourceID: m.id,
		TargetID: m.id,
		Payload:  fmt.Sprintf("cycle-%d", cycle),
	})
	m.processed++
	return nil
}
