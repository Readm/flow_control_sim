package ahead_port

// Connect creates a Port and connects two components.
// The upstream component will receive the InPort view (for sending data).
// The downstream component will receive the OutPort view (for receiving data).
//
// Usage:
//   port := Connect(outputQueue, link)
//   // outputQueue.toDownstream is now set to port.AsInPort()
//   // link.fromUpstream is now set to port.AsOutPort()
//
// Returns the created Port for monitoring/debugging if needed.
func Connect(upstream, downstream interface{}) *Port {
	// Use placeholder IDs for backward compatibility
	port := NewPort(-1, -1)

	// Set upstream component's downstream port (InPort view)
	if setter, ok := upstream.(interface{ SetDownstreamPort(InPort) }); ok {
		setter.SetDownstreamPort(port.AsInPort())
	} else {
		panic("upstream component does not have SetDownstreamPort(InPort) method")
	}

	// Set downstream component's upstream port (OutPort view)
	if setter, ok := downstream.(interface{ SetUpstreamPort(OutPort) }); ok {
		setter.SetUpstreamPort(port.AsOutPort())
	} else {
		panic("downstream component does not have SetUpstreamPort(OutPort) method")
	}

	return port
}

// ConnectWithIDs creates a Port with specified node IDs and connects two components.
// This is useful for profiling where you need to track which nodes are communicating.
func ConnectWithIDs(sourceNodeID, targetNodeID int, upstream, downstream interface{}) *Port {
	port := NewPort(sourceNodeID, targetNodeID)

	// Set upstream component's downstream port (InPort view)
	if setter, ok := upstream.(interface{ SetDownstreamPort(InPort) }); ok {
		setter.SetDownstreamPort(port.AsInPort())
	} else {
		panic("upstream component does not have SetDownstreamPort(InPort) method")
	}

	// Set downstream component's upstream port (OutPort view)
	if setter, ok := downstream.(interface{ SetUpstreamPort(OutPort) }); ok {
		setter.SetUpstreamPort(port.AsOutPort())
	} else {
		panic("downstream component does not have SetUpstreamPort(OutPort) method")
	}

	return port
}

// ConnectWithPort uses an existing port to connect two components.
// This is useful when you need to create the port separately for monitoring.
func ConnectWithPort(port *Port, upstream, downstream interface{}) {
	// Set upstream component's downstream port (InPort view)
	if setter, ok := upstream.(interface{ SetDownstreamPort(InPort) }); ok {
		setter.SetDownstreamPort(port.AsInPort())
	} else {
		panic("upstream component does not have SetDownstreamPort(InPort) method")
	}

	// Set downstream component's upstream port (OutPort view)
	if setter, ok := downstream.(interface{ SetUpstreamPort(OutPort) }); ok {
		setter.SetUpstreamPort(port.AsOutPort())
	} else {
		panic("downstream component does not have SetUpstreamPort(OutPort) method")
	}
}
