//go:build !linux

package ebpf

import "testing"

func TestStreamTrackerStub(t *testing.T) {
	comps := make(chan Completion, 1)
	updates := make(chan LiveUpdate, 1)
	tr := newStreamTracker(comps, updates)
	if tr == nil {
		t.Fatal("newStreamTracker returned nil")
	}
	tr.handleEvent(&tcpEvent{}) // no-op, should not panic
}
