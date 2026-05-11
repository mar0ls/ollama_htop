package ebpf

import (
	"testing"
	"time"
)

func TestCompletionOutputTPS(t *testing.T) {
	c := Completion{
		OutputTokens:   100,
		OutputDuration: time.Second,
	}
	if got := c.OutputTPS(); got != 100 {
		t.Errorf("OutputTPS = %v, want 100", got)
	}
}

func TestCompletionOutputTPSZeroDuration(t *testing.T) {
	c := Completion{OutputTokens: 100, OutputDuration: 0}
	if got := c.OutputTPS(); got != 0 {
		t.Errorf("OutputTPS zero duration = %v, want 0", got)
	}
}

func TestCompletionInputTPS(t *testing.T) {
	c := Completion{
		InputTokens:   50,
		InputDuration: 500 * time.Millisecond,
	}
	if got := c.InputTPS(); got != 100 {
		t.Errorf("InputTPS = %v, want 100", got)
	}
}

func TestCompletionInputTPSZeroDuration(t *testing.T) {
	c := Completion{InputTokens: 50, InputDuration: 0}
	if got := c.InputTPS(); got != 0 {
		t.Errorf("InputTPS zero duration = %v, want 0", got)
	}
}
