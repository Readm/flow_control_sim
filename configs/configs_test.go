package configs

import (
	"testing"

	app "github.com/Readm/flow_sim/framework/app"
)

func TestRegisteredConfigsValidate(t *testing.T) {
	provider := Provider()
	descriptors := provider.List()
	if len(descriptors) == 0 {
		t.Fatal("no configs registered")
	}
	for _, desc := range descriptors {
		if desc.Config == nil {
			t.Fatalf("config %s missing template", desc.Name)
		}
		cfg := desc.Config
		if err := app.ValidateConfig(cfg); err != nil {
			t.Fatalf("config %s failed validation: %v", desc.Name, err)
		}
	}
}
