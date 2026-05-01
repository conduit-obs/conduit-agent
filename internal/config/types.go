// Package config defines the Conduit agent configuration schema (conduit.yaml)
// and provides loading, defaulting, and validation. The schema is deliberately
// vendor-neutral and small in V0; see ADR-0007 (declarative output block) and
// docs/adr/adr-0008.md (cardinality guard) for the rationale.
//
// The shape of this package is the source of truth for what conduit.yaml
// accepts. internal/expander consumes AgentConfig (already validated and
// defaulted) to produce upstream OTel Collector YAML.
package config

// AgentConfig is the root of conduit.yaml. The fields here are stable surface
// area; additions are additive (new optional field), removals are breaking
// changes that must go through ADR review and SemVer (see ADR-0014).
type AgentConfig struct {
	// ServiceName is required. It populates resource attribute service.name on
	// every emitted signal. Whether to allow per-instrumentation override or
	// pin to this value is settled by OPEN-DECISION-7 and tracked in
	// conduit-agent-plan/13-decision-log.md.
	ServiceName string `yaml:"service_name"`

	// DeploymentEnvironment is required. It populates resource attribute
	// deployment.environment on every emitted signal.
	DeploymentEnvironment string `yaml:"deployment_environment"`

	// Output declares where Conduit sends data: directly to Honeycomb, or to
	// any OTLP-capable gateway. Exactly one of Honeycomb / Gateway must be
	// populated, matching Mode. See ADR-0007.
	Output Output `yaml:"output"`

	// Profile selects which platform-default fragment set to layer on top of
	// the always-on OTLP receiver. When omitted entirely, defaults are
	// applied as if the user wrote {mode: auto, host_metrics: true,
	// system_logs: true}. Set to {mode: none} to disable all platform
	// defaults and run OTLP-only (the M2 behavior).
	Profile *Profile `yaml:"profile,omitempty"`
}

// ProfileMode discriminates which platform fragment set is loaded.
//
// Conduit V0 ships fragments for "linux" and "darwin"; "windows" lands at M6.
// "docker" lands at M4 and tells the expander to bind OTLP receivers to
// 0.0.0.0 (so peer containers can reach them) — its host-metrics fragment
// is intentionally empty in V0 because scraping /proc and /sys from inside
// a container requires bind mounts the user must opt into. "k8s" lands at
// M5 with the same OTLP-bind=0.0.0.0 behavior; M5.B adds the kubeletstats
// + container-log + k8sattributes fragment set the Helm chart wires up by
// default. "auto" detects runtime.GOOS at expansion time and chooses the
// matching fragment set, falling back to "none" with a warning when it
// doesn't have one for that OS. "none" disables all platform defaults so
// the binary behaves like the M2-era OTLP-only collector.
type ProfileMode string

const (
	ProfileModeAuto   ProfileMode = "auto"
	ProfileModeLinux  ProfileMode = "linux"
	ProfileModeDarwin ProfileMode = "darwin"
	ProfileModeDocker ProfileMode = "docker"
	ProfileModeK8s    ProfileMode = "k8s"
	ProfileModeNone   ProfileMode = "none"
)

// Profile turns the platform default fragments (host metrics, system log
// files, etc.) on or off.
type Profile struct {
	// Mode selects the fragment set; see ProfileMode. Defaults to "auto".
	Mode ProfileMode `yaml:"mode,omitempty"`

	// HostMetrics toggles the platform's hostmetrics receiver fragment.
	// Pointer so we can distinguish "field omitted" from "set to false";
	// nil means "use the default for the resolved Mode" (true for linux /
	// darwin, false for none).
	HostMetrics *bool `yaml:"host_metrics,omitempty"`

	// SystemLogs toggles the platform's system-log fragment (filelog,
	// journald on Linux, etc.). Pointer for the same reason as HostMetrics.
	SystemLogs *bool `yaml:"system_logs,omitempty"`
}

// HostMetricsEnabled returns the effective host_metrics setting given the
// resolved Mode, applying the "default true unless mode=none" rule.
func (p *Profile) HostMetricsEnabled() bool {
	if p == nil || p.Mode == ProfileModeNone {
		return false
	}
	if p.HostMetrics == nil {
		return true
	}
	return *p.HostMetrics
}

// SystemLogsEnabled returns the effective system_logs setting given the
// resolved Mode, applying the "default true unless mode=none" rule.
func (p *Profile) SystemLogsEnabled() bool {
	if p == nil || p.Mode == ProfileModeNone {
		return false
	}
	if p.SystemLogs == nil {
		return true
	}
	return *p.SystemLogs
}

// OutputMode is the discriminator for the Output block.
type OutputMode string

const (
	// OutputModeHoneycomb sends OTLP/HTTP directly to Honeycomb's ingest URL.
	OutputModeHoneycomb OutputMode = "honeycomb"
	// OutputModeGateway sends OTLP (gRPC) to a customer-operated gateway —
	// any OTLP-capable collector, including Honeycomb's own gateway. The
	// gateway is responsible for downstream destinations.
	OutputModeGateway OutputMode = "gateway"
)

// Output declares Conduit's egress. Only one nested block (Honeycomb or
// Gateway) may be populated, and it must match Mode.
type Output struct {
	Mode      OutputMode       `yaml:"mode"`
	Honeycomb *HoneycombOutput `yaml:"honeycomb,omitempty"`
	Gateway   *GatewayOutput   `yaml:"gateway,omitempty"`
}

// HoneycombOutput configures direct egress to Honeycomb.
type HoneycombOutput struct {
	// APIKey is the Honeycomb ingest key (header x-honeycomb-team). Required.
	// May reference an environment variable using OTel's standard ${env:NAME}
	// syntax, which the embedded collector resolves at startup.
	APIKey string `yaml:"api_key"`

	// Endpoint overrides Honeycomb's ingest URL. Optional; defaults to
	// https://api.honeycomb.io. Useful for EU tenants (api.eu1.honeycomb.io)
	// or testing against a sandbox.
	Endpoint string `yaml:"endpoint,omitempty"`
}

// DefaultHoneycombEndpoint is the production US Honeycomb ingest URL.
const DefaultHoneycombEndpoint = "https://api.honeycomb.io"

// GatewayOutput configures egress to a customer-operated OTLP gateway.
type GatewayOutput struct {
	// Endpoint is the gateway's OTLP/gRPC URL. Required.
	Endpoint string `yaml:"endpoint"`

	// Headers are additional headers to attach to every export. Optional.
	// Use this for gateway-specific auth (e.g. an API key) when the gateway
	// requires one.
	Headers map[string]string `yaml:"headers,omitempty"`
}
