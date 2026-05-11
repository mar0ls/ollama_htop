# ollamaHtop Architecture

## Overview

`ollamaHtop` has two data sources:

- Ollama API polling (`/api/ps`, `/api/version`)
- optional eBPF capture (TCX) for live tok/s, TTFT, and `<think>` phases

Data flow:

```
Ollama API ──polling──▶ internal/ollama.Poller ──chan State──────┐
                                                                   │
eBPF TC hook (optional) ─▶ Completion + LiveUpdate channels ──────▶ internal/store.Store ──Snapshot──▶ internal/tui
                                                                   │                                      └────────────▶ internal/web (SSE + JSON)
System metrics (/proc,/sys,nvidia-smi) ────────────────────────────┘
```

## Components

### `cmd/ollamaHtop`
- flag parsing (`-host`, `-ebpf`, `-web-port`, `-debug`, `-version`)
- starts all goroutines
- resolves the network interface for eBPF (`resolveEBPFIface`)

### `internal/ollama`
- `Poller` queries Ollama every 1 s
- emits `State` with model list, version, and connection status

### `internal/ebpf`
- Linux-only monitor via TCX (`link.AttachTCX`)
- reads events from ring buffer
- reassembles HTTP/NDJSON stream, computes live TPS, TTFT, TTFR, and `thinking/responding` phases
- no-op stubs on non-Linux platforms

### `internal/store`
- central, thread-safe application state
- merges polling + eBPF + sysinfo
- builds `View` for TUI and web
- computes latency stats (avg/p95/p99), req/s, tok/W

### `internal/sysinfo`
- Linux: CPU / RAM / load / temperatures + GPU (NVIDIA / AMD)
- non-Linux: stubs

### `internal/tui`
- ANSI rendering without external TUI libraries
- 500 ms refresh cycle
- model sorting and throughput/system sections

### `internal/web`
- `GET /` HTML dashboard
- `GET /api/metrics` JSON snapshot
- `GET /api/events` SSE stream — one event per update + heartbeat

## Runtime data flow

1. Poller publishes `ollama.State` to a channel.
2. eBPF monitor (when enabled) publishes `Completion` and `LiveUpdate` events.
3. Main loop in `main` writes data into `store.Store`.
4. On each tick: `Store.Snapshot()`.
5. Snapshot is broadcast concurrently to TUI and web (SSE).

## Requirements

- Linux amd64/arm64 (eBPF requires kernel ≥ 6.6)
- Go 1.24+
- Ollama accessible locally or remotely
- eBPF mode: root or `CAP_NET_ADMIN`

## Implementation notes

- Sparkline: 60 buckets × 5 s = 5-minute window.
- Without eBPF the app still runs (model list + system metrics), but live tok/s and related fields are empty.
- Remote eBPF capture supports IPv4 traffic; IPv6 hosts are rejected at startup.
