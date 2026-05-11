//go:build linux

package ebpf

// maxPayload must match MAX_PAYLOAD in tc_ollama.c.
const maxPayload = 4096

// tcpEvent mirrors the C struct tcp_event from tc_ollama.c.
// Field names / layout are fixed — they must match the compiled BPF object.
type tcpEvent struct {
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
