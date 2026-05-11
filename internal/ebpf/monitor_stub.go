//go:build !linux

package ebpf

import (
	"context"
	"errors"
)

// Monitor is a no-op on non-Linux platforms.
type Monitor struct{}

func New(_ string, _ chan<- Completion, _ chan<- LiveUpdate) *Monitor {
	return &Monitor{}
}

func Available() bool { return false }

func (m *Monitor) Run(_ context.Context) error {
	return errors.New("eBPF monitoring requires Linux")
}
