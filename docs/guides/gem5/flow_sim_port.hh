#ifndef __FLOW_SIM_PORT_HH__
#define __FLOW_SIM_PORT_HH__

#include "mem/external_slave.hh"
#include "mem/packet.hh"
#include "sim/eventq.hh"
#include "internal/bridge/gem5/bridge_c_api.h" 

// Function pointers from libflowsim.so
extern "C" {
    void FlowSim_Init();
    void FlowSim_Tick();
    void FlowSim_RecvRequest(Gem5Request* req);
}

namespace gem5
{

class FlowSimExternalPort : public ExternalSlave::ExternalPort
{
  private:
    // Tick Event to drive FlowSim
    class TickEvent : public Event
    {
      private:
        FlowSimExternalPort& port;
      public:
        TickEvent(FlowSimExternalPort& p) : Event(Default_Pri), port(p) {}
        void process() override {
            port.handleTick();
        }
        const char* description() const override { return "FlowSim Tick Event"; }
    };

    // Response Event to send packet back to GEM5
    class ResponseEvent : public Event
    {
      private:
        FlowSimExternalPort& port;
        PacketPtr pkt;
      public:
        ResponseEvent(FlowSimExternalPort& p, PacketPtr packet) 
            : Event(Default_Pri), port(p), pkt(packet) {}
        void process() override {
            port.sendResponseNow(pkt);
            delete this; 
        }
        const char* description() const override { return "FlowSim Response Event"; }
    };

    // Retry Event to signal GEM5 to retry failed requests
    class RetryEvent : public Event
    {
      private:
        FlowSimExternalPort& port;
      public:
        RetryEvent(FlowSimExternalPort& p) : Event(Default_Pri), port(p) {}
        void process() override {
            port.sendRetryNow();
        }
        const char* description() const override { return "FlowSim Retry Event"; }
    };

    TickEvent tickEvent;
    RetryEvent retryEvent;
    Tick tickInterval;

  public:
    FlowSimExternalPort(const std::string &name_, ExternalSlave &owner_) :
        ExternalSlave::ExternalPort(name_, owner_),
        tickEvent(*this),
        retryEvent(*this),
        tickInterval(1000) // Default 1 cycle = 1000 ticks (1ns if 1THz?? typical is 1ps or 1ns depending on freq)
                           // Let's assume 1GHz = 1000 ticks if 1 tick = 1ps. 
    {
        static bool initialized = false;
        if (!initialized) {
            FlowSim_Init();
            initialized = true;
        }
        // Schedule first tick? 
        // Need to be careful not to schedule during construction if eventq not ready.
        // Usually done in init() or first recv. 
    }

    void init(); // Called by owner->init()

    bool recvTimingReq(PacketPtr pkt);
    void recvFunctional(PacketPtr pkt);
    Tick recvAtomic(PacketPtr pkt);
    
    // Internal handlers
    void handleTick();
    void scheduleResponse(PacketPtr pkt, Tick delay);
    void sendResponseNow(PacketPtr pkt);
    
    void scheduleRetry();
    void sendRetryNow();

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
