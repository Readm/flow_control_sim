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
	fmt.Printf("[FlowSim] Received Request: Addr=0x%x Size=%d Cmd=%d Tick=%d ID=%d\n",
		uint64(req.addr), uint32(req.size), int(req.cmd), uint64(req.tick), int(req.id))

	// TODO: Translate to internal Packet and inject to NoC
}

// Required for c-shared build mode
func main() {}
