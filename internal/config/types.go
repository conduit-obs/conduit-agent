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
	// every emitted signal.
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

	// Metrics configures Conduit's derived-metrics behavior. The only V0
	// knob is the RED-from-spans connector (metrics.red); future V1 / V2
	// expansions (custom metric pipelines, prometheusreceiver scrape
	// configs, etc.) attach here so the schema doesn't grow a top-level
	// field per metric source. nil = full defaults (RED enabled,
	// documented dimension set, 5000-combination cardinality limit).
	Metrics *Metrics `yaml:"metrics,omitempty"`

	// OBI toggles the [OpenTelemetry eBPF Instrumentation
	// project](https://opentelemetry.io/docs/zero-code/obi/) — zero-code
	// application instrumentation that captures HTTP / gRPC /
	// JSON-RPC / database RED metrics and distributed trace spans
	// without code changes. Linux-only; the validator rejects
	// obi.enabled: true on non-Linux profiles. Off by default
	// everywhere except profile.mode=k8s, where applyDefaults flips
	// it on (k8s is OBI's strongest auto-discovery path; everywhere
	// else the cap-grant decision belongs to the operator). See
	// docs/adr/adr-0020.md for the integration rationale; the curated
	// schema is deliberately tiny (enabled + replace_span_metrics_connector)
	// because OBI's own auto-discovery handles the common case and ADR-0012
	// says "promote knobs when patterns emerge". Anything beyond these two
	// fields goes through Overrides (overrides.receivers.obi.<...>).
	OBI *OBI `yaml:"obi,omitempty"`

	// Overrides is the documented escape hatch for advanced users who need
	// to reach upstream OTel Collector knobs Conduit has not surfaced as
	// first-class fields. Any key under here is spliced verbatim into the
	// rendered Collector configuration as a second config source — the
	// embedded Collector deep-merges base + overrides at startup, with
	// overrides winning where they overlap (and lists replacing rather
	// than concatenating, matching upstream multi-config merge semantics).
	//
	// Heavy reliance on this field signals the schema is missing a
	// first-class knob; field engineers and PMs review patterns at retro
	// time and decide whether to promote them to typed fields. See
	// docs/adr/adr-0012.md for the design and review cadence; conduit
	// doctor's CDT0xxx checks (M11) warn when overrides is non-empty.
	//
	// Example — bumping kubeletstats collection interval:
	//
	//   overrides:
	//     receivers:
	//       kubeletstats:
	//         collection_interval: 15s
	//
	// Example — adding the redactionprocessor to the logs pipeline (note
	// that lists replace, so you must restate the full pipeline order):
	//
	//   overrides:
	//     processors:
	//       redaction:
	//         allow_all_keys: true
	//         blocked_values: ['(?i)password=\\S+']
	//     service:
	//       pipelines:
	//         logs:
	//           processors: [memory_limiter, resourcedetection, k8sattributes,
	//                        resource, transform/logs, redaction, batch]
	Overrides map[string]any `yaml:"overrides,omitempty"`
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
	ProfileModeAuto    ProfileMode = "auto"
	ProfileModeLinux   ProfileMode = "linux"
	ProfileModeDarwin  ProfileMode = "darwin"
	ProfileModeDocker  ProfileMode = "docker"
	ProfileModeK8s     ProfileMode = "k8s"
	ProfileModeWindows ProfileMode = "windows"
	ProfileModeNone    ProfileMode = "none"
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

// OBI toggles the OpenTelemetry eBPF Instrumentation receiver. The
// schema is deliberately minimal — two fields covering the two most
// consequential decisions (turn it on; suppress the M8 RED connector
// when OBI replaces it). Anything more elaborate (instrumentation
// targets, OBI features, k8s metadata enrichment, discovery poll
// interval) goes through AgentConfig.Overrides as overrides.receivers.
// obi.<...>, deep-merged at startup. See docs/adr/adr-0020.md for
// why the curated surface stays this small in V0.1.
type OBI struct {
	// Enabled toggles the OBI receiver. Pointer so applyDefaults can
	// distinguish "field omitted" (apply profile default — k8s -> true,
	// every other profile -> false) from "set to false" (operator
	// explicitly disabled OBI on a profile that would otherwise default
	// it on). Validation rejects Enabled = true on non-Linux profiles
	// because OBI is Linux-only by upstream design.
	Enabled *bool `yaml:"enabled,omitempty"`

	// ReplaceSpanMetricsConnector, when true, suppresses the M8
	// span_metrics connector emission entirely so OBI is the only
	// source of RED metrics. Default false (both sources flow; OBI's
	// native instrumentation.scope = "obi" tag disambiguates at query
	// time). Validation rejects this being true while Enabled is false
	// — the configuration would have no effect, surface that as an
	// error rather than letting it silently drift.
	ReplaceSpanMetricsConnector bool `yaml:"replace_span_metrics_connector,omitempty"`
}

// OBIEnabled reports the effective on/off state of the OBI receiver
// given the resolved Profile mode. The defaulting rule is "off
// everywhere except k8s" (per ADR-0020 sub-decision 4); applyDefaults
// runs first and pre-fills o.Enabled when it was nil, so by the time
// the expander asks this method o.Enabled is always non-nil. The
// nil-receiver / nil-Enabled paths exist for tests that hand-build an
// AgentConfig without going through Load/Parse.
func (o *OBI) OBIEnabled(p *Profile) bool {
	if o != nil && o.Enabled != nil {
		return *o.Enabled
	}
	return obiDefaultForProfile(p)
}

// obiDefaultForProfile is the central place that encodes the
// per-profile default. Same place applyDefaults reads from, same place
// OBIEnabled reads from when the operator omitted the block. K8s gets
// true; every other resolved profile gets false. Profile.Mode = auto
// is treated as "false until expander resolves auto -> goos" because
// auto-resolution happens at expand time and the runtime GOOS is the
// signal we'd want at that point — tests that hand-build cfg with
// mode=auto get false from this helper, which is the safe default for
// non-Linux test hosts.
func obiDefaultForProfile(p *Profile) bool {
	if p == nil {
		return false
	}
	return p.Mode == ProfileModeK8s
}

// OutputMode is the discriminator for the Output block. Three modes
// covering three distinct intents:
//
//   - honeycomb: named preset for Honeycomb's OTLP/HTTP ingest. Pre-wires
//     the x-honeycomb-team header and an endpoint default; the operator
//     only supplies the API key. Conduit will grow more named presets as
//     vendors prove they're worth the maintenance overhead (Datadog,
//     Grafana Cloud, etc. — promote them out of "use otlp mode" once we
//     have enough field signal).
//   - otlp: generic OTLP/HTTP egress. The escape hatch for any OTLP-HTTP
//     destination not yet covered by a named preset (Datadog OTLP intake,
//     Grafana Cloud OTLP, SigNoz Cloud, AWS ADOT, in-cluster collectors,
//     etc.). The operator supplies the endpoint and any required auth
//     headers.
//   - gateway: OTLP/gRPC egress to a customer-operated gateway collector.
//     The mental model is "fan out / aggregate at a gateway tier", not
//     "send directly to a vendor". gRPC is the typical wire protocol for
//     collector-to-collector flows.
type OutputMode string

const (
	// OutputModeHoneycomb sends OTLP/HTTP directly to Honeycomb's ingest
	// URL with the x-honeycomb-team header pre-wired.
	OutputModeHoneycomb OutputMode = "honeycomb"
	// OutputModeOTLP sends OTLP/HTTP to an arbitrary endpoint, with
	// caller-supplied headers for vendor auth. Use this for any
	// OTLP-HTTP destination Conduit doesn't yet ship a named preset for.
	OutputModeOTLP OutputMode = "otlp"
	// OutputModeGateway sends OTLP (gRPC) to a customer-operated gateway —
	// any OTLP-capable collector, including Honeycomb's own gateway. The
	// gateway is responsible for downstream destinations.
	OutputModeGateway OutputMode = "gateway"
)

// Output declares Conduit's egress. Exactly one nested block must be
// populated, and it must match Mode.
type Output struct {
	Mode      OutputMode       `yaml:"mode"`
	Honeycomb *HoneycombOutput `yaml:"honeycomb,omitempty"`
	OTLP      *OTLPOutput      `yaml:"otlp,omitempty"`
	Gateway   *GatewayOutput   `yaml:"gateway,omitempty"`

	// PersistentQueue (M10.A) backs each egress exporter's sending_queue
	// with the upstream filestorage extension instead of the default
	// in-memory queue. When enabled, OTLP batches that fail to deliver
	// (network blip, destination 5xx) are persisted to disk and replayed
	// on restart instead of being lost when the agent process exits.
	// Off by default because the dir must be writable + survive across
	// upgrades — operators on read-only roots, ephemeral containers, or
	// strict-FS-policy hosts opt in deliberately.
	PersistentQueue *PersistentQueue `yaml:"persistent_queue,omitempty"`
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

	// Traces (M10.B) routes the traces pipeline through a customer-
	// operated Refinery cluster instead of going direct to Honeycomb,
	// while metrics + logs continue straight to api.honeycomb.io. nil
	// means "send everything direct" (the default). The Refinery layer
	// applies tail-based sampling rules and forwards the kept traces to
	// Honeycomb itself, so this is a routing choice, not a destination
	// switch.
	Traces *HoneycombTraces `yaml:"traces,omitempty"`
}

// HoneycombTraces holds the per-signal routing knobs for direct-to-
// Honeycomb output. Today the only knob is via_refinery; future field
// additions (e.g. a sampling-rate hint for the SDK side) attach here so
// the schema doesn't grow a top-level field per traces concern.
type HoneycombTraces struct {
	// ViaRefinery, when set, routes traces through the configured
	// Refinery endpoint instead of api.honeycomb.io. Refinery accepts
	// OTLP/gRPC and forwards to Honeycomb on the same API key after
	// applying its sampling rules. Nil = send traces direct.
	ViaRefinery *RefineryRouting `yaml:"via_refinery,omitempty"`
}

// RefineryRouting points the traces pipeline at a Refinery cluster.
// Required when set; validation rejects empty Endpoint.
type RefineryRouting struct {
	// Endpoint is Refinery's OTLP/gRPC URL. Required. Typical values:
	//   https://refinery.observability.svc:4317     (in-cluster TLS)
	//   refinery.example.com:4317                   (operator-managed)
	// Schemes other than https produce a doctor (M11) warning unless
	// Insecure: true is also set.
	Endpoint string `yaml:"endpoint"`

	// Insecure skips TLS verification on the Refinery connection.
	// Default false; setting true is a lab-only override that doctor
	// (M11) flags as a warning even on success.
	Insecure bool `yaml:"insecure,omitempty"`
}

// PersistentQueue toggles disk-backed sending_queue for the configured
// egress exporter (M10.A). When enabled, the upstream filestorage
// extension persists in-flight OTLP batches under Dir; on restart the
// exporter resumes draining the queue instead of dropping anything that
// hadn't shipped before shutdown.
//
// Trade-offs Conduit doesn't hide:
//
//   - Dir must be writable, survive upgrades, and not be on a tmpfs
//     mount (defeats the purpose). The default is /var/lib/conduit/queue,
//     which the deb / rpm packages create with the right ownership.
//   - On Windows the default is %PROGRAMDATA%\Conduit\queue (set by
//     the MSI install — see deploy/windows/wix/conduit.wxs).
//   - On ECS Fargate / immutable container hosts there's no useful
//     persistent dir; PersistentQueue stays off and operators rely on
//     the SDK's retry budget.
//
// Sized for V0: queue_size: 1000 batches per signal (matches upstream
// default), num_consumers: 4. Operators tune via the overrides:
// escape hatch (ADR-0012); first-class knobs land if tuning patterns
// emerge.
type PersistentQueue struct {
	// Enabled toggles the disk-backed queue. Default false.
	Enabled bool `yaml:"enabled"`

	// Dir is the on-disk filestorage directory. Optional; defaults to
	// DefaultPersistentQueueDir when Enabled is true and Dir is empty.
	Dir string `yaml:"dir,omitempty"`
}

// DefaultHoneycombEndpoint is the production US Honeycomb ingest URL.
const DefaultHoneycombEndpoint = "https://api.honeycomb.io"

// DefaultPersistentQueueDir is the on-disk filestorage directory used
// when persistent_queue.enabled is true and persistent_queue.dir is
// empty. Matches /var/lib/conduit/ created by the linux nfpms maintainer
// scripts; Windows installs override via the MSI.
const DefaultPersistentQueueDir = "/var/lib/conduit/queue"

// OTLPOutput configures generic OTLP/HTTP egress. Use this for any
// OTLP-HTTP destination Conduit doesn't yet ship a named preset for —
// Datadog (https://otlp.<site>.datadoghq.com), Grafana Cloud
// (https://otlp-gateway-prod-<region>.grafana.net/otlp), SigNoz Cloud,
// AWS ADOT, in-cluster collectors with HTTP receivers, etc. The vendor's
// docs will tell you which header carries auth.
type OTLPOutput struct {
	// Endpoint is the OTLP/HTTP base URL. Required. Conduit appends
	// /v1/traces, /v1/metrics, and /v1/logs at request time per the
	// upstream otlphttp exporter convention.
	Endpoint string `yaml:"endpoint"`

	// Headers are additional HTTP headers to attach to every export.
	// Optional; the typical use is an auth token (e.g.
	// "Authorization: Bearer ${env:GRAFANA_CLOUD_OTLP_TOKEN}",
	// "DD-API-KEY: ${env:DD_API_KEY}", "x-honeycomb-team: ..."). May
	// reference environment variables via ${env:NAME}.
	Headers map[string]string `yaml:"headers,omitempty"`

	// Compression overrides the wire compression. Optional; defaults to
	// "gzip", which every modern OTLP/HTTP receiver supports. Set to
	// "none" only when the destination explicitly rejects compressed
	// payloads.
	Compression string `yaml:"compression,omitempty"`

	// Insecure skips TLS verification on the egress connection. Default
	// false. Setting this to true is a lab-only override per ADR-0009;
	// production destinations should always present a valid certificate.
	Insecure bool `yaml:"insecure,omitempty"`
}

// GatewayOutput configures egress to a customer-operated OTLP gateway.
type GatewayOutput struct {
	// Endpoint is the gateway's OTLP/gRPC URL. Required.
	Endpoint string `yaml:"endpoint"`

	// Headers are additional headers to attach to every export. Optional.
	// Use this for gateway-specific auth (e.g. an API key) when the gateway
	// requires one.
	Headers map[string]string `yaml:"headers,omitempty"`

	// Insecure skips TLS verification on the gateway connection (M10.C).
	// Default false — the rendered exporter ships an explicit
	// `tls.insecure: false` block so the TLS-required-by-default contract
	// is visible in `conduit preview` output. Setting true is a lab-only
	// override per ADR-0009; conduit doctor (M11) flags it as a warning
	// even when the connection succeeds (AC-06.3).
	Insecure bool `yaml:"insecure,omitempty"`
}

// Metrics is the umbrella for metric-pipeline tuning. V0 ships exactly
// one nested block (RED, the spans → request/error/duration tee); V1+
// will likely add fields here for prometheusreceiver scrape config,
// derived-metric rollups, etc.
type Metrics struct {
	// RED configures the span_metrics connector that tees RED metrics
	// (request count / error count / duration histogram) off the traces
	// pipeline. Lives before any sampling step so derived metrics see
	// 100% of traffic even when you tail-sample downstream. nil = use
	// the documented defaults (enabled, default dimension set, 5000
	// total-combination cardinality limit). See ADR-0006 (allowlist +
	// denylist model) and 04-milestone-plan.md §M8.
	RED *REDConfig `yaml:"red,omitempty"`
}

// REDConfig tunes the RED-from-spans connector. The defaults are
// chosen to be "the dimension set Datadog / Honeycomb / Grafana Cloud
// users would expect on a service map without lifting a finger" —
// service.name (built into the connector), deployment.environment,
// http.{route,method,status_code}, rpc.{system,service,method},
// messaging.{system,operation}. Operators with multi-tenant or
// regionalized workloads can add tenant-safe dimensions through
// SpanDimensions / ExtraResourceDimensions; high-cardinality
// attributes (raw IDs, paths, URLs) are blocked at validation time
// per the denylist in validate.go.
type REDConfig struct {
	// Enabled toggles RED-from-spans generation. Pointer so we can
	// distinguish "field omitted" from "set to false"; nil collapses
	// to true via applyDefaults. Set to false to skip rendering the
	// span_metrics connector entirely (e.g. when a downstream gateway
	// is the place running spanmetrics in your topology).
	Enabled *bool `yaml:"enabled,omitempty"`

	// SpanDimensions is appended to the default span-attribute
	// dimension list. Each entry must NOT be in the high-cardinality
	// denylist (trace_id, span_id, request_id, user.id, customer_id,
	// tenant_id, url.full, http.url, http.path, http.target). The
	// validator rejects denylisted entries with a CDT0501-mapped
	// error pointing at this field.
	SpanDimensions []string `yaml:"span_dimensions,omitempty"`

	// ExtraResourceDimensions is appended to the default resource-
	// attribute dimension list (service.name [implicit],
	// deployment.environment, k8s.namespace.name, cloud.region, team).
	// Same denylist as SpanDimensions applies — adding tenant_id at
	// the resource level is just as cardinality-explosive as adding
	// it at the span level.
	ExtraResourceDimensions []string `yaml:"extra_resource_dimensions,omitempty"`

	// CardinalityLimit caps the total number of unique dimension-value
	// combinations the connector tracks; excess combinations are
	// dropped into a single overflow series tagged
	// otel.metric.overflow="true". Defaults to 5000, which sits well
	// above a typical service's RED dimension fan-out (a few hundred)
	// but well below the cardinality wall that would make
	// span_metrics' working set unbounded. Maps directly to the
	// upstream connector's aggregation_cardinality_limit. conduit
	// doctor (M11) will surface the overflow-bucket count as
	// CDT0510 when it's non-zero.
	CardinalityLimit int `yaml:"cardinality_limit,omitempty"`
}

// REDEnabled reports the effective RED-on / RED-off setting.
func (r *REDConfig) REDEnabled() bool {
	if r == nil {
		return true
	}
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// DefaultREDCardinalityLimit caps the total dimension-combination
// fan-out the span_metrics connector retains. Mirrors the upstream
// connector's aggregation_cardinality_limit, which kicks excess
// combinations into a single overflow series instead of blowing up
// memory.
const DefaultREDCardinalityLimit = 5000

// REDDefaultSpanDimensions is the always-on span-dimension set the
// connector adds on top of its built-in service.name / span.name /
// span.kind / status.code dimensions. Every entry has been weighed
// against the cardinality denylist: http.route is the templated form
// (NOT raw http.target / http.url); http.method / http.status_code
// are bounded; rpc.* and messaging.* fan out by service shape, not
// by request.
var REDDefaultSpanDimensions = []string{
	"deployment.environment",
	"http.route",
	"http.method",
	"http.status_code",
	"rpc.system",
	"rpc.service",
	"rpc.method",
	"messaging.system",
	"messaging.operation",
}

// REDDefaultResourceDimensions is the always-on resource-dimension
// set. service.name is implicit (the connector emits it on every
// metric without prompting), but is included here so the rendered
// resource_metrics_key_attributes list is self-describing.
var REDDefaultResourceDimensions = []string{
	"service.name",
	"deployment.environment",
	"k8s.namespace.name",
	"cloud.region",
	"team",
}

// REDDefaultHistogramBuckets is the explicit bucket boundary set used
// for the duration histogram. Tuned for typical HTTP request latency
// (10ms..10s); deliberately fewer / wider buckets than upstream's
// default 16-bucket schema to keep cardinality predictable. Operators
// who need finer resolution should override via the overrides:
// escape hatch (ADR-0012).
var REDDefaultHistogramBuckets = []string{
	"10ms",
	"50ms",
	"100ms",
	"250ms",
	"500ms",
	"1s",
	"2.5s",
	"5s",
	"10s",
}

// REDDimensionDenylist is the set of attribute names rejected from
// SpanDimensions / ExtraResourceDimensions at validation time. Each
// entry would, if added as a RED dimension, multiply the connector's
// dimension-combination fan-out by ~one-per-request — an O(N)
// cardinality blow-up that would tip span_metrics into its overflow
// bucket within minutes on real traffic.
//
// Names with their reason:
//   - trace_id / span_id / request_id: per-request unique by definition
//   - user.id / customer_id / tenant_id: per-user / per-tenant unique
//   - url.full / http.url: unique per query string / fragment
//   - http.path / http.target: usually contains IDs (vs http.route which
//     is the templated form)
var REDDimensionDenylist = map[string]string{
	"trace_id":    "per-request unique; tracks every individual request — use http.route for endpoint-level grouping",
	"span_id":     "per-span unique; not meaningful as a dimension",
	"request_id":  "per-request unique",
	"user.id":     "per-user unique; cardinality scales with user count",
	"customer_id": "per-customer unique; cardinality scales with customer count",
	"tenant_id":   "per-tenant unique; cardinality scales with tenant count",
	"url.full":    "includes query string + fragment; varies per call",
	"http.url":    "deprecated alias for url.full",
	"http.path":   "usually contains IDs; use http.route for the templated form",
	"http.target": "usually contains IDs; use http.route for the templated form",
}
