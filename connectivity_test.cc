#include <iostream>
#include <vector>
#include <cstdint>
#include <functional>
#include "internal/bridge/gem5/bridge_c_api.h"
#include "internal/bridge/gem5/libflowsim.h"

// ==========================================
// Mocks for GEM5 Environment
// ==========================================

class MockPacket {
public:
    uint64_t addr;
    uint32_t size;
    int cmd;
    
    MockPacket(uint64_t a, uint32_t s, int c) : addr(a), size(s), cmd(c) {}
    
    void makeResponse() { 
        std::cout << "[GEM5 Mock] Packet converted to Response (Cmd=" << cmd << ")" << std::endl;
    }
    uint64_t getAddr() { return addr; }
    uint32_t getSize() { return size; }
    int requestorId() { return 1; }
    void setData(uint8_t* d) {}
    void deleteData() {}
    
    bool isRead() { return cmd == CMD_READ_REQ || cmd == CMD_READ_EX_REQ; }
    bool isWrite() { return cmd == CMD_WRITE_REQ || cmd == CMD_WRITEBACK; }
    
    // Phase 3 helpers
    bool needsWritable() { return cmd == CMD_READ_EX_REQ; }
    bool isUpgrade() { return cmd == CMD_UPGRADE_REQ; }
    bool isResponse() { return cmd >= 10; }
    bool isSwap() { return cmd == CMD_SWAP_REQ; }
    
};

typedef MockPacket* PacketPtr;

class MockPort {
public:
    void scheduleResponse(PacketPtr pkt, uint64_t delay) {
        std::cout << "[GEM5 Mock] Response Scheduled for Cmd: " << pkt->cmd << std::endl;
        sendTimingResp(pkt);
    }
    
    bool sendTimingResp(PacketPtr pkt) {
        std::cout << "[GEM5 Mock] Response Sent! Cycle Complete." << std::endl;
        return true;
    }
    
    void scheduleRetry() {
        std::cout << "[GEM5 Mock] Retry Scheduled!" << std::endl;
        sendRetryReq();
    }
    
    void sendRetryReq() {
        std::cout << "[GEM5 Mock] SendRetryReq Called! Master notified to resume." << std::endl;
    }
};

namespace gem5 {
    // Mocking MemCmd Namespace/Enum effectively
    namespace MemCmd {
        const int WritebackDirty = CMD_WRITEBACK;
        const int CleanEvict = CMD_CLEAN_WB;
    }
}

// ==========================================
// Shim Re-implementation for Test Context
// ==========================================
extern "C" void Gem5_SendResponse(Gem5Request* resp) {
    std::cout << "[C++ Shim] Gem5_SendResponse Called." << std::endl;
    if (!resp || !resp->port_handle) return;
    
    MockPort* port = static_cast<MockPort*>(resp->port_handle);
    PacketPtr pkt = reinterpret_cast<PacketPtr>(resp->id);
    
    // Update Packet Cmd based on Response
    pkt->cmd = resp->cmd; // Simplified update for mock
    
    port->scheduleResponse(pkt, 1);
}

extern "C" void Gem5_SendRetry(void* port_handle) {
    if (!port_handle) return;
    MockPort* port = static_cast<MockPort*>(port_handle);
    port->scheduleRetry();
}


// ==========================================
// Main Test
// ==========================================
int main() {
    std::cout << "[Test Phase 3] Initializing FlowSim..." << std::endl;
    FlowSim_Init();
    
    MockPort port;
    
    // Test 1: Read Exclusive (RFO) -> ReadExResp
    std::cout << "\n[Test Phase 3] Testing ReadExReq (RFO)..." << std::endl;
    MockPacket pkt1(0xAAAA0000, 64, CMD_READ_EX_REQ);
    Gem5Request req1;
    req1.addr = pkt1.addr;
    req1.size = pkt1.size;
    req1.cmd = CMD_READ_EX_REQ;
    req1.id = (uint64_t)&pkt1;
    req1.port_handle = &port;
    
    FlowSim_RecvRequest(&req1);
    
    // Test 2: Writeback -> WriteResp
    std::cout << "\n[Test Phase 3] Testing Writeback..." << std::endl;
    MockPacket pkt2(0xBBBB0000, 64, CMD_WRITEBACK);
    Gem5Request req2;
    req2.addr = pkt2.addr;
    req2.size = pkt2.size;
    req2.cmd = CMD_WRITEBACK;
    req2.id = (uint64_t)&pkt2;
    req2.port_handle = &port;
    
    FlowSim_RecvRequest(&req2);
    
    // Test 3: Manual Retry Trigger (Simulate FlowSim availability)
    std::cout << "\n[Test Phase 3] Testing SendRetry..." << std::endl;
    Gem5_SendRetry(&port);

    std::cout << "\n[Test Phase 3] Done." << std::endl;
    return 0;
}
