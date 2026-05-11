//go:build linux

package ebpf

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

type connKey struct {
	srcIP, dstIP     uint32
	srcPort, dstPort uint16
}

type connStream struct {
	buf        []byte
	inHeaders  bool
	model      string
	startTime  time.Time
	firstTokAt time.Time
	totalToks  int64
	thinkToks  int64
	respToks   int64
	stage      GenerationStage
	thinkStart time.Time
	thinkEnd   time.Time
	respStart  time.Time
	connKey    uint64
}

type streamTracker struct {
	conns        map[connKey]*connStream
	completionCh chan<- Completion
	liveUpdateCh chan<- LiveUpdate
	nextKey      uint64
}

func newStreamTracker(completionCh chan<- Completion, liveUpdateCh chan<- LiveUpdate) *streamTracker {
	return &streamTracker{
		conns:        make(map[connKey]*connStream),
		completionCh: completionCh,
		liveUpdateCh: liveUpdateCh,
	}
}

func (t *streamTracker) handleEvent(ev *tcpEvent) {
	key := connKey{
		srcIP: ev.SrcIP, dstIP: ev.DstIP,
		srcPort: ev.SrcPort, dstPort: ev.DstPort,
	}

	st, ok := t.conns[key]
	if !ok {
		t.nextKey++
		st = &connStream{
			inHeaders: true,
			startTime: time.Now(),
			connKey:   t.nextKey,
		}
		t.conns[key] = st
		t.sendLive(st, true)
	}

	if ev.DataLen > 0 {
		st.buf = append(st.buf, ev.Data[:ev.DataLen]...)
		st.processBuffer(t)
	}

	if ev.Fin == 1 {
		st.flush(t)
		t.sendLive(st, false)
		delete(t.conns, key)
	}
}

func (st *connStream) processBuffer(t *streamTracker) {
	if st.inHeaders {
		idx := bytes.Index(st.buf, []byte("\r\n\r\n"))
		if idx < 0 {
			return
		}
		st.inHeaders = false
		st.buf = st.buf[idx+4:]
	}
	t.extractJSON(st)
}

func (st *connStream) flush(t *streamTracker) {
	if !st.inHeaders {
		t.extractJSON(st)
	}
}

type ndjsonLine struct {
	Model string `json:"model"`
	Done  bool   `json:"done"`
	// /api/generate
	Response string `json:"response"`
	// /api/chat
	Message *struct {
		Content string `json:"content"`
	} `json:"message"`
	// Final stats (done=true)
	EvalCount          int64 `json:"eval_count"`
	EvalDuration       int64 `json:"eval_duration"`
	PromptEvalCount    int64 `json:"prompt_eval_count"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	TotalDuration      int64 `json:"total_duration"`
}

func (l *ndjsonLine) content() string {
	if l.Message != nil {
		return l.Message.Content
	}
	return l.Response
}

// brace-depth scanner; chunked-transfer hex lines are skipped (no '{').
func (t *streamTracker) extractJSON(st *connStream) {
	buf := st.buf
	consumed := 0

	for i := 0; i < len(buf); i++ {
		if buf[i] != '{' {
			consumed = i + 1
			continue
		}
		depth := 0
		inStr := false
		escape := false
		end := -1
		for j := i; j < len(buf); j++ {
			c := buf[j]
			if escape {
				escape = false
				continue
			}
			if c == '\\' && inStr {
				escape = true
				continue
			}
			if c == '"' {
				inStr = !inStr
				continue
			}
			if inStr {
				continue
			}
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
				if depth == 0 {
					end = j
					break
				}
			}
		}
		if end < 0 {
			break
		}

		var line ndjsonLine
		if err := json.Unmarshal(buf[i:end+1], &line); err != nil {
			slog.Debug("ebpf: json parse error", "err", err)
			consumed = end + 1
			i = end
			continue
		}

		if line.Model != "" && st.model == "" {
			st.model = line.Model
		}

		t.handleLine(st, &line, time.Now())

		consumed = end + 1
		i = end
	}

	if consumed > 0 {
		st.buf = st.buf[consumed:]
	}
}

func (t *streamTracker) handleLine(st *connStream, line *ndjsonLine, now time.Time) {
	if line.Done {
		if line.EvalCount > 0 {
			select {
			case t.completionCh <- Completion{
				Model:          st.model,
				OutputTokens:   line.EvalCount,
				OutputDuration: time.Duration(line.EvalDuration),
				InputTokens:    line.PromptEvalCount,
				InputDuration:  time.Duration(line.PromptEvalDuration),
				TotalDuration:  time.Duration(line.TotalDuration),
				RecordedAt:     now,
			}:
			default:
			}
		}
		return
	}

	content := line.content()
	if content == "" {
		return
	}

	if st.firstTokAt.IsZero() {
		st.firstTokAt = now
	}
	st.totalToks++
	t.updateStage(st, content, now)

	elapsed := now.Sub(st.startTime).Seconds()
	var liveTPS float64
	if elapsed > 0.1 {
		liveTPS = float64(st.totalToks) / elapsed
	}

	var thinkEls time.Duration
	var thinkTPS float64
	if st.stage == StageThinking && !st.thinkStart.IsZero() {
		thinkEls = now.Sub(st.thinkStart)
		if thinkEls.Seconds() > 0 {
			thinkTPS = float64(st.thinkToks) / thinkEls.Seconds()
		}
	}

	var ttfr time.Duration
	var respTPS float64
	if !st.respStart.IsZero() {
		ttfr = st.respStart.Sub(st.startTime)
		if d := now.Sub(st.respStart).Seconds(); d > 0 {
			respTPS = float64(st.respToks) / d
		}
	}

	select {
	case t.liveUpdateCh <- LiveUpdate{
		Model:               st.model,
		ConnKey:             st.connKey,
		OutputTPS:           liveTPS,
		TimeToFirstToken:    st.firstTokAt.Sub(st.startTime),
		TimeToFirstResponse: ttfr,
		Streaming:           true,
		RecordedAt:          now,
		Stage:               st.stage,
		ThinkTokens:         st.thinkToks,
		ThinkElapsed:        thinkEls,
		ThinkTPS:            thinkTPS,
		ResponseTokens:      st.respToks,
		ResponseTPS:         respTPS,
	}:
	default:
	}
}

func (t *streamTracker) updateStage(st *connStream, content string, now time.Time) {
	switch st.stage {
	case StageGenerate:
		if strings.Contains(content, "<think>") {
			st.stage = StageThinking
			st.thinkStart = now
		} else {
			st.respToks++
		}
	case StageThinking:
		st.thinkToks++
		if strings.Contains(content, "</think>") {
			st.thinkEnd = now
			st.stage = StageResponding
			st.respStart = now
		}
	case StageResponding:
		st.respToks++
	}
}

func (t *streamTracker) sendLive(st *connStream, streaming bool) {
	select {
	case t.liveUpdateCh <- LiveUpdate{
		Model:      st.model,
		ConnKey:    st.connKey,
		Streaming:  streaming,
		RecordedAt: time.Now(),
	}:
	default:
	}
}
