//go:build !trace

package link

// No-ops for non-trace builds

func (l *Link) traceReceiveStart() float64                                { return 0 }
func (l *Link) traceReceiveEnd(start float64, cycle int, packetCount int) {}

func (l *Link) traceProcessStart() float64               { return 0 }
func (l *Link) traceProcessEnd(start float64, cycle int) {}

func (l *Link) traceSendStart() float64               { return 0 }
func (l *Link) traceSendEnd(start float64, cycle int) {}
