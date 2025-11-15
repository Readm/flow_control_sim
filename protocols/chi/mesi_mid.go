package chi

import (
	"fmt"

	"github.com/Readm/flow_sim/capabilities"
	"github.com/Readm/flow_sim/slicc"
)

// MESIMidSpec models a CHI-compatible MESI controller located at a mid-level cache.
var MESIMidSpec = &slicc.StateMachineSpec{
	Name:         "CHI-MESI-Mid",
	Description:  "CHI mid-level MESI controller (acts as both requester and responder)",
	DefaultState: "I",
	States: []slicc.StateSpec{
		{Name: "I", Description: "Invalid – no cached copy"},
		{Name: "IS", Description: "Issued Read, waiting for data"},
		{Name: "IM", Description: "Issued ReadUnique/Upgrade, waiting for data"},
		{Name: "S", Description: "Shared clean copy"},
		{Name: "M", Description: "Modified (dirty/unique)"},
	},
	Events: []slicc.EventSpec{
		{Name: "LocalGetS", Description: "Local master issues ReadShared"},
		{Name: "LocalGetX", Description: "Local master issues ReadUnique/Upgrade"},
		{Name: "DataResp", Description: "Data returns from downstream/home"},
		{Name: "Writeback", Description: "Line written back or evicted"},
		{Name: "SnoopGetS", Description: "Upstream snoop requesting shared data"},
		{Name: "SnoopGetX", Description: "Upstream snoop requesting exclusive data"},
	},
	Transitions: []slicc.TransitionSpec{
		// Local read (shared)
		{FromStates: []string{"I"}, Events: []string{"LocalGetS"}, ToState: "IS", Actions: []string{"issue_gets"}},
		{FromStates: []string{"IS"}, Events: []string{"DataResp"}, ToState: "S", Actions: []string{"forward_data"}},

		// Local read unique / upgrade
		{FromStates: []string{"I"}, Events: []string{"LocalGetX"}, ToState: "IM", Actions: []string{"issue_getx"}},
		{FromStates: []string{"S"}, Events: []string{"LocalGetX"}, ToState: "IM", Actions: []string{"issue_upgrade"}},
		{FromStates: []string{"IM"}, Events: []string{"DataResp"}, ToState: "M", Actions: []string{"forward_data"}},

		// Writeback / eviction
		{FromStates: []string{"S"}, Events: []string{"Writeback"}, ToState: "I", Actions: []string{"writeback_clean"}},
		{FromStates: []string{"M"}, Events: []string{"Writeback"}, ToState: "I", Actions: []string{"writeback_dirty"}},

		// Snoops
		{FromStates: []string{"S"}, Events: []string{"SnoopGetX"}, ToState: "I", Actions: []string{"forward_clean", "invalidate"}},
		{FromStates: []string{"M"}, Events: []string{"SnoopGetX"}, ToState: "I", Actions: []string{"forward_dirty", "invalidate"}},
		{FromStates: []string{"M"}, Events: []string{"SnoopGetS"}, ToState: "S", Actions: []string{"forward_dirty_shared"}},
		{FromStates: []string{"S"}, Events: []string{"SnoopGetS"}, ToState: "S", Actions: []string{"forward_clean_shared"}},
	},
}

// NewMESIMidStateMachine builds the capability using the predefined spec.
func NewMESIMidStateMachine(cache capabilities.RequestCache) (capabilities.ProtocolStateMachine, error) {
	if err := MESIMidSpec.Validate(); err != nil {
		return nil, fmt.Errorf("CHI MESI spec invalid: %w", err)
	}
	return capabilities.NewStateMachineCapability("chi-mesi-mid", MESIMidSpec, cache)
}
