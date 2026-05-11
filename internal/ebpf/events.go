package ebpf

import "time"

type GenerationStage uint8

const (
	StageGenerate   GenerationStage = iota // normal generation (default)
	StageThinking                          // inside <think>…</think>
	StageResponding                        // after </think>
)

// Completion is the final token stats from a finished stream (done=true).
type Completion struct {
	Model          string
	OutputTokens   int64
	OutputDuration time.Duration
	InputTokens    int64
	InputDuration  time.Duration
	TotalDuration  time.Duration
	RecordedAt     time.Time
}

func (c Completion) OutputTPS() float64 {
	if c.OutputDuration <= 0 {
		return 0
	}
	return float64(c.OutputTokens) / c.OutputDuration.Seconds()
}

func (c Completion) InputTPS() float64 {
	if c.InputDuration <= 0 {
		return 0
	}
	return float64(c.InputTokens) / c.InputDuration.Seconds()
}

type LiveUpdate struct {
	Model               string
	ConnKey             uint64 // opaque connection identifier
	OutputTPS           float64
	TimeToFirstToken    time.Duration
	TimeToFirstResponse time.Duration
	Streaming           bool // false = stream ended
	RecordedAt          time.Time

	Stage          GenerationStage
	ThinkTokens    int64
	ThinkElapsed   time.Duration
	ThinkTPS       float64
	ResponseTokens int64
	ResponseTPS    float64
}
