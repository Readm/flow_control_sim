package main

// Packet represents a generic message flowing through the simulator.
// It is used for both request and response by distinguishing the Type field.
type Packet struct {
    ID          int64  // unique packet id
    Type        string // "request" or "response"
    SrcID       int    // source node id
    DstID       int    // destination node id

    GeneratedAt int // cycle when generated (for requests)
    SentAt      int // cycle when sent to channel
    ReceivedAt  int // cycle when received by next hop
    CompletedAt int // cycle when processed (for requests)

    // Request specific
    MasterID  int   // original master id
    RequestID int64 // request id (same as ID for request packets)
}

// Config holds simulation configuration values.
type Config struct {
    NumMasters int
    NumSlaves  int
    NumRelays  int // first phase = 1

    TotalCycles int

    // fixed one-way latencies in cycles
    MasterRelayLatency int // Master -> Relay
    RelayMasterLatency int // Relay  -> Master
    RelaySlaveLatency  int // Relay  -> Slave
    SlaveRelayLatency  int // Slave  -> Relay

    // processing and generation
    SlaveProcessRate int     // requests processed per cycle per slave
    RequestRate      float64 // per-master per-cycle probability to generate one request

    // weighting for choosing destination slave (length == NumSlaves)
    SlaveWeights []int

    // visualization settings
    Headless   bool   // true to run without visualization
    VisualMode string // "gui" | "web" | "none" (default: "gui" if Headless is false)
}

// NodeIDAllocator provides simple incremental ids for nodes.
type NodeIDAllocator struct {
    nextID int
}

func NewNodeIDAllocator() *NodeIDAllocator {
    return &NodeIDAllocator{nextID: 0}
}

func (a *NodeIDAllocator) Allocate() int {
    id := a.nextID
    a.nextID++
    return id
}

// PacketIDAllocator provides unique ids for packets.
type PacketIDAllocator struct {
    next int64
}

func NewPacketIDAllocator() *PacketIDAllocator {
    return &PacketIDAllocator{next: 1}
}

func (a *PacketIDAllocator) Allocate() int64 {
    id := a.next
    a.next++
    return id
}


