package store

import (
	"math"
	"sort"
	"sync"
	"time"

	"ollamaHtop/internal/ebpf"
	"ollamaHtop/internal/ollama"
	"ollamaHtop/internal/sysinfo"
)

const (
	liveWindow  = 2 * time.Second  // live stream freshness cutoff
	staleWindow = 10 * time.Second // completed-eval staleness cutoff
	latWindow   = 5 * time.Minute  // latency sample retention
)

// Store is the central state holder; all methods are goroutine-safe.
// Writers call Push*/Set*, readers call Snapshot().
type Store struct {
	mu         sync.Mutex
	hasCapture bool
	startedAt  time.Time
	staticInfo sysinfo.StaticInfo

	ollamaState ollama.State
	perModel    map[string]*modelEntry
	liveConns   map[uint64]*connEntry
	latencies   []latEntry
	sysInfo     sysinfo.Info

	outputRing  ring
	inputRing   ring
	cpuTempRing ring
	gpuTempRing ring
}

type modelEntry struct {
	last   ebpf.Completion
	ttft   time.Duration
	seenAt time.Time
}

type connEntry struct {
	model     string
	outputTPS float64
	ttft      time.Duration
	ttfr      time.Duration
	streaming bool
	stage     ebpf.GenerationStage
	thinkToks int64
	thinkEls  time.Duration
	thinkTPS  float64
	respToks  int64
	respTPS   float64
	updatedAt time.Time
}

type latEntry struct {
	totalDur time.Duration
	msPerTok float64
	at       time.Time
}

// New creates a Store. Pass hasCapture=true when eBPF monitoring is active.
func New(hasCapture bool) *Store {
	return &Store{
		hasCapture: hasCapture,
		startedAt:  time.Now(),
		staticInfo: sysinfo.CollectStatic(),
		perModel:   make(map[string]*modelEntry),
		liveConns:  make(map[uint64]*connEntry),
		latencies:  make([]latEntry, 0, 256),
	}
}

// PushCompletion records the final stats of a finished generation.
func (s *Store) PushCompletion(c ebpf.Completion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()

	me := s.perModel[c.Model]
	if me == nil {
		me = &modelEntry{}
		s.perModel[c.Model] = me
	}
	me.last = c
	me.seenAt = now

	s.outputRing.record(c.OutputTPS(), now)
	s.inputRing.record(c.InputTPS(), now)

	if c.TotalDuration > 0 {
		le := latEntry{totalDur: c.TotalDuration, at: now}
		if c.OutputTokens > 0 {
			le.msPerTok = float64(c.OutputDuration.Milliseconds()) / float64(c.OutputTokens)
		}
		s.latencies = append(s.latencies, le)
	}
}

// PushLiveUpdate records a streaming chunk from an active generation.
func (s *Store) PushLiveUpdate(u ebpf.LiveUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()

	ce := s.liveConns[u.ConnKey]
	if ce == nil {
		ce = &connEntry{}
		s.liveConns[u.ConnKey] = ce
	}
	ce.model = u.Model
	ce.outputTPS = u.OutputTPS
	ce.streaming = u.Streaming
	ce.stage = u.Stage
	ce.thinkToks = u.ThinkTokens
	ce.thinkEls = u.ThinkElapsed
	ce.thinkTPS = u.ThinkTPS
	ce.respToks = u.ResponseTokens
	ce.respTPS = u.ResponseTPS
	ce.updatedAt = now

	if u.TimeToFirstToken > 0 {
		ce.ttft = u.TimeToFirstToken
		if me := s.perModel[u.Model]; me != nil {
			me.ttft = u.TimeToFirstToken
		}
	}
	if u.TimeToFirstResponse > 0 {
		ce.ttfr = u.TimeToFirstResponse
	}
	if u.Streaming && u.OutputTPS > 0 {
		s.outputRing.record(u.OutputTPS, now)
	}
	if !u.Streaming {
		delete(s.liveConns, u.ConnKey)
	}
}

// SetOllamaState records the latest /api/ps response.
func (s *Store) SetOllamaState(st ollama.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ollamaState = st
}

// SetSysInfo records the latest system metrics and updates temperature history.
func (s *Store) SetSysInfo(info sysinfo.Info) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if info.CPUTempC > 0 {
		s.cpuTempRing.record(info.CPUTempC, now)
	}
	if info.GPUTempC > 0 {
		s.gpuTempRing.record(info.GPUTempC, now)
	}
	s.sysInfo = info
}

// Snapshot builds and returns a consistent View of the current state.
func (s *Store) Snapshot() View {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.pruneLatencies(now)

	st := s.ollamaState

	// Aggregate live streaming state per model.
	type liveAgg struct {
		outputTPS  float64
		ttft, ttfr time.Duration
		active     int
		stage      ebpf.GenerationStage
		thinkToks  int64
		thinkEls   time.Duration
		thinkTPS   float64
		respToks   int64
		respTPS    float64
	}
	byModel := map[string]*liveAgg{}
	for _, ce := range s.liveConns {
		if !ce.streaming || now.Sub(ce.updatedAt) >= liveWindow {
			continue
		}
		agg := byModel[ce.model]
		if agg == nil {
			agg = &liveAgg{}
			byModel[ce.model] = agg
		}
		agg.active++
		agg.outputTPS += ce.outputTPS
		if ce.ttft > agg.ttft {
			agg.ttft = ce.ttft
		}
		if ce.ttfr > agg.ttfr {
			agg.ttfr = ce.ttfr
		}
		agg.stage = ce.stage
		agg.thinkToks = ce.thinkToks
		agg.thinkEls = ce.thinkEls
		agg.thinkTPS = ce.thinkTPS
		agg.respToks = ce.respToks
		agg.respTPS = ce.respTPS
	}

	inAPI := make(map[string]bool, len(st.Models))
	models := make([]ModelView, 0, len(st.Models))
	for _, m := range st.Models {
		inAPI[m.Name] = true
		mv := ModelView{
			Name:      m.Name,
			SizeBytes: m.Size,
			VRAMBytes: m.SizeVRAM,
		}
		if !m.ExpiresAt.IsZero() {
			if d := time.Until(m.ExpiresAt); d > 0 {
				mv.UntilExpiry = d
			}
		}

		if agg, ok := byModel[m.Name]; ok {
			mv.LiveOutputTPS = agg.outputTPS
			mv.ActiveCount = agg.active
			mv.FirstTokenIn = agg.ttft
			mv.FirstRespIn = agg.ttfr
			mv.ThinkTokens = agg.thinkToks
			mv.ThinkElapsed = agg.thinkEls
			mv.ThinkTPS = agg.thinkTPS
			mv.ResponseTokens = agg.respToks
			mv.ResponseTPS = agg.respTPS
			if agg.active > 0 {
				if agg.stage == ebpf.StageThinking {
					mv.Status = "thinking"
				} else {
					mv.Status = "running"
				}
			}
		}

		if me, ok := s.perModel[m.Name]; ok {
			if mv.FirstTokenIn == 0 && me.ttft > 0 {
				mv.FirstTokenIn = me.ttft
			}
			mv.LastTotalDuration = me.last.TotalDuration
			if me.last.OutputTokens > 0 {
				mv.LastMsPerToken = float64(me.last.OutputDuration.Milliseconds()) / float64(me.last.OutputTokens)
				mv.OutputTPS = me.last.OutputTPS()
				mv.InputTPS = me.last.InputTPS()
			}
			if now.Sub(me.seenAt) < staleWindow && mv.Status == "" {
				mv.Status = "running"
			}
		}

		if mv.Status == "" {
			mv.Status = "idle"
		}
		models = append(models, mv)
	}

	// Include models captured by eBPF that are no longer in the Ollama API list
	// (e.g. keep_alive=0 unloads the model immediately after generation).
	for name, me := range s.perModel {
		if inAPI[name] || now.Sub(me.seenAt) > latWindow {
			continue
		}
		mv := ModelView{Name: name, Status: "idle"}
		mv.FirstTokenIn = me.ttft
		mv.LastTotalDuration = me.last.TotalDuration
		if me.last.OutputTokens > 0 {
			mv.OutputTPS = me.last.OutputTPS()
			mv.InputTPS = me.last.InputTPS()
			mv.LastMsPerToken = float64(me.last.OutputDuration.Milliseconds()) / float64(me.last.OutputTokens)
		}
		models = append(models, mv)
	}

	// Throughput
	var currentOut float64
	for _, agg := range byModel {
		currentOut += agg.outputTPS
	}
	outHist := s.outputRing.history(now)
	inHist := s.inputRing.history(now)
	if currentOut == 0 && len(outHist) > 0 {
		currentOut = outHist[len(outHist)-1]
	}
	var currentIn float64
	if len(inHist) > 0 {
		currentIn = inHist[len(inHist)-1]
	}

	cps, meanLat, p95, p99, meanMpt := s.latStats(now)

	sys := s.sysInfo
	if sys.GPUPowerW > 0 && currentOut > 0 {
		sys.TokPerWatt = currentOut / sys.GPUPowerW
	}
	sys.Hostname = s.staticInfo.Hostname
	sys.IPAddress = s.staticInfo.IPAddress
	sys.Username = s.staticInfo.Username
	sys.CPUName = s.staticInfo.CPUName
	sys.OSVersion = s.staticInfo.OSVersion
	sys.ActiveBuckets = activeBuckets(s.startedAt, now)
	sys.CPUTempHistory = s.cpuTempRing.history(now)
	sys.GPUTempHistory = s.gpuTempRing.history(now)

	return View{
		At:         now,
		Connected:  st.Connected,
		Version:    st.Version,
		HasCapture: s.hasCapture,
		Models:     models,
		Perf: PerfStats{
			OutputTPS:         currentOut,
			InputTPS:          currentIn,
			PeakOutputTPS:     s.outputRing.peak(now),
			PeakInputTPS:      s.inputRing.peak(now),
			OutputHistory:     outHist,
			InputHistory:      inHist,
			PeakBuckets:       sys.ActiveBuckets,
			CompletionsPerSec: cps,
			MeanLatency:       meanLat,
			P95Latency:        p95,
			P99Latency:        p99,
			MeanMsPerToken:    meanMpt,
			WindowStart:       s.outputRing.windowStart(s.startedAt, now),
		},
		Sys: sys,
	}
}

func (s *Store) pruneLatencies(now time.Time) {
	cutoff := now.Add(-latWindow)
	i := 0
	for i < len(s.latencies) && s.latencies[i].at.Before(cutoff) {
		i++
	}
	s.latencies = s.latencies[i:]
}

func (s *Store) latStats(now time.Time) (cps float64, mean, p95, p99 time.Duration, meanMpt float64) {
	n := len(s.latencies)
	if n == 0 {
		return
	}

	oneMin := now.Add(-60 * time.Second)
	var recent int
	for _, l := range s.latencies {
		if l.at.After(oneMin) {
			recent++
		}
	}
	cps = float64(recent) / 60.0

	totalsMS := make([]float64, n)
	var sumMpt float64
	var cntMpt int
	for i, l := range s.latencies {
		ms := float64(l.totalDur.Milliseconds())
		totalsMS[i] = ms
		mean += l.totalDur
		if l.msPerTok > 0 {
			sumMpt += l.msPerTok
			cntMpt++
		}
	}
	mean /= time.Duration(n)
	sort.Float64s(totalsMS)
	p95 = time.Duration(pct(totalsMS, 95)) * time.Millisecond
	p99 = time.Duration(pct(totalsMS, 99)) * time.Millisecond
	if cntMpt > 0 {
		meanMpt = sumMpt / float64(cntMpt)
	}
	return
}

func pct(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}
