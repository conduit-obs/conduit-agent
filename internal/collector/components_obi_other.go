//go:build !linux

// Package collector — non-Linux stub for OBI receiver registration.
//
// OBI's eBPF-driven instrumentation is Linux-only by upstream design
// (kernel 5.8+, libbpf, BTF). On macOS and Windows we ship the same
// single binary minus OBI; this file's no-op `addPlatformReceivers`
// keeps the components.go call site tag-free without forcing
// non-Linux builds to resolve the `go.opentelemetry.io/obi` module.
//
// See components_obi_linux.go for the Linux-side implementation and
// ADR-0020 sub-decision 1 for why "OBI compiled in on Linux only" is
// the chosen shape.

package collector

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/receiver"
)

// addPlatformReceivers (non-Linux build) is a no-op. The conduit
// binary on macOS / Windows carries no OBI receiver — operators on
// those platforms cannot run OBI even if the binary exposed the
// factory, since the eBPF receiver fails at startup outside Linux.
func addPlatformReceivers(
	_ map[component.Type]receiver.Factory,
	_ map[component.Type]string,
) {
}
