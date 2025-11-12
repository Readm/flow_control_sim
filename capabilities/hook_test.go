package capabilities

import (
	"testing"

	"flow_sim/core"
	"flow_sim/hooks"
)

func TestHookCapabilityRegistersHandlers(t *testing.T) {
	broker := hooks.NewPluginBroker()
	count := 0
	bundle := hooks.HookBundle{
		AfterProcess: []hooks.AfterProcessHook{
			func(ctx *hooks.ProcessContext) error {
				count++
				return nil
			},
		},
	}

	cap := NewHookCapability("test-hook", hooks.PluginCategoryInstrumentation, "hook bundle", bundle)
	if err := cap.Register(broker); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	ctx := &hooks.ProcessContext{
		Packet: &core.Packet{},
		NodeID: 1,
		Cycle:  10,
	}
	if err := broker.EmitAfterProcess(ctx); err != nil {
		t.Fatalf("EmitAfterProcess returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected hook to increment count")
	}
}
