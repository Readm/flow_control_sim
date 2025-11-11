package policy

import "flow_sim/core"

// Manager coordinates routing, flow control, and domain lookups.
type Manager interface {
	ResolveRoute(packet *core.Packet, sourceID int, defaultTarget int) (int, error)
	CheckFlowControl(packet *core.Packet, sourceID int, targetID int) error
	DomainOf(addr uint64) string
}

// Router decides next-hop targets.
type Router interface {
	ResolveRoute(packet *core.Packet, sourceID int, defaultTarget int) (int, error)
}

// FlowController validates whether a packet can be sent.
type FlowController interface {
	Check(packet *core.Packet, sourceID int, targetID int) error
}

// DomainMapper returns domain metadata for an address.
type DomainMapper interface {
	DomainOf(addr uint64) string
}

type manager struct {
	router Router
	flow   FlowController
	domain DomainMapper
}

// NewDefaultManager creates a manager with permissive defaults.
func NewDefaultManager() Manager {
	return &manager{
		router: &defaultRouter{},
		flow:   &noopFlowController{},
		domain: &defaultDomainMapper{},
	}
}

// WithRouter returns a copy of the manager using the provided router.
func WithRouter(m Manager, r Router) Manager {
	base := asManager(m)
	base.router = r
	return base
}

// WithFlowController returns a copy using the provided flow controller.
func WithFlowController(m Manager, fc FlowController) Manager {
	base := asManager(m)
	base.flow = fc
	return base
}

// WithDomainMapper returns a copy using the provided domain mapper.
func WithDomainMapper(m Manager, dm DomainMapper) Manager {
	base := asManager(m)
	base.domain = dm
	return base
}

func (m *manager) ResolveRoute(packet *core.Packet, sourceID int, defaultTarget int) (int, error) {
	if m.router == nil {
		return defaultTarget, nil
	}
	return m.router.ResolveRoute(packet, sourceID, defaultTarget)
}

func (m *manager) CheckFlowControl(packet *core.Packet, sourceID int, targetID int) error {
	if m.flow == nil {
		return nil
	}
	return m.flow.Check(packet, sourceID, targetID)
}

func (m *manager) DomainOf(addr uint64) string {
	if m.domain == nil {
		return ""
	}
	return m.domain.DomainOf(addr)
}

type defaultRouter struct{}

func (r *defaultRouter) ResolveRoute(_ *core.Packet, _ int, defaultTarget int) (int, error) {
	return defaultTarget, nil
}

type noopFlowController struct{}

func (f *noopFlowController) Check(_ *core.Packet, _ int, _ int) error {
	return nil
}

type defaultDomainMapper struct{}

func (m *defaultDomainMapper) DomainOf(_ uint64) string {
	return ""
}

func asManager(m Manager) *manager {
	if concrete, ok := m.(*manager); ok {
		return &manager{
			router: concrete.router,
			flow:   concrete.flow,
			domain: concrete.domain,
		}
	}
	return &manager{
		router: &defaultRouter{},
		flow:   &noopFlowController{},
		domain: &defaultDomainMapper{},
	}
}
