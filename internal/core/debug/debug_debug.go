//go:build debug
// +build debug

package debug

import (
	"fmt"
	"log"
	"runtime"
	"time"
)

func init() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)
}

// Enabled returns whether debug logging is enabled.
// In debug build, always returns true.
func Enabled() bool {
	return true
}

// Logf logs a formatted message with goroutine ID and timestamp.
// In debug build, this outputs detailed logging information.
func Logf(format string, args ...interface{}) {
	_, file, line, _ := runtime.Caller(1)
	goroutineID := getGoroutineID()
	timestamp := time.Now().Format("15:04:05.000000")
	log.Printf("[%s] [goroutine %d] %s:%d: %s", timestamp, goroutineID, file, line, fmt.Sprintf(format, args...))
}

// getGoroutineID returns the current goroutine ID.
func getGoroutineID() int64 {
	buf := make([]byte, 64)
	n := runtime.Stack(buf, false)
	id := int64(-1)
	fmt.Sscanf(string(buf[:n]), "goroutine %d", &id)
	return id
}

