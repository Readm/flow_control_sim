//go:build amd64
// +build amd64

#include "textflag.h"

// func Pause()
TEXT ·Pause(SB), NOSPLIT, $0
    PAUSE
    RET
