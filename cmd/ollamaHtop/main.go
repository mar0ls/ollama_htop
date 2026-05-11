package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"ollamaHtop/internal/ebpf"
	"ollamaHtop/internal/ollama"
	"ollamaHtop/internal/store"
	"ollamaHtop/internal/sysinfo"
	"ollamaHtop/internal/tui"
	"ollamaHtop/internal/web"
)

var version = "dev"

func main() {
	fs := flag.NewFlagSet("ollamaHtop", flag.ExitOnError)
	host := fs.String("host", "", "Ollama address (default: $OLLAMA_HOST or http://localhost:11434)")
	useEBPF := fs.Bool("ebpf", false, "Transparent eBPF tok/s monitoring (requires root/CAP_NET_ADMIN, kernel 6.6+)")
	webPort := fs.Int("web-port", 9090, "Web dashboard port (0 = disabled)")
	debug := fs.Bool("debug", false, "Write debug log to ollamaHtop.log")
	showVer := fs.Bool("version", false, "Print version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "flag error: %v\n", err)
		os.Exit(1)
	}

	if *showVer {
		fmt.Printf("ollamaHtop %s\n", version)
		os.Exit(0)
	}

	if *debug {
		f, err := os.OpenFile("ollamaHtop.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot open log file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close() //nolint:errcheck
		slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	}

	ollamaHost := "http://localhost:11434"
	if env := os.Getenv("OLLAMA_HOST"); env != "" {
		ollamaHost = env
	}
	if *host != "" {
		ollamaHost = *host
	}

	slog.Info("ollamaHtop start", "version", version, "host", ollamaHost)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	hasCapture := false
	completionCh := make(chan ebpf.Completion, 64)
	liveUpdateCh := make(chan ebpf.LiveUpdate, 64)

	if *useEBPF {
		if !ebpf.Available() {
			fmt.Fprintln(os.Stderr, "ERROR: eBPF not available (requires root/CAP_NET_ADMIN and kernel 6.6+)")
			os.Exit(1)
		}
		iface, err := resolveEBPFIface(ollamaHost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "eBPF: cannot determine network interface: %v\n", err)
			os.Exit(1)
		}
		if iface == "" {
			fmt.Fprintln(os.Stderr, "eBPF: IPv6 Ollama hosts are not supported (capture is IPv4-only)")
			os.Exit(1)
		}
		mon := ebpf.New(iface, completionCh, liveUpdateCh)
		hasCapture = true
		fmt.Fprintf(os.Stderr, "eBPF: monitoring port 11434 on %s\n", iface)
		go func() {
			if err := mon.Run(ctx); err != nil {
				slog.Error("ebpf monitor exited", "err", err)
			}
		}()
	}

	st := store.New(hasCapture)

	stateCh := make(chan ollama.State, 8)
	poller := ollama.NewPoller(ollamaHost)
	go poller.Run(ctx, 1*time.Second, stateCh)

	var webSrv *web.Server
	webAddr := ""
	if *webPort > 0 {
		webAddr = "0.0.0.0:" + strconv.Itoa(*webPort)
		webSrv = web.New(webAddr)
		go func() {
			if err := webSrv.Run(ctx); err != nil {
				slog.Error("web server", "err", err)
			}
		}()
	}

	tuiDisplay := ""
	if *webPort > 0 {
		tuiDisplay = fmt.Sprintf("localhost:%d", *webPort)
	}
	t := tui.New(ollamaHost, tuiDisplay)

	keyCh := make(chan tui.Key, 4)
	go tui.ReadInput(keyCh)

	// Main loop: fan-in all updates, tick the UI every 500 ms.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return

			case st2, ok := <-stateCh:
				if !ok {
					return
				}
				st.SetOllamaState(st2)

			case c, ok := <-completionCh:
				if !ok {
					return
				}
				st.PushCompletion(c)

			case u, ok := <-liveUpdateCh:
				if !ok {
					return
				}
				st.PushLiveUpdate(u)

			case <-ticker.C:
				st.SetSysInfo(sysinfo.Collect())
				v := st.Snapshot()
				t.UpdateSnapshot(v)
				if webSrv != nil {
					webSrv.Push(v)
				}
			}
		}
	}()

	t.Run(keyCh)
	cancel()
}

// resolveEBPFIface returns the local interface whose traffic reaches ollamaHost.
// For localhost it returns "lo"; for remote hosts it uses a UDP routing lookup.
func resolveEBPFIface(ollamaHost string) (string, error) {
	u, err := url.Parse(ollamaHost)
	if err != nil {
		return "lo", nil
	}
	h := u.Hostname()
	if h == "" || h == "localhost" || h == "127.0.0.1" {
		return "lo", nil
	}
	// IPv6 literal hosts are not supported by the IPv4-only TC program.
	if ip := net.ParseIP(h); ip != nil && ip.To4() == nil {
		return "", nil
	}
	port := u.Port()
	if port == "" {
		port = "11434"
	}

	// UDP dial does a routing lookup without sending any packets.
	conn, err := net.Dial("udp", net.JoinHostPort(h, port))
	if err != nil {
		return "", fmt.Errorf("routing lookup to %s: %w", h, err)
	}
	defer conn.Close() //nolint:errcheck
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || localAddr.IP.To4() == nil {
		return "", nil // resolved to IPv6 — unsupported
	}
	localIP := localAddr.IP

	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.Equal(localIP) {
				return iface.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no interface found for local IP %s", localIP)
}
