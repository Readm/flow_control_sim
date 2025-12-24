package base

/*
#include "../bridge_c_api.h"
*/
import "C"

type Gem5Command int

const (
	// Requests
	CmdReadReq    Gem5Command = C.CMD_READ_REQ
	CmdWriteReq   Gem5Command = C.CMD_WRITE_REQ
	CmdReadExReq  Gem5Command = C.CMD_READ_EX_REQ
	CmdWriteback  Gem5Command = C.CMD_WRITEBACK
	CmdCleanWB    Gem5Command = C.CMD_CLEAN_WB
	CmdUpgradeReq Gem5Command = C.CMD_UPGRADE_REQ
	CmdSwapReq    Gem5Command = C.CMD_SWAP_REQ

	// Responses
	CmdReadResp    Gem5Command = C.CMD_READ_RESP
	CmdWriteResp   Gem5Command = C.CMD_WRITE_RESP
	CmdReadExResp  Gem5Command = C.CMD_READ_EX_RESP
	CmdUpgradeResp Gem5Command = C.CMD_UPGRADE_RESP
	CmdRetryResp   Gem5Command = C.CMD_RETRY_RESP
)
