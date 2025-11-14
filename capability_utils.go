package main

import "github.com/Readm/flow_sim/capabilities"

func capabilityNameList(caps []capabilities.NodeCapability) []string {
	if len(caps) == 0 {
		return nil
	}
	names := make([]string, 0, len(caps))
	for _, cap := range caps {
		if cap == nil {
			continue
		}
		desc := cap.Descriptor()
		if desc.Name != "" {
			names = append(names, desc.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}
