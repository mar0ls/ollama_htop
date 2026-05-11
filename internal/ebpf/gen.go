package ebpf

// Regenerate BPF bytecode (requires clang + libbpf-dev on Linux):
//
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror -I/usr/include/x86_64-linux-gnu" BpfOllama ./bpf/tc_ollama.c
