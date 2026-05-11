// Package store holds all runtime state and produces View snapshots for the UI.
package store

import (
	"time"

	"ollamaHtop/internal/sysinfo"
)

type View struct {
	At         time.Time
	Connected  bool
	Version    string
	HasCapture bool
	Models     []ModelView
	Perf       PerfStats
	Sys        sysinfo.Info
}

type ModelView struct {
	Name          string
	SizeBytes     int64
	VRAMBytes     int64
	LiveOutputTPS float64
	OutputTPS     float64
	InputTPS      float64
	Status        string // "running" | "thinking" | "idle"
	UntilExpiry   time.Duration
	FirstTokenIn  time.Duration // TTFT
	FirstRespIn   time.Duration // TTFR — first response token after thinking
	ActiveCount   int

	ThinkTokens    int64
	ThinkElapsed   time.Duration
	ThinkTPS       float64
	ResponseTokens int64
	ResponseTPS    float64

	LastTotalDuration time.Duration
	LastMsPerToken    float64
}

type PerfStats struct {
	OutputTPS     float64
	InputTPS      float64
	PeakOutputTPS float64
	PeakInputTPS  float64
	OutputHistory []float64 // 60 buckets × 5 s = 5-min window
	InputHistory  []float64
	PeakBuckets   int // how many buckets have data so far

	CompletionsPerSec float64
	MeanLatency       time.Duration
	P95Latency        time.Duration
	P99Latency        time.Duration
	MeanMsPerToken    float64
	WindowStart       time.Time
}
