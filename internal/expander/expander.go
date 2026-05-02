// Package expander turns a validated *config.AgentConfig into the upstream
// OpenTelemetry Collector YAML that the embedded collector consumes. It is
// the only place the agent's user-facing schema is translated into upstream
// concepts; cmd/preview prints the result, cmd/run feeds it to the
// collector.
//
// V0 expansion has two layers:
//
//  1. A single base template (templates/base.yaml.tmpl) that defines the
//     always-on OTLP receiver, the standard processor chain (memory_limiter,
//     resource, batch), the egress exporter selected by output.mode, and the
//     three pipelines.
//
//  2. Platform-default fragments loaded from internal/profiles when the
//     resolved profile is linux or darwin. Fragments are spliced into the
//     base template's receivers: block; per-pipeline receiver lists are
//     computed in Go (not in the template) so the rendered YAML stays clean.
//
// Profile mode resolution lives here so the expander can announce on stderr
// when it falls back from auto -> none on an unsupported GOOS.
package expander

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/conduit-obs/conduit-agent/internal/config"
	"github.com/conduit-obs/conduit-agent/internal/profiles"
)

//go:embed templates/*.yaml.tmpl
var templatesFS embed.FS

const baseTemplateName = "base.yaml.tmpl"

// templateView is the value passed to the template engine. Pipeline
// receiver / processor lists and the spliced-in receiver fragment are
// computed in Go before the template runs so the template logic stays
// linear.
type templateView struct {
	*config.AgentConfig

	// ExporterName is the pipeline-level exporter id matching whichever
	// concrete exporter the Output.Mode produced. For honeycomb mode it's
	// "otlphttp/honeycomb"; for gateway mode it's "otlp/gateway".
	ExporterName string

	// ExtraReceivers is the YAML body to splice in under "receivers:". It
	// is already indented two spaces (or empty when no profile fragments
	// apply).
	ExtraReceivers string

	// OTLPBindAddress is the host part of the OTLP receiver listen
	// addresses. Defaults to "127.0.0.1" so a stock host install does not
	// expose OTLP to the local network; the docker / k8s profiles
	// override to "0.0.0.0" so peer containers / pods can reach the
	// agent. Operators who want LAN-wide ingest on a host install set
	// profile.mode=docker explicitly (the schema is the knob; no separate
	// bind field).
	OTLPBindAddress string

	// K8sAttributes turns on the k8sattributes processor block in
	// `processors:` and inserts it into every pipeline's processor list
	// (after resourcedetection, before resource — so host identity is
	// established first, then k8s metadata layered on, then the user's
	// resource block can override either). True only for profile.mode=k8s
	// in V0 because the processor needs a Kubernetes API client and
	// matching RBAC.
	K8sAttributes bool

	// TraceReceivers / MetricReceivers / LogReceivers list the receiver IDs
	// each pipeline consumes. Always begins with "otlp"; the relevant
	// profile receivers append based on signal.
	TraceReceivers  []string
	MetricReceivers []string
	LogReceivers    []string

	// TraceProcessors / MetricProcessors / LogProcessors list the
	// processor IDs each pipeline runs. The base set is computed from
	// always-on processors; profile-specific processors (today: just
	// k8sattributes) are inserted by the expander before the template
	// runs.
	TraceProcessors  []string
	MetricProcessors []string
	LogProcessors    []string

	// TraceExporters / MetricExporters / LogExporters list the exporter
	// (and connector-as-exporter) IDs each pipeline writes to. In the
	// RED-disabled / pre-M8 baseline this is just [ExporterName,
	// "debug"]. With RED on, the traces pipeline appends "span_metrics"
	// (the connector id) so spans tee through the connector and emerge
	// as derived metrics on the metrics pipeline.
	TraceExporters  []string
	MetricExporters []string
	LogExporters    []string

	// REDEnabled reports whether the span_metrics connector should be
	// rendered into the output. When true the template emits the
	// connector block and tees the traces pipeline through it; when
	// false the connector block is omitted and the metrics pipeline
	// looks the same as a M2-era OTLP-only render. Computed once in
	// newView so the template logic stays branch-free.
	REDEnabled bool

	// REDSpanDimensions is the final span-attribute dimension list
	// rendered into the connector's "dimensions:" block — the always-on
	// REDDefaultSpanDimensions concatenated with any user-supplied
	// entries from cfg.Metrics.RED.SpanDimensions. Order is preserved
	// so operators can reason about precedence (defaults first, user
	// adds last).
	REDSpanDimensions []string

	// REDResourceDimensions is the final resource-attribute dimension
	// list rendered into the connector's
	// "resource_metrics_key_attributes:" block — REDDefaultResourceDimensions
	// concatenated with cfg.Metrics.RED.ExtraResourceDimensions.
	REDResourceDimensions []string

	// REDHistogramBuckets is the explicit histogram bucket boundary
	// list. Sourced from REDDefaultHistogramBuckets in V0; M9+ may
	// expose a knob to override per-deployment.
	REDHistogramBuckets []string

	// REDCardinalityLimit caps the connector's total tracked dimension
	// combinations (maps to upstream aggregation_cardinality_limit).
	// Sourced from cfg.Metrics.RED.CardinalityLimit (defaulted to
	// DefaultREDCardinalityLimit by config.applyDefaults).
	REDCardinalityLimit int

	// PersistentQueueEnabled (M10.A) toggles the file_storage extension
	// + the per-exporter sending_queue.storage block. When true the
	// rendered YAML wires every active exporter's sending_queue to the
	// extension's persistent backing; when false the upstream
	// in-memory sending_queue defaults apply unchanged.
	PersistentQueueEnabled bool

	// PersistentQueueDir is the on-disk directory backing the
	// file_storage extension when PersistentQueueEnabled is true.
	// Already defaulted by config.applyDefaults (V0 default:
	// /var/lib/conduit/queue).
	PersistentQueueDir string

	// RefineryEnabled (M10.B) toggles the otlp/refinery exporter block
	// in honeycomb mode. When true, the traces pipeline routes only
	// through the refinery exporter (metrics + logs continue direct
	// to Honeycomb); when false the traces pipeline uses the same
	// otlphttp/honeycomb exporter as metrics + logs.
	RefineryEnabled  bool
	RefineryEndpoint string
	RefineryInsecure bool

	// GatewayInsecure (M10.C) feeds into the gateway exporter's
	// `tls.insecure` field. The rendered YAML always emits an explicit
	// `tls: { insecure: <bool> }` block in gateway mode so the
	// TLS-required-by-default contract is visible at preview time
	// (true means the operator opted into the lab override).
	GatewayInsecure bool
}

// Expand renders the BASE upstream OTel Collector YAML for cfg — the
// always-on receivers / processors / exporters plus active platform
// fragments. It does NOT include cfg.Overrides; callers handing the
// result to the embedded collector should use ExpandConfigs instead so
// the user's overrides participate in the merge.
//
// cfg is expected to be already validated (Load/Parse handles that).
//
// Profile resolution side effects: when profile.mode is "auto" and the
// runtime GOOS has no fragment set, Expand writes a one-line warning to
// warnW and proceeds as if the user had set profile.mode=none. Pass
// io.Discard from non-interactive callers (tests) to suppress.
func Expand(cfg *config.AgentConfig) (string, error) {
	return expandTo(cfg, nil)
}

// ExpandWithWarnings is identical to Expand but lets the caller capture
// any soft warnings produced during profile resolution. Used by cmd/run
// and cmd/preview to surface those warnings to the user; tests pass
// io.Discard.
func ExpandWithWarnings(cfg *config.AgentConfig, warnW io.Writer) (string, error) {
	return expandTo(cfg, warnW)
}

// ExpandConfigs returns the slice of YAML config sources the embedded
// OTel Collector should resolve in order. For configs without
// cfg.Overrides set the slice has one element (the base render). With
// overrides, the second element is the user's overrides block, which the
// collector deep-merges over the first per its standard multi-config
// resolver semantics: maps merge by key (overrides win where both set
// the same key), lists replace wholesale.
//
// See docs/adr/adr-0012.md for the design rationale and review cadence
// for the overrides escape hatch.
func ExpandConfigs(cfg *config.AgentConfig) ([]string, error) {
	return ExpandConfigsWithWarnings(cfg, nil)
}

// ExpandConfigsWithWarnings is the warn-writer variant of ExpandConfigs.
// Used by cmd/run and cmd/preview so profile-resolution warnings reach
// stderr while the rendered configs go through the regular result path.
func ExpandConfigsWithWarnings(cfg *config.AgentConfig, warnW io.Writer) ([]string, error) {
	base, err := expandTo(cfg, warnW)
	if err != nil {
		return nil, err
	}
	if cfg == nil || len(cfg.Overrides) == 0 {
		return []string{base}, nil
	}
	overridesYAML, err := marshalOverrides(cfg.Overrides)
	if err != nil {
		return nil, fmt.Errorf("expand: marshal overrides: %w", err)
	}
	return []string{base, overridesYAML}, nil
}

// marshalOverrides serializes the user's overrides map to YAML with a
// stable, two-space indent that matches the rest of the rendered config.
// We use yaml.v3 (already a go.mod dep via internal/config) rather than
// the encoding/json -> any -> yaml.Marshal route because yaml.v3
// preserves list ordering — critical for service.pipelines.<signal>.processors
// where order is semantic.
func marshalOverrides(overrides map[string]any) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(overrides); err != nil {
		_ = enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func expandTo(cfg *config.AgentConfig, warnW io.Writer) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("expand: nil AgentConfig")
	}

	view, err := newView(cfg, warnW)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New(baseTemplateName).Funcs(funcs()).ParseFS(templatesFS, "templates/"+baseTemplateName)
	if err != nil {
		return "", fmt.Errorf("expand: parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, baseTemplateName, view); err != nil {
		return "", fmt.Errorf("expand: execute template: %w", err)
	}
	return buf.String(), nil
}

func newView(cfg *config.AgentConfig, warnW io.Writer) (*templateView, error) {
	v := &templateView{
		AgentConfig:     cfg,
		OTLPBindAddress: resolveOTLPBindAddress(cfg.Profile),
		K8sAttributes:   profileWantsK8sAttributes(cfg.Profile),
		TraceReceivers:  []string{"otlp"},
		MetricReceivers: []string{"otlp"},
		LogReceivers:    []string{"otlp"},
	}
	switch cfg.Output.Mode {
	case config.OutputModeHoneycomb:
		v.ExporterName = "otlphttp/honeycomb"
		// M10.B: route traces through Refinery if configured. Metrics
		// and logs continue through the direct Honeycomb exporter; the
		// per-pipeline exporter list overrides happen below after
		// applyREDView so RED's connector tee composes correctly.
		if hc := cfg.Output.Honeycomb; hc != nil && hc.Traces != nil && hc.Traces.ViaRefinery != nil {
			v.RefineryEnabled = true
			v.RefineryEndpoint = hc.Traces.ViaRefinery.Endpoint
			v.RefineryInsecure = hc.Traces.ViaRefinery.Insecure
		}
	case config.OutputModeOTLP:
		v.ExporterName = "otlphttp/otlp"
	case config.OutputModeGateway:
		v.ExporterName = "otlp/gateway"
		if cfg.Output.Gateway != nil {
			v.GatewayInsecure = cfg.Output.Gateway.Insecure
		}
	default:
		return nil, fmt.Errorf("expand: unsupported output.mode %q (validation should have caught this)", cfg.Output.Mode)
	}

	// M10.A: persistent queue toggle. The expander only checks
	// Enabled; Dir is already defaulted in applyDefaults so the
	// template reads a known-good value.
	if pq := cfg.Output.PersistentQueue; pq != nil && pq.Enabled {
		v.PersistentQueueEnabled = true
		v.PersistentQueueDir = pq.Dir
	}

	platform := resolvePlatform(cfg.Profile, warnW)
	if platform != "" {
		fragment, ids, err := loadProfileFragments(platform, cfg.Profile)
		if err != nil {
			return nil, err
		}
		v.ExtraReceivers = fragment
		v.MetricReceivers = append(v.MetricReceivers, ids.metrics...)
		v.LogReceivers = append(v.LogReceivers, ids.logs...)
	}

	v.TraceProcessors = pipelineProcessorIDs(signalTraces, v.K8sAttributes)
	v.MetricProcessors = pipelineProcessorIDs(signalMetrics, v.K8sAttributes)
	v.LogProcessors = pipelineProcessorIDs(signalLogs, v.K8sAttributes)

	// Default exporter lists — RED, when enabled, appends the
	// span_metrics connector id to the traces pipeline below.
	//
	// M10.B: when Refinery routing is configured, the traces pipeline
	// swaps the direct Honeycomb exporter for otlp/refinery. Metrics
	// and logs keep going to Honeycomb directly — Refinery is a
	// trace-tier sampler, not a fanout gateway.
	tracesEgress := v.ExporterName
	if v.RefineryEnabled {
		tracesEgress = "otlp/refinery"
	}
	v.TraceExporters = []string{tracesEgress, "debug"}
	v.MetricExporters = []string{v.ExporterName, "debug"}
	v.LogExporters = []string{v.ExporterName, "debug"}

	applyREDView(v, cfg)

	return v, nil
}

// applyREDView populates the RED-related templateView fields and tees
// the connector into the traces / metrics pipelines. Called from
// newView after the per-pipeline processor lists are computed so the
// connector wire-up sees the final receiver / exporter sets.
//
// The connector ID rendered into the YAML is "span_metrics" (the
// upstream snake_case name introduced in v0.119 and stable since;
// v0.151 still accepts the legacy "spanmetrics" alias but warns at
// startup, and we don't want to ship a startup warning).
//
// The function tolerates Metrics / Metrics.RED being nil so tests can
// hand-build an AgentConfig without going through Load/Parse and still
// get the RED-on default; the only knob a nil RED block is missing is
// the cardinality limit, which we substitute with the documented
// default.
func applyREDView(v *templateView, cfg *config.AgentConfig) {
	var red *config.REDConfig
	if cfg.Metrics != nil {
		red = cfg.Metrics.RED
	}
	v.REDEnabled = red.REDEnabled()
	if !v.REDEnabled {
		return
	}
	var (
		userSpanDims, userResDims []string
		cardLimit                 = config.DefaultREDCardinalityLimit
	)
	if red != nil {
		userSpanDims = red.SpanDimensions
		userResDims = red.ExtraResourceDimensions
		if red.CardinalityLimit > 0 {
			cardLimit = red.CardinalityLimit
		}
	}
	v.REDSpanDimensions = append(append([]string{}, config.REDDefaultSpanDimensions...), userSpanDims...)
	v.REDResourceDimensions = append(append([]string{}, config.REDDefaultResourceDimensions...), userResDims...)
	v.REDHistogramBuckets = config.REDDefaultHistogramBuckets
	v.REDCardinalityLimit = cardLimit

	const connectorID = "span_metrics"
	// Traces pipeline tees through the connector by listing it in its
	// exporters. The data still flows to the real egress exporter
	// alongside; the connector is a synthetic exporter that emits
	// metrics on the receiver-side of the metrics pipeline.
	v.TraceExporters = append(v.TraceExporters, connectorID)
	v.MetricReceivers = append(v.MetricReceivers, connectorID)
}

// pipelineSignal is an internal enum used to compute per-pipeline
// processor lists. It exists so the always-on processor sequence and the
// profile-specific insertion points are expressed in one place.
type pipelineSignal int

const (
	signalTraces pipelineSignal = iota
	signalMetrics
	signalLogs
)

// pipelineProcessorIDs returns the ordered processor ID list for the
// given pipeline, splicing in profile-specific processors at fixed
// positions:
//
//   - memory_limiter is always first (drop on overload before doing
//     anything else with the data).
//   - resourcedetection runs second so host identity is on every
//     subsequent processor's input.
//   - k8sattributes (when enabled by profile.mode=k8s) runs after
//     resourcedetection so host attrs land first; the user's resource:
//     block can still override either by listing the same keys in
//     conduit.yaml.
//   - resource runs after k8sattributes so explicit conduit.yaml values
//     (service.name, deployment.environment) win over auto-detected
//     metadata.
//   - transform/logs is logs-only and runs after resource so it sees
//     the canonical resource shape.
//   - batch is always last.
func pipelineProcessorIDs(s pipelineSignal, k8sAttrs bool) []string {
	out := []string{"memory_limiter", "resourcedetection"}
	if k8sAttrs {
		out = append(out, "k8sattributes")
	}
	out = append(out, "resource")
	if s == signalLogs {
		out = append(out, "transform/logs")
	}
	out = append(out, "batch")
	return out
}

// profileWantsK8sAttributes reports whether the resolved profile should
// pull in the k8sattributes processor. V0 ties this exclusively to
// profile.mode=k8s; the processor needs RBAC the chart only grants in
// that mode.
func profileWantsK8sAttributes(p *config.Profile) bool {
	return p != nil && p.Mode == config.ProfileModeK8s
}

// resolveOTLPBindAddress picks the host portion of the OTLP receiver
// listen addresses. Container-native profiles (docker, k8s) need 0.0.0.0
// so peer containers / pods can reach the agent; every host-mode profile
// stays on 127.0.0.1 so a stock install does not silently expose OTLP to
// the local network. See the templateView.OTLPBindAddress comment for the
// schema-level rationale.
func resolveOTLPBindAddress(p *config.Profile) string {
	if p == nil {
		return "127.0.0.1"
	}
	switch p.Mode {
	case config.ProfileModeDocker, config.ProfileModeK8s:
		return "0.0.0.0"
	default:
		return "127.0.0.1"
	}
}

// resolvePlatform turns a *config.Profile into the platform name to load
// fragments for, or "" when no profile applies. Defaults to none.
//
// docker (M9.A) ships a host-metrics fragment that mirrors the linux
// shape but expects /proc and /sys to be bind-mounted to /hostfs by
// the operator's compose file (the recipe is in
// deploy/docker/README.md and deploy/docker/compose-linux-host.yaml).
// The `root_path: /hostfs` re-roots every scraper there so the
// rendered fragment is identical across docker / k8s. profile.mode=
// docker still flips OTLP receivers to 0.0.0.0 for peer-container
// reachability (handled by resolveOTLPBindAddress).
//
// k8s loads three fragments (hostmetrics + kubelet + logs) — the Helm
// chart in deploy/helm/conduit-agent provides the matching DaemonSet
// host mounts and ClusterRole RBAC in M5.C.
//
// windows (M6.A) loads hostmetrics + Windows Event Log (Application +
// System channels). The agent runs as a Windows Service, so OTLP stays
// on 127.0.0.1 by default — peer apps on the same host reach the
// agent through the loopback, and ingress from other hosts is the
// operator's deliberate firewall + bind-address override.
func resolvePlatform(p *config.Profile, warnW io.Writer) string {
	if p == nil {
		return ""
	}
	switch p.Mode {
	case config.ProfileModeNone:
		return ""
	case config.ProfileModeLinux, config.ProfileModeDarwin, config.ProfileModeDocker, config.ProfileModeK8s, config.ProfileModeWindows:
		return string(p.Mode)
	case config.ProfileModeAuto, "":
		detected := profiles.DetectPlatform()
		if detected == "" && warnW != nil {
			fmt.Fprintf(warnW,
				"conduit: profile.mode=auto on %s but Conduit ships no profile for this OS; falling back to OTLP-only. Set profile.mode=none to silence.\n",
				runtime.GOOS)
		}
		return detected
	default:
		// Validation would have caught this; treat as none defensively.
		if warnW != nil {
			fmt.Fprintf(warnW, "conduit: unknown profile.mode %q; falling back to OTLP-only\n", string(p.Mode))
		}
		return ""
	}
}

// pipelineReceiverIDs holds the per-pipeline receiver IDs contributed by a
// profile (atop the always-present otlp).
type pipelineReceiverIDs struct {
	metrics []string
	logs    []string
}

// loadProfileFragments concatenates the YAML fragments selected by profile
// into a single block (already indented two spaces) and returns the
// receiver IDs each pipeline should consume. Fragment loading is governed
// by the per-feature toggles on cfg.Profile.
//
// Platforms that ship a kubelet.yaml fragment (today only k8s) get the
// kubelet receiver added to the metrics pipeline whenever host_metrics is
// enabled; the two are bundled because there is no useful Kubernetes
// metrics story without both per-node host stats and per-pod kubelet
// stats. Operators who want only one half should use overrides: in their
// conduit.yaml.
func loadProfileFragments(platform string, p *config.Profile) (string, pipelineReceiverIDs, error) {
	var (
		buf bytes.Buffer
		ids pipelineReceiverIDs
	)

	// Both the host-metrics and system-logs fragments are gated by the
	// same has-fragment-AND-feature-enabled rule: the user's toggle
	// expresses intent ("I want host metrics on"), the registry check
	// expresses capability ("Conduit ships a fragment for this
	// platform/signal pair"). Docker is the first platform that
	// ships hostmetrics but not logs (V0 docker logs are still OTLP-
	// only — peer apps push container logs to the agent, no on-host
	// filelog scrape), so the absence path has to be silent rather
	// than an error.
	if p.HostMetricsEnabled() && profiles.Has(platform, profiles.SignalHostMetrics) {
		body, err := profiles.Load(platform, profiles.SignalHostMetrics)
		if err != nil {
			return "", ids, fmt.Errorf("expand: load %s hostmetrics fragment: %w", platform, err)
		}
		writeIndentedFragment(&buf, body)
		ids.metrics = append(ids.metrics, "hostmetrics")

		// Platforms that ship kubelet.yaml (today only k8s) layer it on
		// top of the host scrapers — see the function-level comment for
		// why bundling is the right V0 default.
		if profiles.Has(platform, profiles.SignalKubelet) {
			body, err := profiles.Load(platform, profiles.SignalKubelet)
			if err != nil {
				return "", ids, fmt.Errorf("expand: load %s kubelet fragment: %w", platform, err)
			}
			writeIndentedFragment(&buf, body)
			ids.metrics = append(ids.metrics, "kubeletstats")
		}
	}

	if p.SystemLogsEnabled() && profiles.Has(platform, profiles.SignalSystemLogs) {
		body, err := profiles.Load(platform, profiles.SignalSystemLogs)
		if err != nil {
			return "", ids, fmt.Errorf("expand: load %s logs fragment: %w", platform, err)
		}
		writeIndentedFragment(&buf, body)
		// Receiver IDs come from the fragment body itself: any line that
		// starts at column 0 (no indent) and ends with a colon is a
		// top-level receiver. This keeps the loader from having to
		// duplicate knowledge that already lives in the YAML.
		ids.logs = append(ids.logs, extractTopLevelReceivers(body)...)
	}

	return buf.String(), ids, nil
}

// writeIndentedFragment appends body to w with every line prefixed by two
// spaces, plus a trailing newline so the next fragment starts cleanly. We
// strip leading comment-only lines and the trailing newline first to keep
// the rendered YAML tidy.
func writeIndentedFragment(w *bytes.Buffer, body string) {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return
	}
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			w.WriteByte('\n')
		} else {
			w.WriteString("  ")
			w.WriteString(line)
			w.WriteByte('\n')
		}
	}
}

// extractTopLevelReceivers returns the IDs of receivers declared at column
// zero in body (i.e. the keys directly under what becomes "receivers:").
// "filelog/system:" -> "filelog/system". Comment lines (#) and blank lines
// are skipped.
func extractTopLevelReceivers(body string) []string {
	var ids []string
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		if !strings.HasSuffix(line, ":") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(line, ":"))
	}
	return ids
}

func funcs() template.FuncMap {
	return template.FuncMap{
		// q double-quotes a string as a YAML scalar by piggybacking on
		// JSON's string-encoding rules. Safest way to embed user-supplied
		// values that may contain quotes, backslashes, or ${env:...} refs.
		"q": func(s string) (string, error) {
			b, err := json.Marshal(s)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
		// join concatenates a string slice with a separator — used to
		// render pipeline processor and receiver inline lists like
		// "[memory_limiter, k8sattributes, batch]" without dragging in
		// the full strings package every place the template wants one.
		"join": strings.Join,
	}
}
