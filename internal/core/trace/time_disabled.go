//go:build !trace && !profile

package trace

func GetCPUCycles() float64 {
	return 0
}
