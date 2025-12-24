#include <iostream>
#include <vector>
#include <cstdint>
#include "internal/bridge/gem5/bridge_c_api.h"
#include "internal/bridge/gem5/libflowsim.h"

// Mock GEM5 Packet class (simplified)
class MockPacket {
public:
    uint64_t addr;
    uint32_t size;
    int cmd;
    
    MockPacket(uint64_t a, uint32_t s, int c) : addr(a), size(s), cmd(c) {}
    
    uint64_t getAddr() { return addr; }
    uint32_t getSize() { return size; }
    bool isRead() { return cmd == CMD_READ_REQ; }
    bool isWrite() { return cmd == CMD_WRITE_REQ; }
    int requestorId() { return 0; }
};

int main() {
    std::cout << "[Test] Initializing FlowSim..." << std::endl;
    FlowSim_Init();
    
    std::cout << "[Test] Advancing Tick..." << std::endl;
    FlowSim_Tick();
    
    std::cout << "[Test] Sending Request..." << std::endl;
    Gem5Request req;
    req.addr = 0x12345678;
    req.size = 64;
    req.cmd = CMD_READ_REQ;
    req.tick = 1000;
    req.id = 1;
    
    FlowSim_RecvRequest(&req);
    
    std::cout << "[Test] Done." << std::endl;
    return 0;
}
