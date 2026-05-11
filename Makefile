.PHONY: build build-amd64 build-arm64 clean run run-full test lint bpf-generate bpf-check

VERSION  ?= dev
BINARY   := ollamaHtop
CMD      := ./cmd/ollamaHtop
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"
GOOS     ?= linux
GOARCH   ?= amd64

# Build binary (default: Linux amd64)
build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) $(CMD)

# Build Linux x86_64/amd64 binary
build-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY)-amd64 $(CMD)

# Build Linux arm64 binary (Raspberry Pi, ARM servers)
build-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY)-arm64 $(CMD)

# Remove compiled binaries
clean:
	rm -f $(BINARY) $(BINARY)-amd64 $(BINARY)-arm64

# Run locally (no eBPF)
run: build
	./$(BINARY)

# Run with eBPF and web dashboard (requires root/CAP_NET_ADMIN)
run-full: build
	sudo ./$(BINARY) -ebpf -web-port 9090

# Run unit tests
test:
	go test ./...

# Lint (requires golangci-lint)
lint:
	golangci-lint run ./...

# Regenerate eBPF artifacts (requires clang + llvm + libelf-dev on Linux)
bpf-generate:
	cd internal/ebpf && go generate ./...

# Validate: regenerate eBPF artifacts and verify build succeeds
bpf-check: bpf-generate
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...
