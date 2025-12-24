#include "flow_sim_port.hh"
#include "base/trace.hh"
#include "debug/ExternalPort.hh"

namespace gem5
{

bool
FlowSimExternalPort::recvTimingReq(PacketPtr pkt)
{
    // Convert GEM5 Packet to C Gem5Request
    Gem5Request req;
    req.addr = pkt->getAddr();
    req.size = pkt->getSize();
    
    // Map Command
    if (pkt->isRead()) req.cmd = 1; // CMD_READ_REQ
    else if (pkt->isWrite()) req.cmd = 2; // CMD_WRITE_REQ
    else req.cmd = 0; // Unknown

    req.tick = 0; // pkt->headerDelay? curTick()? 
    // Note: In real GEM5, we access curTick() but here we just pass 0 for Ping test
    
    req.id = pkt->requestorId();

    // Call FlowSim (Go)
    FlowSim_RecvRequest(&req);

    // For Phase 1, we just return true (accepted) and delete the packet if it's a request we handle?
    // In real simulation, we need to send a response later. 
    // For connectivity test, we just drop it or print.
    
    // To avoid memory leak in GEM5 if we don't respond:
    // Usually we should keep it until we respond. 
    // Here we just pretend we consumed it.
    
    return true;
}

void
FlowSimExternalPort::recvFunctional(PacketPtr pkt)
{
    // Functional access logic (optional for now)
}

Tick
FlowSimExternalPort::recvAtomic(PacketPtr pkt)
{
    return 0; 
}

// Register the handler
// ExternalSlave::registerHandler("flowsim", new FlowSimPortHandler);
// This registration typically happens in a .cc file static initializer or explicitly.

} // namespace gem5
