//go:build !linux

package sysinfo

import "testing"

func TestCollectStaticReturnsStruct(t *testing.T) {
	info := CollectStatic()
	// On non-Linux the stub returns a zero-value struct — just ensure no panic.
	_ = info
}

func TestCollectReturnsStruct(t *testing.T) {
	info := Collect()
	_ = info
}

func TestCollectGPUStub(t *testing.T) {
	g := collectGPU()
	if g.available {
		t.Error("stub collectGPU should return not available")
	}
}
