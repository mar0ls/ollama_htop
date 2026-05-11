//go:build linux

// Package ebpf attaches a TC BPF hook to capture Ollama traffic on port 11434.
// Requires kernel 6.6+ (TCX API) and CAP_NET_ADMIN.
package ebpf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"syscall"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

// Monitor attaches a TC BPF hook and dispatches Completion/LiveUpdate events.
type Monitor struct {
	iface        string
	completionCh chan<- Completion
	liveUpdateCh chan<- LiveUpdate
}

// New creates a Monitor for the given network interface.
func New(iface string, completionCh chan<- Completion, liveUpdateCh chan<- LiveUpdate) *Monitor {
	return &Monitor{
		iface:        iface,
		completionCh: completionCh,
		liveUpdateCh: liveUpdateCh,
	}
}

// Available checks eBPF TCX preconditions: kernel ≥ 6.6, memlock, ringbuf.
func Available() bool {
	if !kernelAtLeast(6, 6) {
		return false
	}
	if err := rlimit.RemoveMemlock(); err != nil {
		return false
	}
	m, err := ebpf.NewMap(&ebpf.MapSpec{
		Type:       ebpf.RingBuf,
		MaxEntries: 4096,
	})
	if err != nil {
		return false
	}
	_ = m.Close()
	return true
}

// kernelAtLeast returns true if the running Linux kernel is >= major.minor.
func kernelAtLeast(major, minor int) bool {
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return false
	}
	rel := make([]byte, 0, len(uts.Release))
	for _, c := range uts.Release {
		if c == 0 {
			break
		}
		rel = append(rel, byte(c))
	}
	s := string(rel)
	var maj, min int
	if _, err := fmt.Sscanf(s, "%d.%d", &maj, &min); err != nil {
		return false
	}
	if maj != major {
		return maj > major
	}
	return min >= minor
}

// Run attaches the BPF program and processes ring buffer events until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) error {
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("ebpf: remove memlock: %w", err)
	}

	iface, err := net.InterfaceByName(m.iface)
	if err != nil {
		return fmt.Errorf("ebpf: interface %q not found: %w", m.iface, err)
	}

	objs := BpfOllamaObjects{}
	if err := LoadBpfOllamaObjects(&objs, nil); err != nil {
		return fmt.Errorf("ebpf: load BPF objects: %w", err)
	}
	defer objs.Close() //nolint:errcheck

	lnk, err := link.AttachTCX(link.TCXOptions{
		Interface: iface.Index,
		Program:   objs.CaptureOllama,
		Attach:    ebpf.AttachTCXIngress,
	})
	if err != nil {
		return fmt.Errorf("ebpf: attach TCX on %s (requires kernel 6.6+): %w", m.iface, err)
	}
	defer lnk.Close() //nolint:errcheck
	slog.Info("ebpf: TC hook attached", "iface", m.iface, "port", 11434)

	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		return fmt.Errorf("ebpf: ring buffer reader: %w", err)
	}
	defer rd.Close() //nolint:errcheck

	go func() {
		<-ctx.Done()
		_ = rd.Close()
	}()

	tracker := newStreamTracker(m.completionCh, m.liveUpdateCh)

	for {
		record, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return nil
			}
			slog.Warn("ebpf: ring buffer read", "err", err)
			continue
		}

		if len(record.RawSample) < int(unsafe.Sizeof(tcpEvent{})) {
			continue
		}
		ev := (*tcpEvent)(unsafe.Pointer(&record.RawSample[0]))
		tracker.handleEvent(ev)
	}
}
