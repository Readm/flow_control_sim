#ifndef BRIDGE_C_API_H
#define BRIDGE_C_API_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// Command Types matching GEM5 MemCmd
// Requests
#define CMD_READ_REQ     1
#define CMD_WRITE_REQ    2
#define CMD_READ_EX_REQ  3  // Read Exclusive (RFO)
#define CMD_WRITEBACK    4  // Writeback Dirty
#define CMD_CLEAN_WB     5  // Writeback Clean
#define CMD_UPGRADE_REQ  6  // Upgrade (Shared -> Modified)
#define CMD_SWAP_REQ     7  // Atomic Swap/LLSC

// Responses
#define CMD_READ_RESP    10
#define CMD_WRITE_RESP   11
#define CMD_READ_EX_RESP 12
#define CMD_UPGRADE_RESP 13
#define CMD_RETRY_RESP   14 // Not a real MemCmd, but used for internal signaling if needed

typedef struct {
    uint64_t addr;
    uint32_t size;
    int      cmd;
    uint64_t tick;
    uint64_t id; // Requestor ID or Packet Ptr (64-bit)
    void*    port_handle; // Pointer to the C++ Port object for callback
} Gem5Request;

// Function to calculate and send response back to GEM5
void Gem5_SendResponse(Gem5Request* resp);

// Signal GEM5 that FlowSim has space and can accept new requests (SendRetry)
void Gem5_SendRetry(void* port_handle);

#ifdef __cplusplus
}
#endif

#endif // BRIDGE_C_API_H
