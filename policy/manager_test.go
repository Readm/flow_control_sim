package policy

import (
	"errors"
	"testing"

	"github.com/Readm/flow_sim/core"
)

type stubRouter struct {
	target int
	err    error
}

func (s *stubRouter) ResolveRoute(_ *core.Packet, _ int, _ int) (int, error) {
	return s.target, s.err
}

type stubFlow struct {
	err error
}

func (s *stubFlow) Check(_ *core.Packet, _ int, _ int) error {
	return s.err
}

type stubDomain struct {
	value string
}

func (s *stubDomain) DomainOf(_ uint64) string {
	return s.value
}

func TestDefaultManager(t *testing.T) {
	mgr := NewDefaultManager()

	p := &core.Packet{}
	target, err := mgr.ResolveRoute(p, 1, 5)
	if err != nil {
		t.Fatalf("ResolveRoute returned error: %v", err)
	}
	if target != 5 {
		t.Fatalf("expected default target 5, got %d", target)
	}

	if err := mgr.CheckFlowControl(p, 1, 5); err != nil {
		t.Fatalf("CheckFlowControl returned error: %v", err)
	}

	if domain := mgr.DomainOf(0x1234); domain != "" {
		t.Fatalf("expected empty domain, got %q", domain)
	}
}

func TestCustomManagerComposition(t *testing.T) {
	router := &stubRouter{target: 9}
	flow := &stubFlow{}
	domain := &stubDomain{value: "cluster-A"}

	base := NewDefaultManager()
	mgr := WithDomainMapper(WithFlowController(WithRouter(base, router), flow), domain)

	p := &core.Packet{}
	target, err := mgr.ResolveRoute(p, 1, 5)
	if err != nil {
		t.Fatalf("ResolveRoute returned error: %v", err)
	}
	if target != 9 {
		t.Fatalf("expected routed target 9, got %d", target)
	}

	if err := mgr.CheckFlowControl(p, 1, target); err != nil {
		t.Fatalf("CheckFlowControl returned error: %v", err)
	}

	if domainName := mgr.DomainOf(0x2000); domainName != "cluster-A" {
		t.Fatalf("expected domain cluster-A, got %q", domainName)
	}
}

func TestFlowControlError(t *testing.T) {
	router := &stubRouter{target: 9}
	flow := &stubFlow{err: errors.New("no credits")}

	base := NewDefaultManager()
	mgr := WithFlowController(WithRouter(base, router), flow)

	p := &core.Packet{}
	target, err := mgr.ResolveRoute(p, 1, 5)
	if err != nil {
		t.Fatalf("ResolveRoute returned error: %v", err)
	}

	err = mgr.CheckFlowControl(p, 1, target)
	if err == nil {
		t.Fatalf("expected flow control error")
	}
}
