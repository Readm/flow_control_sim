package incentives

import (
	"testing"

	"github.com/Readm/flow_sim/hooks"
)

func TestRegisterAndLoad(t *testing.T) {
	broker := hooks.NewPluginBroker()
	reg := hooks.NewRegistry(broker)

	called := false
	factories := map[string]Factory{
		"stub": func(*hooks.PluginBroker) error {
			called = true
			return nil
		},
	}

	if err := Register(reg, Options{Factories: factories}); err != nil {
		t.Fatalf("register returned error: %v", err)
	}

	if err := reg.LoadGlobal([]string{"incentive/stub"}); err != nil {
		t.Fatalf("load returned error: %v", err)
	}

	if !called {
		t.Fatalf("expected factory to be called")
	}
}
