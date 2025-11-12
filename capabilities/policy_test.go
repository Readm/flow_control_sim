package capabilities

import (
	"errors"
	"testing"

	"flow_sim/core"
	"flow_sim/hooks"
	"flow_sim/policy"
)

type stubPolicy struct {
	resolve func(packet *core.Packet, sourceID int, defaultTarget int) (int, error)
	flow    func(packet *core.Packet, sourceID int, targetID int) error
}

var _ policy.Manager = (*stubPolicy)(nil)

func (s *stubPolicy) ResolveRoute(packet *core.Packet, sourceID int, defaultTarget int) (int, error) {
	if s.resolve == nil {
		return defaultTarget, nil
	}
	return s.resolve(packet, sourceID, defaultTarget)
}

func (s *stubPolicy) CheckFlowControl(packet *core.Packet, sourceID int, targetID int) error {
	if s.flow == nil {
		return nil
	}
	return s.flow(packet, sourceID, targetID)
}

func (s *stubPolicy) DomainOf(addr uint64) string {
	return ""
}

func TestRoutingCapabilityResolvesTarget(t *testing.T) {
	broker := hooks.NewPluginBroker()
	policy := &stubPolicy{
		resolve: func(packet *core.Packet, sourceID int, defaultTarget int) (int, error) {
			return defaultTarget + 10, nil
		},
	}
	cap := NewRoutingCapability("test-routing", policy)
	if err := cap.Register(broker); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	ctx := &hooks.RouteContext{
		Packet:        &core.Packet{},
		SourceNodeID:  1,
		DefaultTarget: 5,
		TargetID:      5,
	}
	if err := broker.EmitAfterRoute(ctx); err != nil {
		t.Fatalf("EmitAfterRoute returned error: %v", err)
	}
	if ctx.TargetID != 15 {
		t.Fatalf("expected target 15, got %d", ctx.TargetID)
	}
}

func TestFlowCapabilityBlocksPacket(t *testing.T) {
	broker := hooks.NewPluginBroker()
	policy := &stubPolicy{
		flow: func(packet *core.Packet, sourceID int, targetID int) error {
			return errors.New("blocked")
		},
	}

	cap := NewFlowControlCapability("test-flow", policy)
	if err := cap.Register(broker); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	ctx := &hooks.MessageContext{
		Packet:   &core.Packet{},
		NodeID:   2,
		TargetID: 7,
	}
	err := broker.EmitBeforeSend(ctx)
	if err == nil {
		t.Fatalf("expected flow control error")
	}
}
