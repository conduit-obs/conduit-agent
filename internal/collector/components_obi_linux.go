//go:build linux

// Package collector — Linux-only OBI receiver registration.
//
// This file is the single point in the agent that imports
// `go.opentelemetry.io/obi/collector`. It is gated on `//go:build
// linux` so non-Linux builds (macOS / Windows test runners, dev
// laptops on those OSes) compile cleanly without needing the OBI
// module resolved — the matching components_obi_other.go provides a
// no-op `addPlatformReceivers` for those builds.
//
// Per ADR-0020 sub-decision 1 ("single Linux binary, OBI compiled in
// unconditionally"), every Linux build of conduit-agent links the
// OBI receiver. Operators flip it on at runtime via `obi.enabled` in
// conduit.yaml (default-on for the k8s profile, default-off
// elsewhere) — but the binary always carries it. That keeps the .deb
// / .rpm / image / Helm chart matrix at one Linux variant rather
// than fanning out into "lean" vs "obi" releases.
//
// Build resolution: `go.opentelemetry.io/obi/collector` is resolved
// via the require + replace directives that `make obi-vendor`
// injects into go.mod after cloning the OBI source into
// third_party/obi/ and running its `make docker-generate` step. The
// CI workflow at .github/workflows/ci.yml runs `make obi-vendor` on
// the Linux runner before any Go toolchain step; a Linux developer
// on a fresh checkout sees `cannot find module providing package
// go.opentelemetry.io/obi/collector` until they run `make obi-vendor`
// once, at which point local builds, tests, and lint all see OBI
// linked. This is intentional — the alternative ("OBI optional on
// Linux") would split the support matrix and contradict
// ADR-0020 sub-decision 1.

package collector

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/receiver"
	obicollector "go.opentelemetry.io/obi/collector"
)

// addPlatformReceivers (Linux build) registers the OBI receiver
// factory + its module-string entry. The signature matches the
// non-Linux stub so components.go's call site stays tag-free.
func addPlatformReceivers(
	factories map[component.Type]receiver.Factory,
	modules map[component.Type]string,
) {
	f := obicollector.NewFactory()
	factories[f.Type()] = f
	modules[f.Type()] = "go.opentelemetry.io/obi v0.8.0"
}
