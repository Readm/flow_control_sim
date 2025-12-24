package base

/*
#include "../bridge_c_api.h"
*/
import "C"

type Gem5Command int

const (
	CmdReadReq   Gem5Command = C.CMD_READ_REQ
	CmdWriteReq  Gem5Command = C.CMD_WRITE_REQ
	CmdReadResp  Gem5Command = C.CMD_READ_RESP
	CmdWriteResp Gem5Command = C.CMD_WRITE_RESP
)
