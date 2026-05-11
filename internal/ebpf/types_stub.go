//go:build !linux

package ebpf

const maxPayload = 4096 //nolint:unused

type tcpEvent struct { //nolint:unused
	SrcIP   uint32
	DstIP   uint32
	SrcPort uint16
	DstPort uint16
	Seq     uint32
	Fin     uint8
	Pad     [3]uint8
	DataLen uint16
	Data    [maxPayload]byte
}
