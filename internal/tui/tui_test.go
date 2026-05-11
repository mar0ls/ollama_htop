package tui

import (
	"strings"
	"testing"
	"time"

	"ollamaHtop/internal/store"
	"ollamaHtop/internal/sysinfo"
)

// ── SortMode ──────────────────────────────────────────────────────────────────

func TestSortModeString(t *testing.T) {
	cases := []struct {
		mode SortMode
		want string
	}{
		{SortName, "name"},
		{SortTokSec, "tok/s"},
		{SortVRAM, "VRAM"},
		{SortStatus, "status"},
		{sortModeCount, "default"},
	}
	for _, c := range cases {
		if got := c.mode.String(); got != c.want {
			t.Errorf("SortMode(%d).String() = %q, want %q", c.mode, got, c.want)
		}
	}
}

// ── formatBytes / formatBytesU ────────────────────────────────────────────────

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		input int64
		want  string
	}{
		{512 * (1 << 20), "512.0 MB"},
		{2 * (1 << 30), "2.0 GB"},
		{0, "0.0 MB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.input); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestFormatBytesU(t *testing.T) {
	if got := formatBytesU(uint64(1 << 30)); got != "1.0 GB" {
		t.Errorf("formatBytesU(1 GiB) = %q", got)
	}
	if got := formatBytesU(uint64(100 * (1 << 20))); got != "100.0 MB" {
		t.Errorf("formatBytesU(100 MiB) = %q", got)
	}
}

// ── formatMS ─────────────────────────────────────────────────────────────────

func TestFormatMS(t *testing.T) {
	cases := []struct {
		ms   float64
		want string
	}{
		{0, "—"},
		{-1, "—"},
		{250, "250ms"},
		{999, "999ms"},
		{1000, "1.00s"},
		{2500, "2.50s"},
	}
	for _, c := range cases {
		if got := formatMS(c.ms); got != c.want {
			t.Errorf("formatMS(%.0f) = %q, want %q", c.ms, got, c.want)
		}
	}
}

// ── formatDur ─────────────────────────────────────────────────────────────────

func TestFormatDur(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Microsecond, "500µs"},
		{250 * time.Millisecond, "250ms"},
		{1500 * time.Millisecond, "1.5s"},
		{2 * time.Second, "2.0s"},
	}
	for _, c := range cases {
		if got := formatDur(c.d); got != c.want {
			t.Errorf("formatDur(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// ── formatDuration ────────────────────────────────────────────────────────────

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "—"},
		{-time.Second, "—"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{2*time.Minute + 5*time.Second, "2m05s"},
	}
	for _, c := range cases {
		if got := formatDuration(c.d); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// ── visLen ────────────────────────────────────────────────────────────────────

func TestVisLen(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"hello", 5},
		{"\033[32mhello\033[0m", 5},     // colored text
		{"\033[1m\033[32mAB\033[0m", 2}, // bold+color
		{"", 0},
	}
	for _, c := range cases {
		if got := visLen(c.s); got != c.want {
			t.Errorf("visLen(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

// ── padRight ──────────────────────────────────────────────────────────────────

func TestPadRight(t *testing.T) {
	s := "\033[32mhi\033[0m" // "hi" with color — visLen=2
	result := padRight(s, 5)
	if visLen(result) != 5 {
		t.Errorf("padRight visLen = %d, want 5", visLen(result))
	}
	if !strings.HasSuffix(result, "   ") {
		t.Errorf("padRight should have trailing spaces: %q", result)
	}

	// Already wide enough — should not pad.
	wide := "hello world"
	if padRight(wide, 5) != wide {
		t.Errorf("padRight should not truncate: %q", padRight(wide, 5))
	}
}

// ── padStr ────────────────────────────────────────────────────────────────────

func TestPadStr(t *testing.T) {
	if got := padStr("ab", 4); got != "ab  " {
		t.Errorf("padStr short = %q, want 'ab  '", got)
	}
	if got := padStr("abcde", 3); got != "abc" {
		t.Errorf("padStr long = %q, want 'abc'", got)
	}
	if got := padStr("abc", 3); got != "abc" {
		t.Errorf("padStr exact = %q, want 'abc'", got)
	}
}

// ── truncate ──────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("hello world", 8); got != "hello..." {
		t.Errorf("truncate long = %q, want 'hello...'", got)
	}
	if got := truncate("hi", 2); got != "hi" {
		t.Errorf("truncate exact = %q", got)
	}
	if got := truncate("hello", 3); got != "..." {
		t.Errorf("truncate to 3 = %q, want '...'", got)
	}
	if got := truncate("hello", 2); got != ".." {
		t.Errorf("truncate to 2 = %q, want '..'", got)
	}
}

// ── tempColor ─────────────────────────────────────────────────────────────────

func TestTempColor(t *testing.T) {
	if got := tempColor(50, 70, 90); got != colorGreen {
		t.Errorf("tempColor(50) = %q, want green", got)
	}
	if got := tempColor(75, 70, 90); got != colorAmber {
		t.Errorf("tempColor(75) = %q, want amber", got)
	}
	if got := tempColor(95, 70, 90); got != colorRed {
		t.Errorf("tempColor(95) = %q, want red", got)
	}
}

// ── renderBar ─────────────────────────────────────────────────────────────────

func TestRenderBar(t *testing.T) {
	b := renderBar(50, 10)
	if visLen(b) != 10 {
		t.Errorf("renderBar(50%%, 10) visLen = %d, want 10", visLen(b))
	}
	// 100% should be all filled.
	b100 := renderBar(100, 8)
	if visLen(b100) != 8 {
		t.Errorf("renderBar(100%%, 8) visLen = %d, want 8", visLen(b100))
	}
	// 0% should be all empty.
	b0 := renderBar(0, 8)
	if visLen(b0) != 8 {
		t.Errorf("renderBar(0%%, 8) visLen = %d, want 8", visLen(b0))
	}
}

// ── buildSparkline ────────────────────────────────────────────────────────────

func TestBuildSparkline(t *testing.T) {
	data := []float64{0, 1, 2, 3, 4, 5}
	spark := buildSparkline(data, 6, 6, colorGreen)
	if visLen(spark) != 6 {
		t.Errorf("buildSparkline visLen = %d, want 6", visLen(spark))
	}
}

func TestBuildSparklineEmpty(t *testing.T) {
	spark := buildSparkline(nil, 10, 0, colorGreen)
	// Should not panic.
	_ = spark
}

func TestBuildSparklineAllZero(t *testing.T) {
	data := []float64{0, 0, 0, 0}
	spark := buildSparkline(data, 4, 4, colorGreen)
	if visLen(spark) != 4 {
		t.Errorf("buildSparkline zeros visLen = %d, want 4", visLen(spark))
	}
}

func TestBuildSparklineInactiveBuckets(t *testing.T) {
	data := []float64{5, 10, 15}
	// activeBuckets < width → should have inactive dots.
	spark := buildSparkline(data, 10, 3, colorGreen)
	if visLen(spark) != 10 {
		t.Errorf("buildSparkline with inactive visLen = %d, want 10", visLen(spark))
	}
}

// ── sectionLine / borderedLine ────────────────────────────────────────────────

func TestSectionLine(t *testing.T) {
	line := sectionLine(80, "Models")
	if !strings.Contains(line, "Models") {
		t.Error("sectionLine should contain title")
	}
}

func TestBorderedLine(t *testing.T) {
	inner := 76
	content := "hello"
	line := borderedLine(inner, content)
	// Should contain the borders and the content.
	if !strings.Contains(line, content) {
		t.Errorf("borderedLine should contain content: %q", line)
	}
}

// ── renderHeader / renderHeader2 ─────────────────────────────────────────────

func TestRenderHeader(t *testing.T) {
	snap := store.View{
		Connected: true,
		Version:   "0.22.1",
		Sys: sysinfo.Info{
			Hostname:  "testhost",
			IPAddress: "10.0.0.1",
			Username:  "user",
		},
	}
	h := renderHeader(100, snap, "localhost:11434")
	if visLen(h) != 100 {
		t.Errorf("renderHeader visLen = %d, want 100", visLen(h))
	}
	if !strings.Contains(h, "0.22.1") {
		t.Error("renderHeader should contain version")
	}
}

func TestRenderHeader2(t *testing.T) {
	snap := store.View{
		Sys: sysinfo.Info{
			CPUName:   "Intel Core i9",
			OSVersion: "Ubuntu 24.04",
		},
	}
	h := renderHeader2(80, snap)
	if visLen(h) != 80 {
		t.Errorf("renderHeader2 visLen = %d, want 80", visLen(h))
	}
}

// ── renderDisconnectedBanner ──────────────────────────────────────────────────

func TestRenderDisconnectedBannerNoTime(t *testing.T) {
	line := renderDisconnectedBanner(76, "localhost:11434", time.Time{})
	if !strings.Contains(line, "No connection") {
		t.Error("banner should mention 'No connection'")
	}
}

func TestRenderDisconnectedBannerWithTime(t *testing.T) {
	disconnAt := time.Now().Add(-30 * time.Second)
	line := renderDisconnectedBanner(76, "host", disconnAt)
	if !strings.Contains(line, "ago") {
		t.Error("banner should mention 'ago' when disconnectedAt is set")
	}
}

// ── renderModels ──────────────────────────────────────────────────────────────

func TestRenderModelsEmpty(t *testing.T) {
	snap := store.View{Connected: true}
	lines := renderModels(76, snap, SortName)
	if len(lines) == 0 {
		t.Error("renderModels should return at least a header line")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "No models") {
			found = true
		}
	}
	if !found {
		t.Error("renderModels empty should mention 'No models'")
	}
}

func TestRenderModelsRunning(t *testing.T) {
	snap := store.View{
		Connected:  true,
		HasCapture: true,
		Models: []store.ModelView{
			{Name: "llama3", Status: "running", LiveOutputTPS: 42, ActiveCount: 1, FirstTokenIn: 200 * time.Millisecond},
		},
	}
	lines := renderModels(100, snap, SortName)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "llama3") {
		t.Error("renderModels should contain model name")
	}
}

func TestRenderModelsThinking(t *testing.T) {
	snap := store.View{
		Connected:  true,
		HasCapture: true,
		Models: []store.ModelView{
			{
				Name: "deepseek", Status: "thinking",
				ThinkTokens: 100, ThinkElapsed: 2 * time.Second, ThinkTPS: 50,
				FirstRespIn: 300 * time.Millisecond,
			},
		},
	}
	lines := renderModels(100, snap, SortName)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "thinking") {
		t.Error("renderModels thinking status should appear")
	}
}

func TestRenderModelsIdle(t *testing.T) {
	snap := store.View{
		Connected:  true,
		HasCapture: true,
		Models: []store.ModelView{
			{Name: "phi3", Status: "idle", LastTotalDuration: 2 * time.Second, LastMsPerToken: 5.5},
		},
	}
	lines := renderModels(100, snap, SortName)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "phi3") {
		t.Error("renderModels idle should contain model name")
	}
}

func TestRenderModelsSortModes(t *testing.T) {
	snap := store.View{
		Connected: true,
		Models: []store.ModelView{
			{Name: "b-model", Status: "running", LiveOutputTPS: 10},
			{Name: "a-model", Status: "idle", LiveOutputTPS: 20},
		},
	}
	for _, mode := range []SortMode{SortName, SortTokSec, SortVRAM, SortStatus} {
		lines := renderModels(100, snap, mode)
		if len(lines) < 2 {
			t.Errorf("renderModels sort=%v returned too few lines", mode)
		}
	}
}

func TestRenderModelsNoCapture(t *testing.T) {
	snap := store.View{
		Connected:  true,
		HasCapture: false,
		Models:     []store.ModelView{{Name: "llama3", Status: "idle"}},
	}
	lines := renderModels(100, snap, SortName)
	joined := strings.Join(lines, "\n")
	// Without capture, tok/s should show dashes.
	if !strings.Contains(joined, "—") {
		t.Error("renderModels without capture should show dashes for TPS")
	}
}

// ── renderThroughput ──────────────────────────────────────────────────────────

func TestRenderThroughput(t *testing.T) {
	snap := store.View{
		Connected: true,
		Perf: store.PerfStats{
			OutputTPS:         42,
			InputTPS:          5,
			PeakOutputTPS:     60,
			OutputHistory:     make([]float64, 60),
			InputHistory:      make([]float64, 60),
			CompletionsPerSec: 0.5,
			MeanLatency:       time.Second,
			P95Latency:        2 * time.Second,
			P99Latency:        3 * time.Second,
			MeanMsPerToken:    7.5,
		},
		Sys: sysinfo.Info{GPUPowerW: 100, TokPerWatt: 0.42},
	}
	lines := renderThroughput(100, snap)
	if len(lines) == 0 {
		t.Error("renderThroughput should return lines")
	}
}

func TestRenderThroughputNoLatency(t *testing.T) {
	snap := store.View{
		Perf: store.PerfStats{
			OutputHistory: make([]float64, 60),
			InputHistory:  make([]float64, 60),
		},
	}
	lines := renderThroughput(100, snap)
	if len(lines) == 0 {
		t.Error("renderThroughput should return lines even with no data")
	}
}

// ── renderSystem ──────────────────────────────────────────────────────────────

func TestRenderSystem(t *testing.T) {
	sys := sysinfo.Info{
		CPUPercent:     55,
		CPUTempC:       65,
		GPUAvail:       true,
		GPUName:        "RTX4090",
		GPUPercent:     80,
		GPUTempC:       82,
		MemPercent:     40,
		MemUsedB:       4 << 30,
		MemTotalB:      16 << 30,
		LoadAvg1:       1.2,
		LoadAvg5:       0.8,
		LoadAvg15:      0.6,
		SensorsAvail:   true,
		ActiveBuckets:  10,
		CPUTempHistory: []float64{60, 62, 65},
		GPUTempHistory: []float64{75, 78, 82},
	}
	lines := renderSystem(100, sys)
	if len(lines) == 0 {
		t.Error("renderSystem should return lines")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "CPU") {
		t.Error("renderSystem should show CPU")
	}
	if !strings.Contains(joined, "RAM") {
		t.Error("renderSystem should show RAM")
	}
}

func TestRenderSystemNoGPU(t *testing.T) {
	sys := sysinfo.Info{
		CPUPercent: 30,
		MemPercent: 20,
		MemTotalB:  8 << 30,
	}
	lines := renderSystem(100, sys)
	if len(lines) == 0 {
		t.Error("renderSystem no GPU should return lines")
	}
}

// ── renderFooter ──────────────────────────────────────────────────────────────

func TestRenderFooter(t *testing.T) {
	footer := renderFooter(80, SortName, "0.0.0.0:9090")
	if !strings.Contains(footer, "quit") {
		t.Error("footer should mention quit")
	}
	if !strings.Contains(footer, "9090") {
		t.Error("footer should mention web port")
	}
}

func TestRenderFooterNoWeb(t *testing.T) {
	footer := renderFooter(80, SortTokSec, "")
	if strings.Contains(footer, "web") {
		t.Error("footer without web address should not mention web")
	}
}

// ── TUI.UpdateSnapshot ────────────────────────────────────────────────────────

func TestTUIUpdateSnapshotConnected(t *testing.T) {
	tui := New("host:11434", "0.0.0.0:9090")
	v := store.View{Connected: true, Version: "1.0"}
	tui.UpdateSnapshot(v)
	tui.mu.Lock()
	defer tui.mu.Unlock()
	if !tui.snap.Connected {
		t.Error("snap.Connected should be true")
	}
	if tui.lastGood.Version != "1.0" {
		t.Errorf("lastGood.Version = %q, want 1.0", tui.lastGood.Version)
	}
	if !tui.disconnectedAt.IsZero() {
		t.Error("disconnectedAt should be zero when connected")
	}
}

func TestTUIUpdateSnapshotDisconnected(t *testing.T) {
	tui := New("host:11434", "")
	// First mark as connected, then disconnected.
	tui.UpdateSnapshot(store.View{Connected: true})
	tui.UpdateSnapshot(store.View{Connected: false})
	tui.mu.Lock()
	defer tui.mu.Unlock()
	if tui.disconnectedAt.IsZero() {
		t.Error("disconnectedAt should be set when transitioning to disconnected")
	}
}

// ── sparkRow ─────────────────────────────────────────────────────────────────

func TestSparkRow(t *testing.T) {
	hist := make([]float64, 60)
	hist[59] = 42
	row := sparkRow("tok/s  ", hist, 42, 60, "12:00", 60, 20)
	if !strings.Contains(row, "tok/s") {
		t.Error("sparkRow should contain label")
	}
}

// ── renderHelp ────────────────────────────────────────────────────────────────

func TestRenderHelp(t *testing.T) {
	help := renderHelp()
	if !strings.Contains(help, "q") {
		t.Error("renderHelp should mention quit key")
	}
}
