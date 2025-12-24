package main

/*
#include "bridge_c_api.h"
*/
import "C"

import (
	"fmt"
)

//export FlowSim_Init
func FlowSim_Init() {
	fmt.Println("[FlowSim] Initializing Network...")
	// TODO: Initialize actual Network, Nodes, and Links here
}

//export FlowSim_Tick
func FlowSim_Tick() {
	// fmt.Println("[FlowSim] Tick")
	// TODO: Advance Network Cycle
}

//export FlowSim_RecvRequest
func FlowSim_RecvRequest(req *C.Gem5Request) {
	// fmt.Printf("[FlowSim] Received Request: Addr=0x%x Cmd=%d\n", uint64(req.addr), int(req.cmd))

	// Phase 3 Logic: Extended Protocol Support
	switch req.cmd {
	case C.CMD_READ_REQ:
		req.cmd = C.CMD_READ_RESP
	case C.CMD_WRITE_REQ:
		req.cmd = C.CMD_WRITE_RESP
	case C.CMD_READ_EX_REQ:
		req.cmd = C.CMD_READ_EX_RESP // RFO -> Data + Exclusive
	case C.CMD_UPGRADE_REQ:
		req.cmd = C.CMD_UPGRADE_RESP // Shared -> Modified (No data usually, just Ack)
	case C.CMD_WRITEBACK:
		// Writeback usually doesn't need a response in Fire-and-Forget,
		// but GEM5 might expect WriteResp if it's WritebackDirty?
		// WritebackDirty is a Request. Response is WriteResp.
		req.cmd = C.CMD_WRITE_RESP
	case C.CMD_CLEAN_WB:
		return // Clean writeback dropped? Or Ack?
	default:
		return // Ignore unknown
	}

	fmt.Println("[FlowSim] Pre-Loopback Callback")
	// In real FlowSim, this would be injected into NoC.
	// Here we just call back directly to simulate "Zero Latency" NoC.
	C.Gem5_SendResponse(req)
	fmt.Println("[FlowSim] Post-Loopback Callback")
}

// Required for c-shared build mode
func main() {}
