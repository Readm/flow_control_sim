package main

// AddressMapper maps addresses to target slave nodes.
type AddressMapper interface {
	TargetSlave(addr uint64) *SlaveNode
}

// RingAddressInterleaver maps cache lines to slaves by interleaving lines across targets.
type RingAddressInterleaver struct {
	slaves []*SlaveNode
	stride uint64
	lineSz uint64
}

// NewRingAddressInterleaver builds an address mapper that stripes cache lines across slaves.
func NewRingAddressInterleaver(slaves []*SlaveNode, stride int) *RingAddressInterleaver {
	if stride <= 0 {
		stride = DefaultRingInterleaveStride
	}
	return &RingAddressInterleaver{
		slaves: slaves,
		stride: uint64(stride),
		lineSz: DefaultCacheLineSize,
	}
}

// TargetSlave returns the slave handling the supplied address.
func (m *RingAddressInterleaver) TargetSlave(addr uint64) *SlaveNode {
	if m == nil || len(m.slaves) == 0 {
		return nil
	}
	line := addr / m.lineSz
	index := (line / m.stride) % uint64(len(m.slaves))
	return m.slaves[index]
}
