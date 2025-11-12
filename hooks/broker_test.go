package hooks

import (
	"errors"
	"testing"

	"flow_sim/core"
)

func TestRouteHooksModifyTarget(t *testing.T) {
	b := NewPluginBroker()

	b.RegisterBeforeRoute(func(ctx *RouteContext) error {
		ctx.TargetID = ctx.DefaultTarget + 1
		return nil
	})
	b.RegisterAfterRoute(func(ctx *RouteContext) error {
		ctx.TargetID = ctx.TargetID + 1
		return nil
	})

	packet := &core.Packet{}
	ctx := &RouteContext{
		Packet:        packet,
		SourceNodeID:  1,
		DefaultTarget: 10,
		TargetID:      10,
	}

	if err := b.EmitBeforeRoute(ctx); err != nil {
		t.Fatalf("EmitBeforeRoute returned error: %v", err)
	}
	if ctx.TargetID != 11 {
		t.Fatalf("expected target 11 after before hooks, got %d", ctx.TargetID)
	}

	if err := b.EmitAfterRoute(ctx); err != nil {
		t.Fatalf("EmitAfterRoute returned error: %v", err)
	}
	if ctx.TargetID != 12 {
		t.Fatalf("expected target 12 after after hooks, got %d", ctx.TargetID)
	}
}

func TestRouteHookErrorStopsProcessing(t *testing.T) {
	b := NewPluginBroker()
	calls := 0

	b.RegisterBeforeRoute(func(ctx *RouteContext) error {
		calls++
		return errors.New("hook fail")
	})
	b.RegisterBeforeRoute(func(ctx *RouteContext) error {
		calls++
		return nil
	})

	ctx := &RouteContext{TargetID: 5}
	err := b.EmitBeforeRoute(ctx)
	if err == nil {
		t.Fatalf("expected error from before route hook")
	}
	if calls != 1 {
		t.Fatalf("expected only first hook to run, calls=%d", calls)
	}
}

func TestSendHooks(t *testing.T) {
	b := NewPluginBroker()
	order := make([]string, 0, 2)

	b.RegisterBeforeSend(func(ctx *MessageContext) error {
		order = append(order, "before")
		return nil
	})
	b.RegisterAfterSend(func(ctx *MessageContext) error {
		order = append(order, "after")
		return nil
	})

	ctx := &MessageContext{Packet: &core.Packet{}}
	if err := b.EmitBeforeSend(ctx); err != nil {
		t.Fatalf("EmitBeforeSend error: %v", err)
	}
	if err := b.EmitAfterSend(ctx); err != nil {
		t.Fatalf("EmitAfterSend error: %v", err)
	}

	if len(order) != 2 || order[0] != "before" || order[1] != "after" {
		t.Fatalf("unexpected hook order: %v", order)
	}
}

func TestProcessHooks(t *testing.T) {
	b := NewPluginBroker()
	start := 0
	end := 0

	b.RegisterBeforeProcess(func(ctx *ProcessContext) error {
		start++
		return nil
	})
	b.RegisterAfterProcess(func(ctx *ProcessContext) error {
		end++
		return nil
	})

	ctx := &ProcessContext{Packet: &core.Packet{}}
	if err := b.EmitBeforeProcess(ctx); err != nil {
		t.Fatalf("EmitBeforeProcess error: %v", err)
	}
	if err := b.EmitAfterProcess(ctx); err != nil {
		t.Fatalf("EmitAfterProcess error: %v", err)
	}

	if start != 1 || end != 1 {
		t.Fatalf("unexpected hook counts: start=%d end=%d", start, end)
	}
}

func TestTxCreatedHook(t *testing.T) {
	b := NewPluginBroker()
	called := false

	b.RegisterTxCreated(func(ctx *TxCreatedContext) error {
		called = true
		return nil
	})

	err := b.EmitTxCreated(&TxCreatedContext{
		Packet:      &core.Packet{},
		Transaction: &core.Transaction{},
	})
	if err != nil {
		t.Fatalf("EmitTxCreated returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected hook to be called")
	}
}

func TestRegisterBundle(t *testing.T) {
	b := NewPluginBroker()

	desc := PluginDescriptor{
		Name:        "test-bundle",
		Category:    PluginCategoryCapability,
		Description: "sample bundle",
	}

	beforeSendCalled := 0
	afterProcessCalled := 0

	bundle := HookBundle{
		BeforeSend: []BeforeSendHook{
			func(ctx *MessageContext) error {
				beforeSendCalled++
				return nil
			},
		},
		AfterProcess: []AfterProcessHook{
			func(ctx *ProcessContext) error {
				afterProcessCalled++
				return nil
			},
		},
	}

	b.RegisterBundle(desc, bundle)

	if plugins := b.ListPlugins(PluginCategoryCapability); len(plugins) != 1 {
		t.Fatalf("expected 1 plugin descriptor, got %d", len(plugins))
	}

	if err := b.EmitBeforeSend(&MessageContext{}); err != nil {
		t.Fatalf("EmitBeforeSend returned error: %v", err)
	}
	if err := b.EmitAfterProcess(&ProcessContext{}); err != nil {
		t.Fatalf("EmitAfterProcess returned error: %v", err)
	}

	if beforeSendCalled != 1 {
		t.Fatalf("expected before send hook to be called once, got %d", beforeSendCalled)
	}
	if afterProcessCalled != 1 {
		t.Fatalf("expected after process hook to be called once, got %d", afterProcessCalled)
	}
}

func TestRegisterPluginMetadata(t *testing.T) {
	b := NewPluginBroker()

	desc := PluginDescriptor{
		Name:     "test-metadata",
		Category: PluginCategoryVisualization,
	}
	b.RegisterPluginMetadata(desc)
	b.RegisterPluginMetadata(desc)

	all := b.ListAllPlugins()
	if len(all) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(all))
	}
}
