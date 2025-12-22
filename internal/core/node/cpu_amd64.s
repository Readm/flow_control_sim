//go:build amd64
// +build amd64

#include "textflag.h"

// func GetCPUCycles() uint64
TEXT ·GetCPUCycles(SB), NOSPLIT, $0-8
    RDTSC           // Result in EDX:EAX
    SHLQ    $32, DX // Shift high 32 bits of EDX to high 32 bits of RDX (actually we need to combine)
    ORQ     DX, AX  // Combine: RDX:RAX is wrong? No.
                    // RDTSC puts high 32 bits in EDX, low 32 in EAX.
                    // We need to return 64-bit value in ret+0(FP).
                    // Correct sequence for 64-bit Assembly:
                    // EDX is high, EAX is low.
                    // Shift RDX left 32 bits (since EDX is lower 32 of RDX, but RDTSC writes to 32-bit registers)
                    // Wait, RDTSC clears high 32 bits of RDX and RAX.
    
    // Correct logic:
    // Move EDX to RDX (zero extend happens automatically on 32-bit write? No, RDTSC writes EDX:EAX).
    // Actually simplicity:
    // RDX = EDX << 32
    // RAX = EAX | RDX
    
    SHLQ    $32, DX
    ORQ     DX, AX
    MOVQ    AX, ret+0(FP)
    RET

// func Pause()
TEXT ·Pause(SB), NOSPLIT, $0
    PAUSE
    RET
