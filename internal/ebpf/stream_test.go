//go:build linux

package ebpf

import (
	"testing"
	"time"
)

func TestExtractJSONSimple(t *testing.T) {
	completionCh := make(chan Completion, 4)
	liveCh := make(chan LiveUpdate, 16)
	tr := newStreamTracker(completionCh, liveCh)

	st := &connStream{}
	st.buf = []byte(`{"model":"m","response":"hi","done":false}` + "\n")
	tr.extractJSON(st)

	if st.model != "m" {
		t.Errorf("model = %q, want %q", st.model, "m")
	}
	if st.totalToks != 1 {
		t.Errorf("totalToks = %d, want 1", st.totalToks)
	}
	if len(st.buf) != 0 {
		t.Errorf("buf not consumed: %q", st.buf)
	}
}

func TestExtractJSONIncomplete(t *testing.T) {
	tr := newStreamTracker(make(chan Completion, 1), make(chan LiveUpdate, 1))
	st := &connStream{}
	// truncated object — must be retained for next call
	st.buf = []byte(`{"model":"m","resp`)
	tr.extractJSON(st)

	if string(st.buf) != `{"model":"m","resp` {
		t.Errorf("incomplete buffer was modified: %q", st.buf)
	}
}

func TestExtractJSONDoneEmitsCompletion(t *testing.T) {
	completionCh := make(chan Completion, 1)
	tr := newStreamTracker(completionCh, make(chan LiveUpdate, 4))
	st := &connStream{model: "m"}
	st.buf = []byte(`{"model":"m","done":true,"eval_count":42,"eval_duration":1000000000,"total_duration":2000000000}`)
	tr.extractJSON(st)

	select {
	case c := <-completionCh:
		if c.OutputTokens != 42 {
			t.Errorf("OutputTokens = %d, want 42", c.OutputTokens)
		}
		if c.OutputDuration != time.Second {
			t.Errorf("OutputDuration = %v, want 1s", c.OutputDuration)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("no Completion emitted")
	}
}

func TestExtractJSONThinkingStage(t *testing.T) {
	tr := newStreamTracker(make(chan Completion, 1), make(chan LiveUpdate, 16))
	st := &connStream{}
	for _, frag := range []string{
		`{"response":"<think>","done":false}`,
		`{"response":"a","done":false}`,
		`{"response":"b","done":false}`,
		`{"response":"</think>","done":false}`,
		`{"response":"answer","done":false}`,
	} {
		st.buf = append(st.buf, frag...)
	}
	tr.extractJSON(st)

	if st.stage != StageResponding {
		t.Errorf("stage = %v, want StageResponding", st.stage)
	}
	if st.thinkToks != 3 { // a, b, </think>
		t.Errorf("thinkToks = %d, want 3", st.thinkToks)
	}
	if st.respToks != 1 { // "answer"
		t.Errorf("respToks = %d, want 1", st.respToks)
	}
}

func TestExtractJSONIgnoresChunkedFraming(t *testing.T) {
	// Chunk-size lines (hex + CRLF) appear between JSON objects in chunked
	// transfer-encoding. The brace scanner should skip them naturally.
	tr := newStreamTracker(make(chan Completion, 1), make(chan LiveUpdate, 4))
	st := &connStream{}
	st.buf = []byte("2A\r\n" + `{"response":"x","done":false}` + "\r\n0\r\n\r\n")
	tr.extractJSON(st)

	if st.totalToks != 1 {
		t.Errorf("totalToks = %d, want 1", st.totalToks)
	}
}
