package benchmarks

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/pprof"
	"strconv"
	"strings"
)

// Profiler manages runtime CPU profiling and analysis
type Profiler struct {
	file *os.File
	path string
}

// StartProfiling starts a CPU profile saving to a temp file
func StartProfiling(name string) (*Profiler, error) {
	// Only run if specific env var is set
	if os.Getenv("PROFILE") != "true" {
		return nil, nil // No-op
	}

	f, err := os.CreateTemp("", fmt.Sprintf("cpu_%s_*.prof", name))
	if err != nil {
		return nil, err
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return nil, err
	}

	return &Profiler{
		file: f,
		path: f.Name(),
	}, nil
}

// StopAndAnalyze stops profiling, runs analysis, and prints breakdown
func (p *Profiler) StopAndAnalyze() {
	if p == nil {
		return
	}
	pprof.StopCPUProfile()
	p.file.Close()
	defer os.Remove(p.path) // Cleanup temp file

	// Run go tool pprof to convert to text
	// We need the current executable path for pprof to symbolize
	execPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Profiler Error: cannot get executable path: %v\n", err)
		return
	}

	cmd := exec.Command("go", "tool", "pprof", "-top", "-cum=false", "-nodecount=100000", execPath, p.path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: If 'go' is not in path or other issue
		fmt.Printf("Profiler Analysis Skipped: 'go tool pprof' failed: %v\n", err)
		return
	}

	// Parse and Analyze
	fmt.Println("----- Top Functions -----")
	lines := strings.Split(string(output), "\n")
	for i := 0; i < 20 && i < len(lines); i++ {
		fmt.Println(lines[i])
	}
	fmt.Println("-------------------------")

	analyzeProfileOutput(string(output))
}

func analyzeProfileOutput(output string) {
	lines := strings.Split(output, "\n")

	var (
		catApp   float64
		catSync  float64
		catGCMem float64
		catSysIO float64
		catOther float64
		totalPct float64
	)

	runtimeGCMem := []string{"gc", "malloc", "heap", "scan", "scavenge", "sweep", "barrier", "writeBarrier"}
	// Expanded sync keywords based on experience
	runtimeSync := []string{"chan", "select", "lock", "unlock", "sem", "schedule", "findrunnable", "gopark", "mcall", "systemstack", "duff", "nanotime", "casgstatus", "exitsyscall", "osyield"}
	runtimeOther := []string{"memmove", "memclr"}

	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) < 6 {
			continue
		}

		if !strings.HasSuffix(parts[1], "%") {
			continue
		}

		pctStr := strings.TrimSuffix(parts[1], "%")
		pct, err := strconv.ParseFloat(pctStr, 64)
		if err != nil {
			continue
		}

		name := strings.Join(parts[5:], " ")

		if strings.HasPrefix(name, "github.com/Readm/flow_sim") {
			catApp += pct
		} else if strings.HasPrefix(name, "runtime.") {
			isGCMem := false
			for _, k := range runtimeGCMem {
				if strings.Contains(name, k) {
					catGCMem += pct
					isGCMem = true
					break
				}
			}
			if isGCMem {
				totalPct += pct
				continue
			}

			isOther := false
			for _, k := range runtimeOther {
				if strings.Contains(name, k) {
					catOther += pct
					isOther = true
					break
				}
			}
			if isOther {
				totalPct += pct
				continue
			}

			isSync := false
			for _, k := range runtimeSync {
				if strings.Contains(name, k) {
					catSync += pct
					isSync = true
					break
				}
			}
			if isSync {
				totalPct += pct
				continue
			}

			// Default unmatched runtime to Sync (safe bet for scheduler bits)
			catSync += pct

		} else if strings.HasPrefix(name, "syscall.") || strings.HasPrefix(name, "internal/poll.") || strings.HasPrefix(name, "os.") {
			catSysIO += pct
		} else {
			catApp += pct
		}

		totalPct += pct
	}

	if totalPct == 0 {
		totalPct = 1.0
	}
	scale := 100.0 / totalPct

	fmt.Println("----- Runtime Profile Breakdown (CPU Active Time Only) -----")
	fmt.Printf("App Logic:    %5.1f%%\n", catApp*scale)
	fmt.Printf("Runtime Sync: %5.1f%% (Schedule, Chan, Lock)\n", catSync*scale)
	fmt.Printf("GC & Memory:  %5.1f%%\n", catGCMem*scale)
	fmt.Printf("System & I/O: %5.1f%%\n", catSysIO*scale)
	fmt.Printf("Data Copy:    %5.1f%% (memmove/memclr)\n", catOther*scale)
	fmt.Println("----------------------------------------------------------")
}
