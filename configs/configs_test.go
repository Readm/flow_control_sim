package configs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	app "github.com/Readm/flow_sim/framework/app"

	"github.com/Readm/flow_sim/configs/loader"
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

func TestTopologyJSONLoader(t *testing.T) {
	t.Parallel()
	const payload = `{
		"meta": { "name": "json_demo", "description": "demo config" },
		"defaults": {
			"total_cycles": 120,
			"request_rate": 0.4,
			"slave_process_rate": 2,
			"master_relay_latency": 3,
			"relay_slave_latency": 1,
			"slave_relay_latency": 2
		},
		"nodes": [
			{"id": "rn0", "capabilities": ["requester"]},
			{"id": "hn0", "capabilities": ["home_directory"]},
			{"id": "sn0", "capabilities": ["slave_target"], "params":{"weight":2}}
		],
		"links": [
			{"from":"rn0","to":"hn0","latency":2,"bandwidth":1},
			{"from":"hn0","to":"sn0","latency":1,"bandwidth":1}
		],
		"schedules": [
			{
				"tick": 0,
				"source": "rn0",
				"transactions": [
					{"type":"ReadOnce","target":"sn0","address":"0x1000","data_size":64}
				]
			}
		],
		"initial_states": {
			"rn0": {"0x1000": "Shared"}
		}
	}`
	doc, err := loader.Load(bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("loader.Load: %v", err)
	}
	cfg, err := doc.ToAppConfig()
	if err != nil {
		t.Fatalf("ToAppConfig: %v", err)
	}
	if cfg.TotalCycles != 120 {
		t.Fatalf("unexpected total cycles %d", cfg.TotalCycles)
	}
	if cfg.NodeSchedules == nil || len(cfg.NodeSchedules["rn0"][0]) != 1 {
		t.Fatalf("schedule not parsed: %#v", cfg.NodeSchedules)
	}
	item := cfg.NodeSchedules["rn0"][0][0]
	if item.TransactionType != app.CHITxnReadOnce {
		t.Fatalf("transaction type mismatch: %v", item.TransactionType)
	}
	if cfg.InitialCacheState == nil || len(cfg.InitialCacheState) == 0 {
		t.Fatal("expected initial cache state")
	}
}

func TestRegisterJSON(t *testing.T) {
	t.Parallel()
	const payload = `{
		"meta": { "name": "temp_json_cfg", "description": "temp" },
		"defaults": {
			"total_cycles": 50,
			"master_relay_latency": 1,
			"relay_slave_latency": 1,
			"slave_relay_latency": 1
		},
		"nodes": [
			{"id": "rn0", "capabilities": ["requester"]},
			{"id": "hn0", "capabilities": ["home_directory"]},
			{"id": "sn0", "capabilities": ["slave_target"]}
		],
		"links": [
			{"from":"rn0","to":"hn0","latency":1,"bandwidth":1}
		]
	}`
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := RegisterJSON(path); err != nil {
		t.Fatalf("RegisterJSON: %v", err)
	}
	provider := Provider()
	cfg := provider.Get("temp_json_cfg")
	if cfg == nil {
		t.Fatal("registered JSON config not found")
	}
	if cfg.TotalCycles != 50 {
		t.Fatalf("expected TotalCycles=50, got %d", cfg.TotalCycles)
	}
}
