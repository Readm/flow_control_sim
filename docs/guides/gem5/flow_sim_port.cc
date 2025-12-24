#include "flow_sim_port.hh"
#include "base/trace.hh"
#include "debug/ExternalPort.hh"
#include <iostream>

// Global C export
extern "C" void Gem5_SendResponse(Gem5Request* resp) {
    if (!resp || !resp->port_handle) return;
    
    gem5::FlowSimExternalPort* port = static_cast<gem5::FlowSimExternalPort*>(resp->port_handle);
    
    // Assume 'id' holds the PacketPtr address for Phase 2/3 Loopback/Shim
    gem5::PacketPtr pkt = reinterpret_cast<gem5::PacketPtr>(resp->id);
    
    // Map Response Command back to Packet
    // Note: In real GEM5, makeResponse() updates the command in place based on the request command.
    // If we are simulating full loopback, we might need to be careful. 
    // Usually, the PacketPtr is still the SAME request packet instance.
    // We just need to call makeResponse() on it if not already done, or if FlowSim tells us to.
    
    if (resp->cmd == CMD_READ_RESP) {
        if (!pkt->isResponse()) pkt->makeResponse();
        // Handle Data payload...
        // For simulation, we might assume data is irrelevant or handled separately.
        // real integration needs memcpy if data matters.
    } else if (resp->cmd == CMD_WRITE_RESP) {
        if (!pkt->isResponse()) pkt->makeResponse();
    } else if (resp->cmd == CMD_READ_EX_RESP) {
         if (!pkt->isResponse()) pkt->makeResponse();
    } else if (resp->cmd == CMD_UPGRADE_RESP) {
         if (!pkt->isResponse()) pkt->makeResponse();
    } else if (resp->cmd == CMD_RETRY_RESP) {
        // Special case: FlowSim signalling it is ready to receive again (SendRetry)
        // Wait, RETRY_RESP is for when FlowSim sends TO Gem5? 
        // No, usually internal signaling. 
    }
    
    // Schedule response
    port->scheduleResponse(pkt, 1);
}

extern "C" void Gem5_SendRetry(void* port_handle) {
    if (!port_handle) return;
    gem5::FlowSimExternalPort* port = static_cast<gem5::FlowSimExternalPort*>(port_handle);
    port->scheduleRetry();
}

namespace gem5
{

void
FlowSimExternalPort::init()
{
    if (!tickEvent.scheduled()) {
        schedule(tickEvent, curTick() + tickInterval);
    }
}

void
FlowSimExternalPort::handleTick()
{
    FlowSim_Tick();
    schedule(tickEvent, curTick() + tickInterval);
}

bool
FlowSimExternalPort::recvTimingReq(PacketPtr pkt)
{
    Gem5Request req;
    req.addr = pkt->getAddr();
    req.size = pkt->getSize();
    req.tick = curTick();
    req.id = (uint64_t)pkt; 
    req.port_handle = this; 

    // Detailed Command Mapping (Phase 3)
    if (pkt->isRead()) {
        if (pkt->needsWritable()) req.cmd = CMD_READ_EX_REQ;
        else req.cmd = CMD_READ_REQ;
    } else if (pkt->isWrite()) {
        if (pkt->cmd == MemCmd::WritebackDirty) req.cmd = CMD_WRITEBACK;
        else if (pkt->cmd == MemCmd::CleanEvict) req.cmd = CMD_CLEAN_WB; // Or ignore
        else req.cmd = CMD_WRITE_REQ;
    } else if (pkt->isUpgrade()) {
        req.cmd = CMD_UPGRADE_REQ;
    } else if (pkt->isSwap()) {
        req.cmd = CMD_SWAP_REQ;
    } else {
        // Unsupported or invalid for this bridge
        req.cmd = 0; 
    }

    // Call FlowSim
    // Note: FlowSim_RecvRequest returns void in current definition.
    // Ideally it should return int (Accepted/Rejected).
    // For now assuming infinite buffer in FlowSim (Always Accepted).
    // If we want backpressure, FlowSim_RecvRequest needs to return bool.
    // Let's stick to void + Async Retry for Phase 3 simplified.
    FlowSim_RecvRequest(&req);

    return true; 
}

void
FlowSimExternalPort::scheduleResponse(PacketPtr pkt, Tick delay)
{
    ResponseEvent* e = new ResponseEvent(*this, pkt);
    schedule(e, curTick() + delay);
}

void 
FlowSimExternalPort::sendResponseNow(PacketPtr pkt)
{
    bool success = sendTimingResp(pkt);
    if (!success) {
        // If sending response failed, we must wait for recvRespRetry from Master/CPU.
        // But ExternalSlave interface usually implies WE are the Slave.
        // If we send Response and it fails, it means the Master is busy.
        // We should not delete the packet or event. We should retry later.
        // Implementation detail: The Master will call us back via recvRespRetry?
        // Wait, recvRespRetry is a method of RequestPort (Master). 
        // We are a ResponsePort (Slave). The *Master* calls *us* with Requests.
        // We call the Master with Responses.
        // If sendTimingResp fails, the Master will call our recvRespRetry() when it has space?
        // NO. If sendTimingResp logic:
        // "If this function returns true, the packet was accepted. If false, the receiver was not able to accept... and will call recvRespRetry() when it is able to."
        // So we (Slave) call sendTimingResp. If it returns false, WE (Slave) must store the packet and wait for the Master to call OUR recvRespRetry().
        // BUT `recvRespRetry` is a virtual method of `RequestPort`. We are `ResponsePort`.
        // The Master implements `recvTimingResp`.
        // If `sendTimingResp` fails, it implies the Master (peer) is busy.
        // The Master will call `retryResp` on its own side?
        // Actually: "The receiver will call recvRespRetry on the sender."
        // We are the Sender of the Response. The Receiver (Master) calls `recvRespRetry` on US (Slave)?
        // Use `recvRespRetry` in `ResponsePort`? 
        // Checking gem5/src/mem/port.hh...
        // ResponsePort has `virtual void recvRespRetry() = 0;`. YES.
        // So we must implement `recvRespRetry`.
        
        // Complex Backpressure logic:
        // 1. Store blocked packet.
        // 2. Initial state = Blocked.
        // 3. When recvRespRetry() is called, resend packet.
        
        // For now, let's just Panic as implementing full buffer management is Phase 4 :)
        // Or simple busy loop (not possible in DES).
        std::cerr << "FlowSim Bridge: Failed to send TimingResp! Backpressure NOT IMPLEMENTED." << std::endl;
    }
}

void
FlowSimExternalPort::handleTick()
{
    FlowSim_Tick();
    schedule(tickEvent, curTick() + tickInterval);
}

void
FlowSimExternalPort::scheduleRetry()
{
    if (!retryEvent.scheduled()) {
        schedule(retryEvent, curTick());
    }
}

void
FlowSimExternalPort::sendRetryNow()
{
    // Notify GEM5 (Master) that we (Slave) can accept new requests.
    // This calls `sendRetryReq` on our Port.
    sendRetryReq();
}

void
FlowSimExternalPort::recvFunctional(PacketPtr pkt)
{
}

Tick
FlowSimExternalPort::recvAtomic(PacketPtr pkt)
{
    return 0; 
}

} // namespace gem5
