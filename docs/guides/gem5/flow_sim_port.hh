#ifndef __FLOW_SIM_PORT_HH__
#define __FLOW_SIM_PORT_HH__

#include "mem/external_slave.hh"
#include "mem/packet.hh"

// Function pointers from libflowsim.so
extern "C" {
    typedef struct {
        uint64_t addr;
        uint32_t size;
        int      cmd;
        uint64_t tick;
        int      id;
    } Gem5Request;

    void FlowSim_Init();
    void FlowSim_Tick();
    void FlowSim_RecvRequest(Gem5Request* req);
}

namespace gem5
{

class FlowSimExternalPort : public ExternalSlave::ExternalPort
{
  public:
    FlowSimExternalPort(const std::string &name_, ExternalSlave &owner_) :
        ExternalSlave::ExternalPort(name_, owner_)
    {
        // Initialize FlowSim when the first port is created (or use a dedicated init object)
        static bool initialized = false;
        if (!initialized) {
            FlowSim_Init();
            initialized = true;
        }
    }

    ~FlowSimExternalPort() { }

    bool recvTimingReq(PacketPtr pkt);
    void recvFunctional(PacketPtr pkt);
    Tick recvAtomic(PacketPtr pkt);
    
    // Stub implementations for now
    bool recvTimingSnoopResp(PacketPtr pkt) { return true; }
    void recvRespRetry() { }
    void recvFunctionalSnoop(PacketPtr packet) { }
};

class FlowSimPortHandler : public ExternalSlave::Handler
{
  public:
    ExternalSlave::ExternalPort *getExternalPort(
        const std::string &name, ExternalSlave &owner,
        const std::string &port_data)
    {
        return new FlowSimExternalPort(name, owner);
    }
};

} // namespace gem5

#endif // __FLOW_SIM_PORT_HH__
