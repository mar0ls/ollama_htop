package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ollamaHtop/internal/store"
	"ollamaHtop/internal/sysinfo"
)

// ── handleDashboard ───────────────────────────────────────────────────────────

func TestHandleDashboard(t *testing.T) {
	srv := New("127.0.0.1:0")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.handleDashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<html") && !strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("response does not look like HTML: %.100s", body)
	}
}

func TestHandleDashboard404(t *testing.T) {
	srv := New("127.0.0.1:0")
	req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	rec := httptest.NewRecorder()
	srv.handleDashboard(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ── handleMetrics ─────────────────────────────────────────────────────────────

func TestHandleMetrics(t *testing.T) {
	srv := New("127.0.0.1:0")
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	rec := httptest.NewRecorder()
	srv.handleMetrics(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cors := rec.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("CORS header = %q, want *", cors)
	}
	var out viewJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
}

func TestHandleMetricsAfterPush(t *testing.T) {
	srv := New("127.0.0.1:0")
	v := store.View{
		At:        time.Now(),
		Connected: true,
		Version:   "0.22.1",
		Models: []store.ModelView{
			{
				Name:      "llama3",
				SizeBytes: 4 << 20,
				Status:    "running",
			},
		},
		Perf: store.PerfStats{
			OutputTPS: 42,
			InputTPS:  10,
		},
		Sys: sysinfo.Info{
			CPUPercent: 30,
			MemUsedB:   1 << 30,
			MemTotalB:  8 << 30,
		},
	}
	srv.Push(v)

	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	rec := httptest.NewRecorder()
	srv.handleMetrics(rec, req)

	var out viewJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if !out.Connected {
		t.Error("connected should be true")
	}
	if out.Version != "0.22.1" {
		t.Errorf("version = %q, want 0.22.1", out.Version)
	}
	if len(out.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(out.Models))
	}
	if out.Models[0].Name != "llama3" {
		t.Errorf("model name = %q, want llama3", out.Models[0].Name)
	}
	if out.Perf.OutputTPS != 42 {
		t.Errorf("output_tps = %v, want 42", out.Perf.OutputTPS)
	}
}

// ── handleSSE ─────────────────────────────────────────────────────────────────

func TestHandleSSEHeaders(t *testing.T) {
	srv := New("127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleSSE(rec, req)
	}()

	// Cancel after a short time to unblock the SSE handler.
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cors := rec.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("CORS header = %q, want *", cors)
	}
}

func TestHandleSSEInitialData(t *testing.T) {
	srv := New("127.0.0.1:0")
	v := store.View{At: time.Now(), Connected: true, Version: "1.0"}
	srv.Push(v)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleSSE(rec, req)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.HasPrefix(body, "data: ") {
		t.Errorf("SSE response should start with 'data: ', got: %.80s", body)
	}
	// Parse the first SSE line's JSON.
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var out viewJSON
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("SSE data is not valid JSON: %v (%s)", err, raw)
		}
		if !out.Connected {
			t.Error("SSE initial data: connected should be true")
		}
		break
	}
}

// ── Push broadcasting ─────────────────────────────────────────────────────────

func TestPushBroadcast(t *testing.T) {
	srv := New("127.0.0.1:0")

	// Register a fake client channel.
	ch := make(chan []byte, 4)
	srv.mu.Lock()
	srv.clients[ch] = struct{}{}
	srv.mu.Unlock()

	v := store.View{At: time.Now(), Version: "2.0"}
	srv.Push(v)

	select {
	case data := <-ch:
		var out viewJSON
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("broadcast data is not valid JSON: %v", err)
		}
		if out.Version != "2.0" {
			t.Errorf("version = %q, want 2.0", out.Version)
		}
	case <-time.After(time.Second):
		t.Error("Push did not broadcast within 1s")
	}
}

func TestPushDropsWhenClientFull(t *testing.T) {
	srv := New("127.0.0.1:0")

	// A channel with no capacity — Push should not block.
	ch := make(chan []byte) // unbuffered, no reader
	srv.mu.Lock()
	srv.clients[ch] = struct{}{}
	srv.mu.Unlock()

	done := make(chan struct{})
	go func() {
		srv.Push(store.View{At: time.Now()})
		close(done)
	}()
	select {
	case <-done:
		// OK — push returned without blocking
	case <-time.After(time.Second):
		t.Error("Push blocked on full client channel")
	}
}

func TestServerRunCancel(t *testing.T) {
	srv := New("127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- srv.Run(ctx)
	}()

	// Give listener a brief moment to start, then stop it.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
}

// ── viewToJSON ────────────────────────────────────────────────────────────────

func TestViewToJSONEmpty(t *testing.T) {
	out := viewToJSON(store.View{})
	if out.Models == nil {
		t.Error("Models should not be nil")
	}
	if len(out.Models) != 0 {
		t.Errorf("expected 0 models, got %d", len(out.Models))
	}
}

func TestViewToJSONModelFields(t *testing.T) {
	v := store.View{
		At:        time.Now(),
		Connected: true,
		Models: []store.ModelView{
			{
				Name:              "phi3",
				SizeBytes:         1 << 30,   // 1 GiB
				VRAMBytes:         512 << 20, // 512 MiB
				LiveOutputTPS:     77,
				OutputTPS:         10,
				FirstTokenIn:      300 * time.Millisecond,
				UntilExpiry:       5 * time.Minute,
				ActiveCount:       2,
				LastTotalDuration: 2 * time.Second,
				LastMsPerToken:    5.5,
			},
		},
	}
	out := viewToJSON(v)
	if len(out.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(out.Models))
	}
	m := out.Models[0]
	if m.Name != "phi3" {
		t.Errorf("Name = %q, want phi3", m.Name)
	}
	// LiveOutputTPS takes precedence over OutputTPS.
	if m.OutputTPS != 77 {
		t.Errorf("OutputTPS = %v, want 77 (live)", m.OutputTPS)
	}
	// 300 ms = 300000 µs → 300000/1000 = 300.0 ms
	if m.TTFT_ms != 300.0 {
		t.Errorf("TTFT_ms = %v, want 300.0", m.TTFT_ms)
	}
	if m.UntilExpirySec != (5 * time.Minute).Seconds() {
		t.Errorf("UntilExpirySec = %v, want %v", m.UntilExpirySec, (5 * time.Minute).Seconds())
	}
	if m.SizeMB != float64(1<<30)/(1<<20) {
		t.Errorf("SizeMB = %v, want %v", m.SizeMB, float64(1<<30)/(1<<20))
	}
	if m.ActiveCount != 2 {
		t.Errorf("ActiveCount = %d, want 2", m.ActiveCount)
	}
	if m.LastMsPerToken != 5.5 {
		t.Errorf("LastMsPerToken = %v, want 5.5", m.LastMsPerToken)
	}
}

func TestViewToJSONModelFallbackTPS(t *testing.T) {
	// When LiveOutputTPS == 0, fall back to historical OutputTPS.
	v := store.View{
		Models: []store.ModelView{
			{Name: "m", LiveOutputTPS: 0, OutputTPS: 55},
		},
	}
	out := viewToJSON(v)
	if out.Models[0].OutputTPS != 55 {
		t.Errorf("OutputTPS = %v, want 55 (fallback)", out.Models[0].OutputTPS)
	}
}

func TestViewToJSONSystem(t *testing.T) {
	v := store.View{
		Sys: sysinfo.Info{
			CPUPercent: 75.5,
			MemUsedB:   2 << 30,
			MemTotalB:  16 << 30,
			MemPercent: 12.5,
			GPUAvail:   true,
			GPUName:    "RTX 4090",
			LoadAvg1:   1.23,
		},
	}
	out := viewToJSON(v)
	if out.System.CPUPercent != 75.5 {
		t.Errorf("CPUPercent = %v, want 75.5", out.System.CPUPercent)
	}
	if out.System.MemUsedMB != float64(2<<30)/(1<<20) {
		t.Errorf("MemUsedMB = %v", out.System.MemUsedMB)
	}
	if !out.System.GPUAvail {
		t.Error("GPUAvail should be true")
	}
	if out.System.GPUName != "RTX 4090" {
		t.Errorf("GPUName = %q, want RTX 4090", out.System.GPUName)
	}
	if out.System.LoadAvg1 != 1.23 {
		t.Errorf("LoadAvg1 = %v, want 1.23", out.System.LoadAvg1)
	}
}

func TestViewToJSONPerf(t *testing.T) {
	v := store.View{
		Perf: store.PerfStats{
			OutputTPS:         88,
			InputTPS:          12,
			PeakOutputTPS:     100,
			PeakInputTPS:      20,
			OutputHistory:     []float64{1, 2, 3},
			InputHistory:      []float64{4, 5, 6},
			CompletionsPerSec: 0.5,
			MeanLatency:       time.Second,
			P95Latency:        2 * time.Second,
			P99Latency:        3 * time.Second,
			MeanMsPerToken:    7.7,
		},
	}
	out := viewToJSON(v)
	p := out.Perf
	if p.OutputTPS != 88 {
		t.Errorf("OutputTPS = %v, want 88", p.OutputTPS)
	}
	if p.MeanLatencyMs != float64(time.Second.Milliseconds()) {
		t.Errorf("MeanLatencyMs = %v, want %v", p.MeanLatencyMs, float64(time.Second.Milliseconds()))
	}
	if p.P95LatencyMs != float64((2 * time.Second).Milliseconds()) {
		t.Errorf("P95LatencyMs = %v", p.P95LatencyMs)
	}
	if len(p.OutputHistory) != 3 {
		t.Errorf("OutputHistory len = %d, want 3", len(p.OutputHistory))
	}
}

func TestViewToJSONTimestamp(t *testing.T) {
	ts := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	v := store.View{At: ts}
	out := viewToJSON(v)
	if out.Timestamp != "2024-01-15T12:00:00Z" {
		t.Errorf("Timestamp = %q, want 2024-01-15T12:00:00Z", out.Timestamp)
	}
}
