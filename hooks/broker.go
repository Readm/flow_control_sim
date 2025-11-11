package hooks

import (
	"sync"

	"flow_sim/core"
)

// TxCreatedContext carries information for transaction creation hooks.
type TxCreatedContext struct {
	Packet      *core.Packet
	Transaction *core.Transaction
}

// TxCreatedHook defines the signature for OnTxCreated plugins.
type TxCreatedHook func(ctx *TxCreatedContext) error

// PluginBroker coordinates hook registration and triggering.
type PluginBroker struct {
	mu sync.RWMutex

	txCreatedHooks []TxCreatedHook

	beforeRouteHooks   []BeforeRouteHook
	afterRouteHooks    []AfterRouteHook
	beforeSendHooks    []BeforeSendHook
	afterSendHooks     []AfterSendHook
	beforeProcessHooks []BeforeProcessHook
	afterProcessHooks  []AfterProcessHook
}

// NewPluginBroker creates an empty broker instance.
func NewPluginBroker() *PluginBroker {
	return &PluginBroker{
		txCreatedHooks:     make([]TxCreatedHook, 0),
		beforeRouteHooks:   make([]BeforeRouteHook, 0),
		afterRouteHooks:    make([]AfterRouteHook, 0),
		beforeSendHooks:    make([]BeforeSendHook, 0),
		afterSendHooks:     make([]AfterSendHook, 0),
		beforeProcessHooks: make([]BeforeProcessHook, 0),
		afterProcessHooks:  make([]AfterProcessHook, 0),
	}
}

// RegisterTxCreated adds a new hook executed when a transaction is created.
func (p *PluginBroker) RegisterTxCreated(h TxCreatedHook) {
	if p == nil || h == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.txCreatedHooks = append(p.txCreatedHooks, h)
}

type RouteContext struct {
	Packet        *core.Packet
	SourceNodeID  int
	DefaultTarget int
	TargetID      int
}

// MessageContext provides data for send/receive hook handlers.
type MessageContext struct {
	Packet *core.Packet
	NodeID int
	Cycle  int
}

// ProcessContext provides data for process stage hooks.
type ProcessContext struct {
	Packet *core.Packet
	NodeID int
	Cycle  int
}

type BeforeRouteHook func(ctx *RouteContext) error
type AfterRouteHook func(ctx *RouteContext) error

// BeforeSendHook executes prior to sending a packet from a node.
type BeforeSendHook func(ctx *MessageContext) error

// AfterSendHook executes after a packet has been dispatched by a node.
type AfterSendHook func(ctx *MessageContext) error

// BeforeProcessHook executes before a node starts processing a packet.
type BeforeProcessHook func(ctx *ProcessContext) error

// AfterProcessHook executes after a node finishes processing a packet.
type AfterProcessHook func(ctx *ProcessContext) error

// RegisterBeforeSend registers a hook for the OnBeforeSend stage.
func (p *PluginBroker) RegisterBeforeSend(h BeforeSendHook) {
	if p == nil || h == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.beforeSendHooks = append(p.beforeSendHooks, h)
}

// RegisterAfterSend registers a hook for the OnAfterSend stage.
func (p *PluginBroker) RegisterAfterSend(h AfterSendHook) {
	if p == nil || h == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.afterSendHooks = append(p.afterSendHooks, h)
}

// RegisterBeforeProcess registers a hook for the OnBeforeProcess stage.
func (p *PluginBroker) RegisterBeforeProcess(h BeforeProcessHook) {
	if p == nil || h == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.beforeProcessHooks = append(p.beforeProcessHooks, h)
}

// RegisterAfterProcess registers a hook for the OnAfterProcess stage.
func (p *PluginBroker) RegisterAfterProcess(h AfterProcessHook) {
	if p == nil || h == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.afterProcessHooks = append(p.afterProcessHooks, h)
}

func (p *PluginBroker) RegisterBeforeRoute(h BeforeRouteHook) {
	if p == nil || h == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.beforeRouteHooks = append(p.beforeRouteHooks, h)
}

func (p *PluginBroker) RegisterAfterRoute(h AfterRouteHook) {
	if p == nil || h == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.afterRouteHooks = append(p.afterRouteHooks, h)
}

// EmitBeforeSend triggers OnBeforeSend hooks.
func (p *PluginBroker) EmitBeforeSend(ctx *MessageContext) error {
	if p == nil || ctx == nil {
		return nil
	}
	p.mu.RLock()
	handlers := make([]BeforeSendHook, len(p.beforeSendHooks))
	copy(handlers, p.beforeSendHooks)
	p.mu.RUnlock()
	for _, handler := range handlers {
		if err := handler(ctx); err != nil {
			return err
		}
	}
	return nil
}

// EmitAfterSend triggers OnAfterSend hooks.
func (p *PluginBroker) EmitAfterSend(ctx *MessageContext) error {
	if p == nil || ctx == nil {
		return nil
	}
	p.mu.RLock()
	handlers := make([]AfterSendHook, len(p.afterSendHooks))
	copy(handlers, p.afterSendHooks)
	p.mu.RUnlock()
	for _, handler := range handlers {
		if err := handler(ctx); err != nil {
			return err
		}
	}
	return nil
}

// EmitBeforeProcess triggers OnBeforeProcess hooks.
func (p *PluginBroker) EmitBeforeProcess(ctx *ProcessContext) error {
	if p == nil || ctx == nil {
		return nil
	}
	p.mu.RLock()
	handlers := make([]BeforeProcessHook, len(p.beforeProcessHooks))
	copy(handlers, p.beforeProcessHooks)
	p.mu.RUnlock()
	for _, handler := range handlers {
		if err := handler(ctx); err != nil {
			return err
		}
	}
	return nil
}

// EmitAfterProcess triggers OnAfterProcess hooks.
func (p *PluginBroker) EmitAfterProcess(ctx *ProcessContext) error {
	if p == nil || ctx == nil {
		return nil
	}
	p.mu.RLock()
	handlers := make([]AfterProcessHook, len(p.afterProcessHooks))
	copy(handlers, p.afterProcessHooks)
	p.mu.RUnlock()
	for _, handler := range handlers {
		if err := handler(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *PluginBroker) EmitBeforeRoute(ctx *RouteContext) error {
	if p == nil || ctx == nil {
		return nil
	}
	p.mu.RLock()
	handlers := make([]BeforeRouteHook, len(p.beforeRouteHooks))
	copy(handlers, p.beforeRouteHooks)
	p.mu.RUnlock()
	for _, handler := range handlers {
		if err := handler(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *PluginBroker) EmitAfterRoute(ctx *RouteContext) error {
	if p == nil || ctx == nil {
		return nil
	}
	p.mu.RLock()
	handlers := make([]AfterRouteHook, len(p.afterRouteHooks))
	copy(handlers, p.afterRouteHooks)
	p.mu.RUnlock()
	for _, handler := range handlers {
		if err := handler(ctx); err != nil {
			return err
		}
	}
	return nil
}

// EmitTxCreated triggers all registered transaction creation hooks.
func (p *PluginBroker) EmitTxCreated(ctx *TxCreatedContext) error {
	if p == nil || ctx == nil {
		return nil
	}

	p.mu.RLock()
	handlers := make([]TxCreatedHook, len(p.txCreatedHooks))
	copy(handlers, p.txCreatedHooks)
	p.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler(ctx); err != nil {
			return err
		}
	}
	return nil
}
