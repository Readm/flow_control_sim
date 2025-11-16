package hooks

import "testing"

func TestRegistryLoadGlobalAndNode(t *testing.T) {
	broker := NewPluginBroker()
	reg := NewRegistry(broker)

	globalDesc := PluginDescriptor{
		Name:     "global-metrics",
		Category: PluginCategoryInstrumentation,
	}

	if err := reg.RegisterGlobal("global-metrics", globalDesc, func(b *PluginBroker) error {
		b.RegisterBundle(globalDesc, HookBundle{
			BeforeSend: []BeforeSendHook{
				func(ctx *MessageContext) error { return nil },
			},
		})
		return nil
	}); err != nil {
		t.Fatalf("RegisterGlobal failed: %v", err)
	}

	nodeDesc := PluginDescriptor{
		Name:     "node-stub",
		Category: PluginCategoryCapability,
	}
	var capturedNodeID int
	if err := reg.RegisterNode("node-stub", nodeDesc, func(nodeID int, b *PluginBroker) error {
		capturedNodeID = nodeID
		return nil
	}); err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	if err := reg.LoadGlobal([]string{"global-metrics"}); err != nil {
		t.Fatalf("LoadGlobal failed: %v", err)
	}
	if err := reg.LoadForNode(42, []string{"node-stub"}); err != nil {
		t.Fatalf("LoadForNode failed: %v", err)
	}

	if capturedNodeID != 42 {
		t.Fatalf("expected node factory to receive id 42, got %d", capturedNodeID)
	}

	descs := broker.ListAllPlugins()
	if len(descs) != 2 {
		t.Fatalf("expected 2 plugin descriptors, got %d", len(descs))
	}
}

func TestRegistryDuplicateRegistration(t *testing.T) {
	reg := NewRegistry(NewPluginBroker())

	desc := PluginDescriptor{Name: "dup", Category: PluginCategoryPolicy}
	err := reg.RegisterGlobal("dup", desc, func(b *PluginBroker) error { return nil })
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}
	err = reg.RegisterGlobal("dup", desc, func(b *PluginBroker) error { return nil })
	if err == nil {
		t.Fatalf("expected duplicate registration to fail")
	}

	err = reg.RegisterNode("dup", desc, func(nodeID int, b *PluginBroker) error { return nil })
	if err != nil {
		t.Fatalf("first node registration failed: %v", err)
	}
	err = reg.RegisterNode("dup", desc, func(nodeID int, b *PluginBroker) error { return nil })
	if err == nil {
		t.Fatalf("expected duplicate node registration to fail")
	}
}

func TestRegistryUnknownPlugin(t *testing.T) {
	reg := NewRegistry(NewPluginBroker())

	if err := reg.LoadGlobal([]string{"missing"}); err == nil {
		t.Fatalf("expected error for missing global plugin")
	}

	if err := reg.LoadForNode(1, []string{"missing"}); err == nil {
		t.Fatalf("expected error for missing node plugin")
	}
}
