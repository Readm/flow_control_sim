package loader

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// LoadFile reads a JSON topology document from a file path.
func LoadFile(path string) (*TopologyDocument, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open topology config: %w", err)
	}
	defer f.Close()
	return Load(f)
}

// Load parses a topology document from any io.Reader.
func Load(r io.Reader) (*TopologyDocument, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read topology config: %w", err)
	}
	var doc TopologyDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse topology config: %w", err)
	}
	doc.applyCollectionDefaults()
	return &doc, nil
}

func (doc *TopologyDocument) applyCollectionDefaults() {
	if doc.Nodes == nil {
		doc.Nodes = []NodeDocument{}
	}
	if doc.Links == nil {
		doc.Links = []LinkDocument{}
	}
	if doc.Schedules == nil {
		doc.Schedules = []ScheduleDocument{}
	}
	if doc.InitialStates == nil {
		doc.InitialStates = make(map[string]map[string]string)
	}
}
