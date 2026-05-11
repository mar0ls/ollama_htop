// Package tui implements an htop-style terminal interface via ANSI escape
// sequences on stdout, without external TUI libraries.
package tui

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"ollamaHtop/internal/store"
	"ollamaHtop/internal/sysinfo"
)

const (
	ansiReset          = "\033[0m"
	ansiBold           = "\033[1m"
	ansiDim            = "\033[2m"
	ansiHideCursor     = "\033[?25l"
	ansiShowCursor     = "\033[?25h"
	ansiHome           = "\033[H"
	ansiClearEOL       = "\033[K"
	ansiClearDown      = "\033[J"
	ansiEnterAltScreen = "\033[?1049h"
	ansiLeaveAltScreen = "\033[?1049l"

	// 256-color palette
	colorBlue   = "\033[38;5;75m"
	colorGreen  = "\033[38;5;114m"
	colorAmber  = "\033[38;5;214m"
	colorRed    = "\033[38;5;196m"
	colorGrey   = "\033[38;5;243m"
	colorWhite  = "\033[38;5;255m"
	colorPink   = "\033[38;5;213m"
	colorBorder = "\033[38;5;240m"
)

type SortMode int

const (
	SortDefault SortMode = iota
	SortName
	SortTokSec
	SortVRAM
	SortStatus
	sortModeCount
)

func (s SortMode) String() string {
	switch s {
	case SortName:
		return "name"
	case SortTokSec:
		return "tok/s"
	case SortVRAM:
		return "VRAM"
	case SortStatus:
		return "status"
	default:
		return "default"
	}
}

type Key byte

const (
	KeyQuit  Key = 'q'
	KeySort  Key = 's'
	KeyHelp  Key = '?'
	KeyWeb   Key = 'w'
	KeyCtrlC Key = 3
)

type TUI struct {
	host    string
	webAddr string
	out     io.Writer

	mu       sync.Mutex
	snap     store.View
	lastGood store.View

	sortMode       SortMode
	showHelp       bool
	disconnectedAt time.Time
	tick           int
}

func New(host, webAddr string) *TUI {
	return &TUI{
		host:    host,
		webAddr: webAddr,
		out:     os.Stdout,
	}
}

func (t *TUI) UpdateSnapshot(v store.View) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if v.Connected {
		t.lastGood = v
		t.disconnectedAt = time.Time{}
	} else if t.snap.Connected && !v.Connected {
		t.disconnectedAt = time.Now()
	}
	t.snap = v
}

func (t *TUI) Run(keyCh <-chan Key) {
	fmt.Fprint(t.out, ansiEnterAltScreen+ansiHideCursor)                      //nolint:errcheck
	defer fmt.Fprint(t.out, ansiLeaveAltScreen+ansiShowCursor+ansiReset+"\n") //nolint:errcheck

	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	defer signal.Stop(sigwinch)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	t.render()

	for {
		select {
		case <-ticker.C:
			t.tick++
			t.render()
		case <-sigwinch:
			t.render()
		case k, ok := <-keyCh:
			if !ok {
				return
			}
			switch k {
			case KeyQuit, KeyCtrlC:
				return
			case KeySort:
				t.mu.Lock()
				t.sortMode = (t.sortMode + 1) % sortModeCount
				t.mu.Unlock()
				t.render()
			case KeyHelp:
				t.mu.Lock()
				t.showHelp = !t.showHelp
				t.mu.Unlock()
				t.render()
			}
		}
	}
}

func ReadInput(keyCh chan<- Key) {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState) //nolint:errcheck

	buf := make([]byte, 4)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			close(keyCh)
			return
		}
		switch buf[0] {
		case 3:
			keyCh <- KeyCtrlC
			return
		case 'q', 'Q':
			keyCh <- KeyQuit
			return
		default:
			keyCh <- Key(buf[0])
		}
	}
}

func (t *TUI) render() {
	t.mu.Lock()
	snap := t.snap
	lastGood := t.lastGood
	sortMode := t.sortMode
	showHelp := t.showHelp
	disconnectedAt := t.disconnectedAt
	tick := t.tick
	t.mu.Unlock()

	w, h := termSize()
	if w < 80 {
		w = 80
	}

	var b strings.Builder
	b.WriteString("\033[2J" + ansiHome) // full clear every frame — MakeRaw disables OPOST
	inner := w - 4
	writeFrame(&b, w, h, snap, lastGood, sortMode, showHelp, disconnectedAt, tick, t.host, t.webAddr, inner)
	fmt.Fprint(t.out, b.String()) //nolint:errcheck
}

func writeFrame(b *strings.Builder, w, _ int, snap, lastGood store.View,
	sortMode SortMode, showHelp bool, disconnectedAt time.Time, tick int,
	host, webAddr string, inner int) {

	_ = tick // reserved for future animations

	lines := 0
	writeln := func(s string) {
		b.WriteString(s)
		b.WriteString(ansiClearEOL)
		b.WriteString("\r\n") // MakeRaw disables OPOST/ONLCR — must use \r\n
		lines++
	}

	writeln(renderHeader(w, snap, host))
	writeln(renderHeader2(w, snap))

	if !snap.Connected {
		writeln(renderDisconnectedBanner(inner, host, disconnectedAt))
		if len(lastGood.Models) > 0 {
			writeln(sectionLine(w, "Models"))
			for _, l := range renderModels(inner, lastGood, sortMode) {
				writeln(l)
			}
			writeln(sectionLine(w, "System"))
			for _, l := range renderSystem(inner, lastGood.Sys) {
				writeln(l)
			}
		}
	} else {
		writeln(sectionLine(w, "Models"))
		for _, l := range renderModels(inner, snap, sortMode) {
			writeln(l)
		}
		if snap.HasCapture {
			writeln(sectionLine(w, "Throughput"))
			for _, l := range renderThroughput(inner, snap) {
				writeln(l)
			}
		}
		writeln(sectionLine(w, "System"))
		for _, l := range renderSystem(inner, snap.Sys) {
			writeln(l)
		}
	}

	writeln(renderFooter(w, sortMode, webAddr))

	b.WriteString(ansiClearDown)
	if showHelp {
		b.WriteString(renderHelp())
	}
}


func renderHeader(w int, snap store.View, host string) string {
	sys := snap.Sys
	left := colorBlue + ansiBold + " ollamaHtop" + ansiReset
	ver := snap.Version
	if ver == "" {
		ver = "—"
	}
	right := colorGrey + host + "  v" + ver + ansiReset

	var userHost string
	if sys.Username != "" && sys.Hostname != "" {
		userHost = sys.Username + "@" + sys.Hostname
	} else {
		userHost = sys.Hostname
	}
	if sys.IPAddress != "" {
		userHost += " (" + sys.IPAddress + ")"
	}
	middle := colorGrey + userHost + ansiReset

	leftLen := visLen(left)
	middleLen := visLen(middle)
	rightLen := visLen(right)
	totalFixed := 2 + leftLen + 2 + middleLen + 2 + rightLen + 2
	dashes := w - totalFixed
	if dashes < 2 {
		dashes = 2
	}
	dl := dashes / 2
	dr := dashes - dl

	return colorBorder + "┌─" + ansiReset + left +
		colorBorder + " " + strings.Repeat("─", dl) + " " + ansiReset +
		middle +
		colorBorder + " " + strings.Repeat("─", dr) + " " + ansiReset +
		right +
		colorBorder + "─┐" + ansiReset
}

func renderHeader2(w int, snap store.View) string {
	sys := snap.Sys
	var parts []string
	if sys.CPUName != "" {
		parts = append(parts, sys.CPUName)
	}
	if sys.OSVersion != "" {
		parts = append(parts, sys.OSVersion)
	}
	content := colorGrey + strings.Join(parts, "  ·  ") + ansiReset
	inner := w - 4
	pad := inner - visLen(content)
	if pad < 0 {
		pad = 0
	}
	return colorBorder + "│ " + ansiReset + content + strings.Repeat(" ", pad) + colorBorder + " │" + ansiReset
}

func sectionLine(w int, title string) string {
	rendered := colorBlue + ansiBold + title + ansiReset
	dashes := w - 2 - visLen(rendered) - 4
	if dashes < 1 {
		dashes = 1
	}
	return colorBorder + "├─ " + ansiReset + rendered + colorBorder + " " + strings.Repeat("─", dashes) + "┤" + ansiReset
}

func borderedLine(inner int, content string) string {
	pad := inner - visLen(content)
	if pad < 0 {
		pad = 0
	}
	return colorBorder + "│" + ansiReset + " " + content + strings.Repeat(" ", pad) + " " + colorBorder + "│" + ansiReset
}

func renderDisconnectedBanner(inner int, host string, disconnectedAt time.Time) string {
	msg := colorAmber + " ⚠ No connection to Ollama at " + host
	if !disconnectedAt.IsZero() {
		msg += fmt.Sprintf(" (%s ago)", time.Since(disconnectedAt).Truncate(time.Second))
	}
	msg += " — retrying..." + ansiReset
	return borderedLine(inner, msg)
}


func renderModels(inner int, snap store.View, sortMode SortMode) []string {
	const (
		colSize    = 8
		colVRAM    = 8
		colTokSec  = 7
		colPrompt  = 8
		colTTFT    = 7
		colStatus  = 10
		colExpires = 8
	)
	fixedCols := colSize + colVRAM + colTokSec + colPrompt + colTTFT + colStatus + colExpires
	colModel := inner - 8 - fixedCols
	if colModel < 12 {
		colModel = 12
	}

	modelHdr, sizeHdr, vramHdr, tokHdr, promptHdr, ttftHdr, statusHdr, expiresHdr :=
		"MODEL", "SIZE", "VRAM", "TOK/S", "PROMPT/S", "TTFT", "STATUS", "EXPIRES"
	switch sortMode {
	case SortName:
		modelHdr = "MODEL▼"
	case SortTokSec:
		tokHdr = "TOK/S▼"
	case SortVRAM:
		vramHdr = "VRAM▼"
	case SortStatus:
		statusHdr = "STATUS▼"
	}

	hdr := fmt.Sprintf(" %-*s %-*s %-*s %-*s %-*s %-*s %-*s %-*s",
		colModel, modelHdr, colSize, sizeHdr, colVRAM, vramHdr,
		colTokSec, tokHdr, colPrompt, promptHdr, colTTFT, ttftHdr, colStatus, statusHdr, colExpires, expiresHdr)

	var lines []string
	lines = append(lines, borderedLine(inner, colorGrey+hdr+ansiReset))

	if len(snap.Models) == 0 {
		lines = append(lines, borderedLine(inner, colorGrey+" No models loaded"+ansiReset))
		lines = append(lines, borderedLine(inner, ""))
		return lines
	}

	models := make([]store.ModelView, len(snap.Models))
	copy(models, snap.Models)
	switch sortMode {
	case SortName:
		sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	case SortTokSec:
		sort.Slice(models, func(i, j int) bool {
			a := models[i].LiveOutputTPS
			if a == 0 {
				a = models[i].OutputTPS
			}
			bv := models[j].LiveOutputTPS
			if bv == 0 {
				bv = models[j].OutputTPS
			}
			return a > bv
		})
	case SortVRAM:
		sort.Slice(models, func(i, j int) bool { return models[i].VRAMBytes > models[j].VRAMBytes })
	case SortStatus:
		sort.Slice(models, func(i, j int) bool {
			if models[i].Status == models[j].Status {
				return models[i].Name < models[j].Name
			}
			return models[i].Status == "running"
		})
	}

	for _, mdl := range models {
		name := colorWhite + truncate(mdl.Name, colModel) + ansiReset
		size := formatBytes(mdl.SizeBytes)
		vram := formatBytes(mdl.VRAMBytes)

		var tps, prompt, ttft string
		if !snap.HasCapture {
			tps = colorGrey + "—" + ansiReset
			prompt = colorGrey + "—" + ansiReset
			ttft = colorGrey + "—" + ansiReset
		} else {
			eff := mdl.LiveOutputTPS
			if eff == 0 {
				eff = mdl.OutputTPS
			}
			if eff > 0 {
				tps = colorGreen + ansiBold + fmt.Sprintf("%.1f", eff) + ansiReset
			} else {
				tps = colorGrey + "—" + ansiReset
			}
			if mdl.InputTPS > 0 {
				prompt = colorGreen + fmt.Sprintf("%.1f", mdl.InputTPS) + ansiReset
			} else {
				prompt = colorGrey + "—" + ansiReset
			}
			if mdl.FirstTokenIn > 0 {
				ttft = colorGreen + formatDur(mdl.FirstTokenIn) + ansiReset
			} else {
				ttft = colorGrey + "—" + ansiReset
			}
		}

		var status string
		switch mdl.Status {
		case "thinking":
			cnt := ""
			if mdl.ActiveCount > 1 {
				cnt = fmt.Sprintf("(%d)", mdl.ActiveCount)
			}
			status = colorPink + "● thinking" + cnt + ansiReset
		case "running":
			cnt := ""
			if mdl.ActiveCount > 1 {
				cnt = fmt.Sprintf("(%d)", mdl.ActiveCount)
			}
			status = colorGreen + "● running" + cnt + ansiReset
		default:
			status = colorGrey + "○ idle" + ansiReset
		}

		expires := colorGrey + formatDuration(mdl.UntilExpiry) + ansiReset

		row := " " + padRight(name, colModel) +
			" " + padRight(size, colSize) +
			" " + padRight(vram, colVRAM) +
			" " + padRight(tps, colTokSec) +
			" " + padRight(prompt, colPrompt) +
			" " + padRight(ttft, colTTFT) +
			" " + padRight(status, colStatus) +
			" " + padRight(expires, colExpires)

		lines = append(lines, borderedLine(inner, row))

		if mdl.Status == "thinking" || mdl.Status == "running" {
			var details []string
			if mdl.FirstTokenIn > 0 {
				details = append(details, colorGrey+"TTFT "+ansiReset+colorGreen+formatDur(mdl.FirstTokenIn)+ansiReset)
			}
			if mdl.ThinkTokens > 0 {
				ti := fmt.Sprintf("%d tok", mdl.ThinkTokens)
				if mdl.ThinkElapsed > 0 {
					ti += " " + formatDur(mdl.ThinkElapsed)
				}
				if mdl.ThinkTPS > 0 {
					ti += fmt.Sprintf(" %.1f tok/s", mdl.ThinkTPS)
				}
				details = append(details, colorGrey+"thinking "+ansiReset+colorPink+ti+ansiReset)
			}
			if mdl.FirstRespIn > 0 {
				details = append(details, colorGrey+"TTFR "+ansiReset+colorGreen+formatDur(mdl.FirstRespIn)+ansiReset)
			}
			if len(details) > 0 {
				indent := strings.Repeat(" ", colModel+2)
				lines = append(lines, borderedLine(inner, indent+strings.Join(details, colorGrey+"  "+ansiReset)))
			}
		}
		if mdl.Status == "idle" && snap.HasCapture && mdl.LastTotalDuration > 0 {
			indent := strings.Repeat(" ", colModel+2)
			detail := colorGrey + "last " + ansiReset + colorGreen + formatDur(mdl.LastTotalDuration) + ansiReset
			if mdl.LastMsPerToken > 0 {
				detail += colorGrey + "  " + ansiReset + colorGreen + fmt.Sprintf("%.1f ms/tok", mdl.LastMsPerToken) + ansiReset
			}
			lines = append(lines, borderedLine(inner, indent+detail))
		}
	}

	lines = append(lines, borderedLine(inner, ""))
	return lines
}


func renderThroughput(inner int, snap store.View) []string {
	perf := snap.Perf
	sparkW := 20
	since := ""
	if !perf.WindowStart.IsZero() {
		since = perf.WindowStart.Local().Format("15:04")
	}

	var lines []string
	lines = append(lines, borderedLine(inner, sparkRow("tok/s  ", perf.OutputHistory, perf.OutputTPS, perf.PeakOutputTPS, since, perf.PeakBuckets, sparkW)))
	lines = append(lines, borderedLine(inner, sparkRow("prompt ", perf.InputHistory, perf.InputTPS, perf.PeakInputTPS, since, perf.PeakBuckets, sparkW)))

	if perf.CompletionsPerSec > 0 || perf.MeanLatency > 0 {
		lat := " " + colorGrey + "latency" + ansiReset
		lat += "  avg " + colorGreen + ansiBold + formatMS(float64(perf.MeanLatency.Milliseconds())) + ansiReset
		lat += "  p95 " + colorAmber + formatMS(float64(perf.P95Latency.Milliseconds())) + ansiReset
		lat += "  p99 " + colorRed + formatMS(float64(perf.P99Latency.Milliseconds())) + ansiReset
		lat += "  " + colorGrey + fmt.Sprintf("%.2f req/s", perf.CompletionsPerSec) + ansiReset
		if perf.MeanMsPerToken > 0 {
			lat += "  " + colorGrey + fmt.Sprintf("%.1f ms/tok", perf.MeanMsPerToken) + ansiReset
		}
		lines = append(lines, borderedLine(inner, lat))
	}

	if snap.Sys.GPUPowerW > 0 {
		pwr := " " + colorGrey + "power  " + ansiReset
		pwr += "  GPU " + colorGreen + ansiBold + fmt.Sprintf("%.1fW", snap.Sys.GPUPowerW) + ansiReset
		if snap.Sys.TokPerWatt > 0 {
			pwr += "  " + colorGreen + fmt.Sprintf("%.1f tok/W", snap.Sys.TokPerWatt) + ansiReset
		}
		lines = append(lines, borderedLine(inner, pwr))
	}

	lines = append(lines, borderedLine(inner, ""))
	return lines
}

var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func sparkRow(label string, hist []float64, cur, maxV float64, since string, activeBuckets, sparkW int) string {
	spark := buildSparkline(hist, sparkW, activeBuckets, colorGreen)
	var val string
	if cur > 0 {
		val = colorGreen + ansiBold + fmt.Sprintf("%.1f tok/s", cur) + ansiReset
	} else {
		val = colorGrey + "0.0 tok/s" + ansiReset
	}
	suffix := ""
	if maxV > 0 {
		suffix = colorGrey + fmt.Sprintf("  max %.1f", maxV) + ansiReset
		if since != "" {
			suffix += colorGrey + "  since " + since + ansiReset
		}
	}
	return " " + colorGrey + label + ansiReset + " " + spark + "   " + val + suffix
}

func buildSparkline(data []float64, width, activeBuckets int, color string) string {
	if activeBuckets > width {
		activeBuckets = width
	}
	start := 0
	if len(data) > width {
		start = len(data) - width
	}
	visible := data[start:]

	maxVal := 0.0
	for _, v := range visible {
		if v > maxVal {
			maxVal = v
		}
	}

	inactiveCols := width - activeBuckets
	var sb strings.Builder
	if inactiveCols > 0 {
		sb.WriteString(colorGrey + strings.Repeat("·", inactiveCols) + ansiReset)
	}

	activeStart := 0
	if len(visible) > activeBuckets {
		activeStart = len(visible) - activeBuckets
	}
	activeData := visible[activeStart:]
	padCount := activeBuckets - len(activeData)
	if padCount > 0 {
		sb.WriteString(color + strings.Repeat(string(sparkBlocks[0]), padCount) + ansiReset)
	}

	var active strings.Builder
	for _, v := range activeData {
		idx := 0
		if maxVal > 0 {
			idx = int(math.Round(v / maxVal * float64(len(sparkBlocks)-1)))
			if idx >= len(sparkBlocks) {
				idx = len(sparkBlocks) - 1
			}
		}
		active.WriteRune(sparkBlocks[idx])
	}
	sb.WriteString(color + active.String() + ansiReset)
	return sb.String()
}


func renderSystem(inner int, sys sysinfo.Info) []string {
	barW := 16
	sparkW := inner - 6 - barW - 10
	if sparkW < 8 {
		sparkW = 8
	}
	if sparkW > 40 {
		sparkW = 40
	}

	var lines []string

	cpuColor := tempColor(sys.CPUTempC, 70, 90)
	cpuLine := " CPU  " + renderBar(sys.CPUPercent, barW) + fmt.Sprintf("  %3.0f%%", sys.CPUPercent)
	if sys.SensorsAvail && sys.CPUTempC > 0 {
		cpuLine += "  " + cpuColor + fmt.Sprintf("%3.0f°C", sys.CPUTempC) + ansiReset
		if len(sys.CPUTempHistory) > 0 {
			cpuLine += " " + buildSparkline(sys.CPUTempHistory, sparkW, sys.ActiveBuckets, cpuColor)
		}
	}
	lines = append(lines, borderedLine(inner, cpuLine))

	if sys.GPUAvail {
		gpuColor := tempColor(sys.GPUTempC, 75, 95)
		gpuLabel := "GPU"
		if sys.GPUName != "" {
			gpuLabel = truncate(sys.GPUName, 8)
		}
		gpuLine := " " + padStr(gpuLabel, 4) + " " + renderBar(sys.GPUPercent, barW) + fmt.Sprintf("  %3.0f%%", sys.GPUPercent)
		if sys.GPUTempC > 0 {
			gpuLine += "  " + gpuColor + fmt.Sprintf("%3.0f°C", sys.GPUTempC) + ansiReset
			if len(sys.GPUTempHistory) > 0 {
				gpuLine += " " + buildSparkline(sys.GPUTempHistory, sparkW, sys.ActiveBuckets, gpuColor)
			}
		}
		lines = append(lines, borderedLine(inner, gpuLine))
	}

	ramLine := " RAM  " + renderBar(sys.MemPercent, barW) +
		fmt.Sprintf("  %3.0f%%", sys.MemPercent) +
		"  " + formatBytesU(sys.MemUsedB) + " / " + formatBytesU(sys.MemTotalB)
	lines = append(lines, borderedLine(inner, ramLine))

	loadLine := colorGrey + fmt.Sprintf(" Load: %.2f  %.2f  %.2f", sys.LoadAvg1, sys.LoadAvg5, sys.LoadAvg15) + ansiReset
	lines = append(lines, borderedLine(inner, loadLine))
	lines = append(lines, borderedLine(inner, ""))

	return lines
}

func renderBar(pct float64, width int) string {
	filled := int(math.Round(pct / 100 * float64(width)))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return colorGreen + strings.Repeat("█", filled) + ansiReset +
		colorGrey + strings.Repeat("░", width-filled) + ansiReset
}

func tempColor(temp, warn, crit float64) string {
	if temp >= crit {
		return colorRed
	}
	if temp >= warn {
		return colorAmber
	}
	return colorGreen
}


func renderFooter(w int, sortMode SortMode, webAddr string) string {
	web := ""
	if webAddr != "" {
		web = "  [w]web: http://" + webAddr
	}
	content := colorGrey + " [q]quit  [s]sort:" + sortMode.String() + "  [?]help" + web + ansiReset
	pad := w - 2 - visLen(content)
	if pad < 0 {
		pad = 0
	}
	return colorBorder + "└" + ansiReset + content + strings.Repeat(" ", pad) + colorBorder + "┘" + ansiReset
}

func renderHelp() string {
	var b strings.Builder
	b.WriteString("\r\n")
	b.WriteString(colorBorder + "  ┌─ Keyboard shortcuts " + strings.Repeat("─", 30) + "┐\r\n" + ansiReset)
	for _, row := range [][2]string{
		{"q / Ctrl+C", "quit"},
		{"s", "cycle sort: default → name → tok/s → VRAM → status"},
		{"?", "toggle this help"},
	} {
		line := fmt.Sprintf("  │  %s%-12s%s  %s%-50s%s  │",
			colorGreen+ansiBold, row[0], ansiReset,
			colorGrey, row[1], ansiReset)
		b.WriteString(line + "\r\n")
	}
	b.WriteString(colorBorder + "  └" + strings.Repeat("─", 66) + "┘\r\n" + ansiReset)
	return b.String()
}


func formatBytes(b int64) string { return formatBytesU(uint64(b)) }
func formatBytesU(b uint64) string {
	const gb = 1 << 30
	const mb = 1 << 20
	if b >= gb {
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	}
	return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
}

func formatMS(ms float64) string {
	if ms <= 0 {
		return "—"
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

func formatDur(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func visLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
}

func padRight(s string, width int) string {
	vis := visLen(s)
	if vis >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vis)
}

func padStr(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return strings.Repeat(".", maxLen)
	}
	return s[:maxLen-3] + "..."
}

func termSize() (int, int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80, 24
	}
	return w, h
}
