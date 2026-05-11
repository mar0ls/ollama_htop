package main

import "testing"

func TestResolveEBPFIfaceLocalhost(t *testing.T) {
	iface, err := resolveEBPFIface("http://localhost:11434")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface != "lo" {
		t.Fatalf("iface = %q, want %q", iface, "lo")
	}
}

func TestResolveEBPFIfaceLoopbackIPv4(t *testing.T) {
	iface, err := resolveEBPFIface("http://127.0.0.1:11434")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface != "lo" {
		t.Fatalf("iface = %q, want %q", iface, "lo")
	}
}

func TestResolveEBPFIfaceIPv6LiteralUnsupported(t *testing.T) {
	iface, err := resolveEBPFIface("http://[::1]:11434")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface != "" {
		t.Fatalf("iface = %q, want empty for IPv6 literal", iface)
	}
}

func TestResolveEBPFIfaceInvalidURLFallsBackToLo(t *testing.T) {
	iface, err := resolveEBPFIface("://bad-url")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface != "lo" {
		t.Fatalf("iface = %q, want lo", iface)
	}
}

func TestResolveEBPFIfaceIPv6DocumentationAddressUnsupported(t *testing.T) {
	iface, err := resolveEBPFIface("http://[2001:db8::1]:11434")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface != "" {
		t.Fatalf("iface = %q, want empty for IPv6", iface)
	}
}

func TestResolveEBPFIfaceEmptyHostFallsBackToLo(t *testing.T) {
	iface, err := resolveEBPFIface("http://:11434")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface != "lo" {
		t.Fatalf("iface = %q, want lo", iface)
	}
}

func TestResolveEBPFIfaceLocalhostWithoutPort(t *testing.T) {
	iface, err := resolveEBPFIface("http://localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface != "lo" {
		t.Fatalf("iface = %q, want lo", iface)
	}
}
