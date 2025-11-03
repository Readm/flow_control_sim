package main

// InFlightMessage represents a packet that is currently in transit.
type InFlightMessage struct {
    Packet       *Packet
    FromID       int
    ToID         int
    ArrivalCycle int
}

// Channel is a simple infinite-capacity link layer that only models fixed delays.
type Channel struct {
    inFlight []*InFlightMessage
}

func NewChannel() *Channel {
    return &Channel{inFlight: make([]*InFlightMessage, 0)}
}

// Send enqueues a packet that will arrive at target after given latency.
func (c *Channel) Send(packet *Packet, fromID, toID, currentCycle, latency int) {
    msg := &InFlightMessage{
        Packet:       packet,
        FromID:       fromID,
        ToID:         toID,
        ArrivalCycle: currentCycle + latency,
    }
    c.inFlight = append(c.inFlight, msg)
}

// CollectArrivals returns all messages that arrive at the given cycle and removes them from in-flight.
func (c *Channel) CollectArrivals(cycle int) []*InFlightMessage {
    if len(c.inFlight) == 0 {
        return nil
    }
    arrivals := make([]*InFlightMessage, 0)
    kept := c.inFlight[:0]
    for _, m := range c.inFlight {
        if m.ArrivalCycle <= cycle {
            arrivals = append(arrivals, m)
        } else {
            kept = append(kept, m)
        }
    }
    // shrink to kept
    c.inFlight = kept
    return arrivals
}

func (c *Channel) InFlightCount() int { return len(c.inFlight) }


