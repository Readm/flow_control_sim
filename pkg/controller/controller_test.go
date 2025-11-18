package controller_test

import (
	"context"
	"errors"
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

func TestControllerStartAndStop(t *testing.T) {
	builder := newTestBuilder(t, 2*time.Millisecond)
	ctrl := controller.New(builder)

	cfg := config.EntityConfig{
		Nodes: []config.NodeConfig{
			{ID: 1},
		},
		Link: config.LinkConfig{},
	}

	startCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := ctrl.Start(startCtx, cfg, 100); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	waitForState(t, ctrl, controller.StatusRunning)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()

	if err := ctrl.Stop(stopCtx); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	if got := ctrl.State(); got != controller.StatusStopped {
		t.Fatalf("expected state stopped, got %v", got)
	}
}

func TestControllerRejectsConcurrentStart(t *testing.T) {
	builder := newTestBuilder(t, time.Millisecond)
	ctrl := controller.New(builder)

	cfg := config.EntityConfig{
		Nodes: []config.NodeConfig{
			{ID: 1},
		},
		Link: config.LinkConfig{},
	}

	if err := ctrl.Start(context.Background(), cfg, 50); err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	if err := ctrl.Start(context.Background(), cfg, 50); !errors.Is(err, controller.ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ctrl.Stop(ctx); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestControllerStopWithoutRun(t *testing.T) {
	ctrl := controller.New(newTestBuilder(t, time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := ctrl.Stop(ctx); !errors.Is(err, controller.ErrNotRunning) {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
}

func waitForState(t *testing.T, ctrl controller.SimulationController, expected controller.Status) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if ctrl.State() == expected {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("state did not reach %v within timeout (current %v)", expected, ctrl.State())
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

		return mgr, 10, nil
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
