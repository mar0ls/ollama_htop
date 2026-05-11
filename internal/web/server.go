// Package web serves the ollamaHtop web dashboard (HTML + JSON API + SSE).
package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"ollamaHtop/internal/store"
)

type Server struct {
	addr string

	mu      sync.RWMutex
	current store.View
	clients map[chan []byte]struct{}
}

var dashboardTmpl = template.Must(template.New("dashboard").Parse(dashboardHTML))

func New(addr string) *Server {
	return &Server{
		addr:    addr,
		clients: make(map[chan []byte]struct{}),
	}
}

// Push stores the latest view and broadcasts it to all connected SSE clients.
func (s *Server) Push(v store.View) {
	s.mu.Lock()
	s.current = v
	data, _ := json.Marshal(viewToJSON(v))
	clients := make([]chan []byte, 0, len(s.clients))
	for ch := range s.clients {
		clients = append(clients, ch)
	}
	s.mu.Unlock()

	for _, ch := range clients {
		select {
		case ch <- data:
		default:
		}
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("web: listen on %s: %w", s.addr, err)
	}
	slog.Info("web server started", "addr", "http://"+s.addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/api/metrics", s.handleMetrics)
	mux.HandleFunc("/api/events", s.handleSSE)

	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("web: server error: %w", err)
	}
	return nil
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboardTmpl.Execute(w, nil)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	v := s.current
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(viewToJSON(v))
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 4)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	s.mu.RLock()
	initial, _ := json.Marshal(viewToJSON(s.current))
	s.mu.RUnlock()
	bw := bufio.NewWriter(w)
	_, _ = bw.WriteString("data: " + string(initial) + "\n\n")
	_ = bw.Flush()
	flusher.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = bw.WriteString(": ping\n\n")
			_ = bw.Flush()
			flusher.Flush()
		case data, ok := <-ch:
			if !ok {
				return
			}
			_, _ = bw.WriteString("data: " + string(data) + "\n\n")
			_ = bw.Flush()
			flusher.Flush()
		}
	}
}

// ── JSON serialisation ────────────────────────────────────────────────────────

type viewJSON struct {
	Timestamp  string      `json:"timestamp"`
	Connected  bool        `json:"connected"`
	Version    string      `json:"version"`
	HasCapture bool        `json:"has_capture"`
	Models     []modelJSON `json:"models"`
	System     systemJSON  `json:"system"`
	Perf       perfJSON    `json:"perf"`
}

type modelJSON struct {
	Name           string  `json:"name"`
	SizeMB         float64 `json:"size_mb"`
	VRAMUsedMB     float64 `json:"vram_used_mb"`
	Status         string  `json:"status"`
	OutputTPS      float64 `json:"output_tps"`
	InputTPS       float64 `json:"input_tps"`
	TTFT_ms        float64 `json:"ttft_ms"`
	UntilExpirySec float64 `json:"until_expiry_sec"`
	ActiveCount    int     `json:"active_count"`
	LastTotalDurMs float64 `json:"last_total_dur_ms"`
	LastMsPerToken float64 `json:"last_ms_per_token"`
}

type systemJSON struct {
	CPUPercent float64 `json:"cpu_percent"`
	CPUTempC   float64 `json:"cpu_temp_c"`
	GPUAvail   bool    `json:"gpu_avail"`
	GPUName    string  `json:"gpu_name"`
	GPUPercent float64 `json:"gpu_percent"`
	GPUTempC   float64 `json:"gpu_temp_c"`
	GPUPowerW  float64 `json:"gpu_power_w"`
	TokPerWatt float64 `json:"tok_per_watt"`
	MemUsedMB  float64 `json:"mem_used_mb"`
	MemTotalMB float64 `json:"mem_total_mb"`
	MemPercent float64 `json:"mem_percent"`
	LoadAvg1   float64 `json:"load_avg_1"`
	LoadAvg5   float64 `json:"load_avg_5"`
	LoadAvg15  float64 `json:"load_avg_15"`
	Hostname   string  `json:"hostname"`
	OSVersion  string  `json:"os_version"`
}

type perfJSON struct {
	OutputTPS         float64   `json:"output_tps"`
	InputTPS          float64   `json:"input_tps"`
	PeakOutputTPS     float64   `json:"peak_output_tps"`
	PeakInputTPS      float64   `json:"peak_input_tps"`
	OutputHistory     []float64 `json:"output_history"`
	InputHistory      []float64 `json:"input_history"`
	CompletionsPerSec float64   `json:"completions_per_sec"`
	MeanLatencyMs     float64   `json:"mean_latency_ms"`
	P95LatencyMs      float64   `json:"p95_latency_ms"`
	P99LatencyMs      float64   `json:"p99_latency_ms"`
	MeanMsPerToken    float64   `json:"mean_ms_per_token"`
}

func viewToJSON(v store.View) viewJSON {
	models := make([]modelJSON, len(v.Models))
	for i, m := range v.Models {
		tps := m.LiveOutputTPS
		if tps == 0 {
			tps = m.OutputTPS
		}
		models[i] = modelJSON{
			Name:           m.Name,
			SizeMB:         float64(m.SizeBytes) / (1 << 20),
			VRAMUsedMB:     float64(m.VRAMBytes) / (1 << 20),
			Status:         m.Status,
			OutputTPS:      tps,
			InputTPS:       m.InputTPS,
			TTFT_ms:        float64(m.FirstTokenIn.Microseconds()) / 1000.0,
			UntilExpirySec: m.UntilExpiry.Seconds(),
			ActiveCount:    m.ActiveCount,
			LastTotalDurMs: float64(m.LastTotalDuration.Microseconds()) / 1000.0,
			LastMsPerToken: m.LastMsPerToken,
		}
	}
	sys := v.Sys
	p := v.Perf
	return viewJSON{
		Timestamp:  v.At.Format(time.RFC3339),
		Connected:  v.Connected,
		Version:    v.Version,
		HasCapture: v.HasCapture,
		Models:     models,
		System: systemJSON{
			CPUPercent: sys.CPUPercent,
			CPUTempC:   sys.CPUTempC,
			GPUAvail:   sys.GPUAvail,
			GPUName:    sys.GPUName,
			GPUPercent: sys.GPUPercent,
			GPUTempC:   sys.GPUTempC,
			GPUPowerW:  sys.GPUPowerW,
			TokPerWatt: sys.TokPerWatt,
			MemUsedMB:  float64(sys.MemUsedB) / (1 << 20),
			MemTotalMB: float64(sys.MemTotalB) / (1 << 20),
			MemPercent: sys.MemPercent,
			LoadAvg1:   sys.LoadAvg1,
			LoadAvg5:   sys.LoadAvg5,
			LoadAvg15:  sys.LoadAvg15,
			Hostname:   sys.Hostname,
			OSVersion:  sys.OSVersion,
		},
		Perf: perfJSON{
			OutputTPS:         p.OutputTPS,
			InputTPS:          p.InputTPS,
			PeakOutputTPS:     p.PeakOutputTPS,
			PeakInputTPS:      p.PeakInputTPS,
			OutputHistory:     p.OutputHistory,
			InputHistory:      p.InputHistory,
			CompletionsPerSec: p.CompletionsPerSec,
			MeanLatencyMs:     float64(p.MeanLatency.Milliseconds()),
			P95LatencyMs:      float64(p.P95Latency.Milliseconds()),
			P99LatencyMs:      float64(p.P99Latency.Milliseconds()),
			MeanMsPerToken:    p.MeanMsPerToken,
		},
	}
}
