package protocol

// This package defines the data structures matching the web/openapi.yaml definition.
// It serves as the single source of truth for the Go backend's communication with the CyEditor frontend.

// CyNetwork corresponds to the 'Network' schema in openapi.yaml.
type CyNetwork struct {
	Version string   `json:"version"`
	Cycle   int      `json:"cycle"`
	Nodes   []CyNode `json:"nodes"`
	Edges   []CyEdge `json:"edges"`
	// Extra fields for rendering control
	Zoom float64     `json:"zoom,omitempty"`
	Pan  *CyPosition `json:"pan,omitempty"`
}

// CyNode corresponds to the 'Node' schema in openapi.yaml.
type CyNode struct {
	NodeID          int           `json:"node_id"`
	NodeName        string        `json:"node_name"`
	NodeFeatures    []string      `json:"node_features"`
	Cache           *interface{}  `json:"cache,omitempty"`     // map to CacheConfig if needed
	Directory       *interface{}  `json:"directory,omitempty"` // map to DirectoryConfig if needed
	CoherenceDomain int           `json:"coherence_domain_id,omitempty"`
	InPorts         []interface{} `json:"in_ports,omitempty"`
	OutPorts        []interface{} `json:"out_ports,omitempty"`
	Display         CyNodeDisplay `json:"display"`
}

// CyNodeDisplay corresponds to the 'display' property of 'Node'.
type CyNodeDisplay struct {
	ID       string     `json:"id"` // Unique string ID for cytoscape
	Type     string     `json:"type"`
	Name     string     `json:"name"`
	Resize   bool       `json:"resize,omitempty"`
	Bg       string     `json:"bg,omitempty"`
	Width    float64    `json:"width"`
	Height   float64    `json:"height"`
	Position CyPosition `json:"position"`
	Image    string     `json:"image,omitempty"`
}

// CyEdge corresponds to the 'Edge' schema in openapi.yaml.
type CyEdge struct {
	EdgeID      int           `json:"edge_id"`
	SrcNodeID   int           `json:"src_node_id"`
	SrcPortID   int           `json:"src_port_id"`
	DstNodeID   int           `json:"dst_node_id"`
	DstPortID   int           `json:"dst_port_id"`
	PacketTypes []int         `json:"packet_types,omitempty"`
	Display     CyEdgeDisplay `json:"display"`
}

// CyEdgeDisplay corresponds to the 'display' property of 'Edge'.
type CyEdgeDisplay struct {
	Data       CyEdgeData   `json:"data"`
	Position   *CyPosition  `json:"position,omitempty"` // Usually null for edges
	LinkStatus []LinkStatus `json:"link_status,omitempty"`
}

// CyEdgeData corresponds to 'display.data' of 'Edge'.
type CyEdgeData struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	LineType string `json:"lineType"`
}

// LinkStatus corresponds to the 'link_status' array item in 'Edge.display'.
type LinkStatus struct {
	Name   string `json:"name"`
	Values []int  `json:"values"`
}

// CyPosition represents x/y coordinates.
type CyPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
