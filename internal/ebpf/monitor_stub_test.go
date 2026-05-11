//go:build !linux

package ebpf

import (
	"context"
	"testing"
)

func TestAvailableReturnsFalse(t *testing.T) {
	if Available() {
		t.Error("Available() should return false on non-Linux")
	}
}

func TestMonitorNewNotNil(t *testing.T) {
	comps := make(chan Completion, 1)
	updates := make(chan LiveUpdate, 1)
	m := New("lo", comps, updates)
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestMonitorRunReturnsError(t *testing.T) {
	comps := make(chan Completion, 1)
	updates := make(chan LiveUpdate, 1)
	m := New("lo", comps, updates)
	err := m.Run(context.Background())
	if err == nil {
		t.Error("Run() should return an error on non-Linux")
	}
}
