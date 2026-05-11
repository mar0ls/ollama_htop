//go:build !linux

package ebpf

type streamTracker struct{} //nolint:unused

func newStreamTracker(_ chan<- Completion, _ chan<- LiveUpdate) *streamTracker { //nolint:unused
	return &streamTracker{}
}

func (t *streamTracker) handleEvent(_ *tcpEvent) {} //nolint:unused
