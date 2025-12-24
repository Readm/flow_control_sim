#ifndef BRIDGE_C_API_H
#define BRIDGE_C_API_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// Command Types matching GEM5 MemCmd (Simplified for now)
#define CMD_READ_REQ  1
#define CMD_WRITE_REQ 2
#define CMD_READ_RESP 3
#define CMD_WRITE_RESP 4

typedef struct {
    uint64_t addr;
    uint32_t size;
    int      cmd;
    uint64_t tick;
    int      id; // Requestor ID or Core ID
} Gem5Request;

// Callback function pointer type for sending response back to GEM5
typedef void (*ResponseCallback)(void* pkt_handle);

#ifdef __cplusplus
}
#endif

#endif // BRIDGE_C_API_H
