package store

import (
	"ollamaHtop/internal/ebpf"
	"ollamaHtop/internal/ollama"
	"ollamaHtop/internal/sysinfo"
	"testing"
	"time"
)

func TestPctEmpty(t *testing.T) {
	if got := pct(nil, 95); got != 0 {
		t.Errorf("pct(nil) = %v, want 0", got)
	}
}

func TestPctBasic(t *testing.T) {
	in := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	cases := []struct {
		p    float64
		want float64
	}{
		{50, 50},
		{95, 100},
		{99, 100},
		{0, 10},
	}
	for _, c := range cases {
		if got := pct(in, c.p); got != c.want {
			t.Errorf("pct(%.0f) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestRingRecordAndHistory(t *testing.T) {
	var r ring
	now := time.Unix(1_000_000_000, 0)

	r.record(10, now)
	r.record(20, now)
	r.record(30, now.Add(5*time.Second))

	hist := r.history(now.Add(5 * time.Second))
	if len(hist) != ringSlots {
		t.Fatalf("history len = %d, want %d", len(hist), ringSlots)
	}
	// newest slot
	if hist[ringSlots-1] != 30 {
		t.Errorf("newest = %v, want 30", hist[ringSlots-1])
	}
	// previous slot — average of 10 and 20
	if hist[ringSlots-2] != 15 {
		t.Errorf("previous = %v, want 15", hist[ringSlots-2])
	}
}

func TestRingPeak(t *testing.T) {
	var r ring
	now := time.Unix(1_000_000_000, 0)
	r.record(5, now)
	r.record(50, now.Add(5*time.Second))
	r.record(15, now.Add(10*time.Second))

	if got := r.peak(now.Add(10 * time.Second)); got != 50 {
		t.Errorf("peak = %v, want 50", got)
	}
}

func TestActiveBuckets(t *testing.T) {
	start := time.Unix(1_000_000_000, 0)
	if got := activeBuckets(start, start); got != 1 {
		t.Errorf("at t=0 buckets = %d, want 1", got)
	}
	if got := activeBuckets(start, start.Add(20*time.Second)); got != 5 {
		t.Errorf("at t=20s buckets = %d, want 5", got)
	}
	if got := activeBuckets(start, start.Add(time.Hour)); got != ringSlots {
		t.Errorf("at t=1h buckets = %d, want %d", got, ringSlots)
	}
}

func TestRingWindowStart(t *testing.T) {
	var r ring
	start := time.Unix(1_000_000_000, 0)
	now := start.Add(10 * time.Second)
	// app just started → window begins at appStart
	ws := r.windowStart(start, now)
	if !ws.Equal(start) {
		t.Errorf("windowStart with fresh app = %v, want %v", ws, start)
	}
	// app running for longer than the ring window → clamped to window limit
	oldStart := now.Add(-time.Duration((ringSlots+10)*slotSecs) * time.Second)
	ws2 := r.windowStart(oldStart, now)
	want := now.Add(-time.Duration(ringSlots*slotSecs) * time.Second)
	if !ws2.Equal(want) {
		t.Errorf("windowStart with old app = %v, want %v", ws2, want)
	}
}

// ── Store integration tests ───────────────────────────────────────────────────

func TestStoreNew(t *testing.T) {
	s := New(false)
	v := s.Snapshot()
	if v.HasCapture {
		t.Error("HasCapture should be false")
	}
	if v.Connected {
		t.Error("Connected should be false on empty store")
	}
	if len(v.Models) != 0 {
		t.Errorf("expected 0 models, got %d", len(v.Models))
	}
}

func TestStoreNewWithCapture(t *testing.T) {
	s := New(true)
	v := s.Snapshot()
	if !v.HasCapture {
		t.Error("HasCapture should be true")
	}
}

func TestStorePushCompletion(t *testing.T) {
	s := New(false)
	c := ebpf.Completion{
		Model:          "llama3",
		OutputTokens:   100,
		OutputDuration: time.Second,
		InputTokens:    50,
		InputDuration:  500 * time.Millisecond,
		TotalDuration:  2 * time.Second,
	}
	s.PushCompletion(c)

	v := s.Snapshot()
	if len(v.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(v.Models))
	}
	m := v.Models[0]
	if m.Name != "llama3" {
		t.Errorf("model name = %q, want llama3", m.Name)
	}
	if m.OutputTPS == 0 {
		t.Error("OutputTPS should be non-zero")
	}
}

func TestStorePushCompletionLatencyStats(t *testing.T) {
	s := New(false)
	for i := 0; i < 10; i++ {
		s.PushCompletion(ebpf.Completion{
			Model:          "model-a",
			TotalDuration:  time.Duration(i+1) * time.Second,
			OutputTokens:   int64(i + 1),
			OutputDuration: time.Duration(i+1) * time.Second,
		})
	}
	v := s.Snapshot()
	if v.Perf.MeanLatency == 0 {
		t.Error("MeanLatency should be non-zero after completions")
	}
	if v.Perf.P95Latency == 0 {
		t.Error("P95Latency should be non-zero")
	}
	if v.Perf.CompletionsPerSec == 0 {
		t.Error("CompletionsPerSec should be non-zero")
	}
}

func TestStorePushCompletionNoTokens(t *testing.T) {
	// Completion with no output tokens — should not panic, msPerTok = 0.
	s := New(false)
	s.PushCompletion(ebpf.Completion{
		Model:         "model-b",
		TotalDuration: time.Second,
	})
	v := s.Snapshot()
	if len(v.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(v.Models))
	}
}

func TestStorePushLiveUpdateRunning(t *testing.T) {
	s := New(false)
	// First put the model in ollama state so it appears via the API path.
	s.SetOllamaState(ollama.State{
		Connected: true,
		Models:    []ollama.LoadedModel{{Name: "llama3"}},
	})
	s.PushLiveUpdate(ebpf.LiveUpdate{
		Model:     "llama3",
		ConnKey:   1,
		OutputTPS: 42,
		Streaming: true,
	})
	v := s.Snapshot()
	if len(v.Models) == 0 {
		t.Fatal("expected at least 1 model")
	}
	m := v.Models[0]
	if m.Status != "running" {
		t.Errorf("status = %q, want running", m.Status)
	}
	if m.LiveOutputTPS == 0 {
		t.Errorf("LiveOutputTPS should be non-zero")
	}
}

func TestStorePushLiveUpdateThinking(t *testing.T) {
	s := New(false)
	s.SetOllamaState(ollama.State{
		Connected: true,
		Models:    []ollama.LoadedModel{{Name: "deepseek"}},
	})
	s.PushLiveUpdate(ebpf.LiveUpdate{
		Model:       "deepseek",
		ConnKey:     2,
		OutputTPS:   10,
		Streaming:   true,
		Stage:       ebpf.StageThinking,
		ThinkTokens: 50,
	})
	v := s.Snapshot()
	if len(v.Models) == 0 {
		t.Fatal("expected at least 1 model")
	}
	m := v.Models[0]
	if m.Status != "thinking" {
		t.Errorf("status = %q, want thinking", m.Status)
	}
	if m.ThinkTokens != 50 {
		t.Errorf("ThinkTokens = %d, want 50", m.ThinkTokens)
	}
}

func TestStorePushLiveUpdateTTFT(t *testing.T) {
	s := New(false)
	s.SetOllamaState(ollama.State{
		Connected: true,
		Models:    []ollama.LoadedModel{{Name: "phi3"}},
	})
	s.PushLiveUpdate(ebpf.LiveUpdate{
		Model:            "phi3",
		ConnKey:          3,
		Streaming:        true,
		TimeToFirstToken: 250 * time.Millisecond,
	})
	v := s.Snapshot()
	if len(v.Models) == 0 {
		t.Fatal("expected at least 1 model")
	}
	if v.Models[0].FirstTokenIn != 250*time.Millisecond {
		t.Errorf("FirstTokenIn = %v, want 250ms", v.Models[0].FirstTokenIn)
	}
}

func TestStorePushLiveUpdateNonStreaming(t *testing.T) {
	// Non-streaming update should remove the connection from liveConns.
	s := New(false)
	s.PushLiveUpdate(ebpf.LiveUpdate{
		Model:     "model-x",
		ConnKey:   99,
		Streaming: true,
	})
	s.PushLiveUpdate(ebpf.LiveUpdate{
		Model:     "model-x",
		ConnKey:   99,
		Streaming: false,
	})
	// liveConns should be empty now — nothing from model-x in live view.
	// model-x also not in ollama state, so Snapshot models = 0.
	v := s.Snapshot()
	if len(v.Models) != 0 {
		t.Errorf("expected 0 models after non-streaming update, got %d", len(v.Models))
	}
}

func TestStoreSetOllamaState(t *testing.T) {
	s := New(false)
	s.SetOllamaState(ollama.State{
		Connected: true,
		Version:   "0.22.1",
		Models:    []ollama.LoadedModel{{Name: "gemma", Size: 1 << 20}},
	})
	v := s.Snapshot()
	if !v.Connected {
		t.Error("Connected should be true")
	}
	if v.Version != "0.22.1" {
		t.Errorf("Version = %q, want 0.22.1", v.Version)
	}
	if len(v.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(v.Models))
	}
	if v.Models[0].Name != "gemma" {
		t.Errorf("model name = %q, want gemma", v.Models[0].Name)
	}
}

func TestStoreModelIdleStatus(t *testing.T) {
	s := New(false)
	s.SetOllamaState(ollama.State{
		Connected: true,
		Models:    []ollama.LoadedModel{{Name: "idle-model"}},
	})
	v := s.Snapshot()
	if len(v.Models) == 0 {
		t.Fatal("expected 1 model")
	}
	if v.Models[0].Status != "idle" {
		t.Errorf("status = %q, want idle", v.Models[0].Status)
	}
}

func TestStoreModelRunningFromCompletion(t *testing.T) {
	// Model has a recent completion — should appear as running (within staleWindow).
	s := New(false)
	s.SetOllamaState(ollama.State{
		Connected: true,
		Models:    []ollama.LoadedModel{{Name: "llama3"}},
	})
	s.PushCompletion(ebpf.Completion{
		Model:          "llama3",
		OutputTokens:   10,
		OutputDuration: time.Second,
		TotalDuration:  2 * time.Second,
	})
	v := s.Snapshot()
	if len(v.Models) == 0 {
		t.Fatal("expected 1 model")
	}
	if v.Models[0].Status != "running" {
		t.Errorf("status = %q, want running", v.Models[0].Status)
	}
}

func TestStoreSetSysInfo(t *testing.T) {
	s := New(false)
	s.SetSysInfo(sysinfo.Info{
		CPUPercent: 42.5,
		CPUTempC:   65.0,
		GPUTempC:   80.0,
	})
	v := s.Snapshot()
	if v.Sys.CPUPercent != 42.5 {
		t.Errorf("CPUPercent = %v, want 42.5", v.Sys.CPUPercent)
	}
}

func TestStoreSetSysInfoZeroTemp(t *testing.T) {
	// Zero-temp should not be recorded in rings (no panic).
	s := New(false)
	s.SetSysInfo(sysinfo.Info{CPUTempC: 0, GPUTempC: 0})
	v := s.Snapshot()
	_ = v // no panic
}

func TestStoreTokPerWattCalculation(t *testing.T) {
	s := New(false)
	s.SetOllamaState(ollama.State{
		Connected: true,
		Models:    []ollama.LoadedModel{{Name: "model-w"}},
	})
	s.SetSysInfo(sysinfo.Info{GPUPowerW: 100})
	s.PushLiveUpdate(ebpf.LiveUpdate{
		Model:     "model-w",
		ConnKey:   7,
		OutputTPS: 50,
		Streaming: true,
	})
	v := s.Snapshot()
	if v.Sys.TokPerWatt == 0 {
		t.Error("TokPerWatt should be non-zero when GPU power and output TPS are set")
	}
}

func TestStoreModelExpiry(t *testing.T) {
	s := New(false)
	s.SetOllamaState(ollama.State{
		Connected: true,
		Models: []ollama.LoadedModel{{
			Name:      "expiring-model",
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}},
	})
	v := s.Snapshot()
	if len(v.Models) == 0 {
		t.Fatal("expected 1 model")
	}
	if v.Models[0].UntilExpiry <= 0 {
		t.Error("UntilExpiry should be positive for future expiry")
	}
}

func TestStoreModelExpired(t *testing.T) {
	s := New(false)
	s.SetOllamaState(ollama.State{
		Connected: true,
		Models: []ollama.LoadedModel{{
			Name:      "expired-model",
			ExpiresAt: time.Now().Add(-5 * time.Minute), // past
		}},
	})
	v := s.Snapshot()
	if len(v.Models) == 0 {
		t.Fatal("expected 1 model")
	}
	if v.Models[0].UntilExpiry != 0 {
		t.Errorf("UntilExpiry = %v, want 0 for past expiry", v.Models[0].UntilExpiry)
	}
}

func TestStorePruneLatencies(t *testing.T) {
	s := New(false)
	// Inject stale latency entries directly.
	old := time.Now().Add(-(latWindow + time.Minute))
	s.mu.Lock()
	s.latencies = []latEntry{
		{totalDur: time.Second, at: old},
		{totalDur: 2 * time.Second, at: old},
	}
	s.mu.Unlock()
	// Snapshot should prune them.
	v := s.Snapshot()
	if v.Perf.MeanLatency != 0 {
		t.Errorf("expected 0 mean latency after pruning, got %v", v.Perf.MeanLatency)
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	s := New(true)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			s.PushCompletion(ebpf.Completion{Model: "m", TotalDuration: time.Second, OutputTokens: 1, OutputDuration: time.Second})
		}
		close(done)
	}()
	for i := 0; i < 50; i++ {
		_ = s.Snapshot()
	}
	<-done
}

func TestStorePushLiveUpdateMultipleConns(t *testing.T) {
	s := New(false)
	s.SetOllamaState(ollama.State{
		Connected: true,
		Models:    []ollama.LoadedModel{{Name: "big-model"}},
	})
	// Two simultaneous connections on the same model.
	s.PushLiveUpdate(ebpf.LiveUpdate{Model: "big-model", ConnKey: 10, OutputTPS: 20, Streaming: true})
	s.PushLiveUpdate(ebpf.LiveUpdate{Model: "big-model", ConnKey: 11, OutputTPS: 30, Streaming: true})
	v := s.Snapshot()
	if len(v.Models) == 0 {
		t.Fatal("expected 1 model")
	}
	m := v.Models[0]
	if m.ActiveCount != 2 {
		t.Errorf("ActiveCount = %d, want 2", m.ActiveCount)
	}
	if m.LiveOutputTPS != 50 {
		t.Errorf("LiveOutputTPS = %v, want 50", m.LiveOutputTPS)
	}
}
