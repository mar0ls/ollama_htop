package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServerVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"0.6.2"}`))
	}))
	defer srv.Close()

	p := NewPoller(srv.URL)
	ver, err := p.ServerVersion(context.Background())
	if err != nil {
		t.Fatalf("ServerVersion: %v", err)
	}
	if ver != "0.6.2" {
		t.Errorf("got %q, want %q", ver, "0.6.2")
	}
}

func TestRunningModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"models": [{
				"name": "deepseek-r1:8b",
				"size": 23300000000,
				"size_vram": 23300000000,
				"digest": "abc123",
				"details": {
					"family": "deepseek",
					"parameter_size": "8B",
					"quantization_level": "Q4_K_M"
				},
				"expires_at": "2025-01-15T12:30:00Z"
			}]
		}`))
	}))
	defer srv.Close()

	p := NewPoller(srv.URL)
	models, err := p.RunningModels(context.Background())
	if err != nil {
		t.Fatalf("RunningModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	m := models[0]
	if m.Name != "deepseek-r1:8b" {
		t.Errorf("name = %q, want %q", m.Name, "deepseek-r1:8b")
	}
	if m.Details.Quant != "Q4_K_M" {
		t.Errorf("quant = %q, want %q", m.Details.Quant, "Q4_K_M")
	}
	if m.ExpiresAt.IsZero() {
		t.Error("expires_at should not be zero")
	}
}

func TestRunningModels_ConnectionError(t *testing.T) {
	p := NewPoller("http://127.0.0.1:1")
	_, err := p.RunningModels(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestRun_SendsStates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.6.2"}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[]}`))
		}
	}))
	defer srv.Close()

	p := NewPoller(srv.URL)
	ch := make(chan State, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	go p.Run(ctx, 50*time.Millisecond, ch)

	var states []State
	for s := range ch {
		states = append(states, s)
		if len(states) >= 3 {
			cancel()
			break
		}
	}

	if len(states) < 2 {
		t.Fatalf("expected at least 2 states, got %d", len(states))
	}
	for _, s := range states {
		if !s.Connected {
			t.Error("expected Connected=true")
		}
		if s.Version != "0.6.2" {
			t.Errorf("version = %q, want %q", s.Version, "0.6.2")
		}
	}
}

func TestRun_HandlesDisconnect(t *testing.T) {
	p := NewPoller("http://127.0.0.1:1")
	ch := make(chan State, 5)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go p.Run(ctx, 50*time.Millisecond, ch)

	s := <-ch
	if s.Connected {
		t.Error("expected Connected=false for unreachable server")
	}
	if s.Err == nil {
		t.Error("expected non-nil Err")
	}
}
