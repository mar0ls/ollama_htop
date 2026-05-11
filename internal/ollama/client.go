package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Poller periodically queries the Ollama API and sends State updates to a channel.
type Poller struct {
	base string
	http *http.Client
}

func NewPoller(base string) *Poller {
	return &Poller{
		base: base,
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *Poller) Run(ctx context.Context, interval time.Duration, out chan<- State) {
	var (
		online  bool
		version string
	)

	tick := time.NewTicker(interval)
	defer tick.Stop()

	fetch := func() {
		models, err := p.RunningModels(ctx)
		if err != nil {
			if online {
				slog.Warn("ollama unreachable", "err", err)
			}
			online = false
			out <- State{Err: err, At: time.Now()}
			return
		}
		if !online {
			if v, err := p.ServerVersion(ctx); err == nil {
				version = v
				slog.Info("ollama connected", "version", version)
			}
			online = true
		}
		out <- State{Models: models, Connected: true, Version: version, At: time.Now()}
	}

	fetch()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			fetch()
		}
	}
}

func (p *Poller) RunningModels(ctx context.Context) ([]LoadedModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/api/ps", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch /api/ps: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("/api/ps returned %d: %s", resp.StatusCode, body)
	}
	var ps struct {
		Models []LoadedModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ps); err != nil {
		return nil, fmt.Errorf("decode /api/ps: %w", err)
	}
	return ps.Models, nil
}

func (p *Poller) ServerVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/api/version", nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch /api/version: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("/api/version returned %d: %s", resp.StatusCode, body)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", fmt.Errorf("decode /api/version: %w", err)
	}
	return v.Version, nil
}
