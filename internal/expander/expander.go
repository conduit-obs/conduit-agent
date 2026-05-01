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
}

// Expand renders cfg into a single upstream OTel Collector YAML document.
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
	case config.OutputModeGateway:
		v.ExporterName = "otlp/gateway"
	default:
		return nil, fmt.Errorf("expand: unsupported output.mode %q (validation should have caught this)", cfg.Output.Mode)
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

	return v, nil
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
// Docker is intentionally fragment-less in V0: scraping host metrics from
// inside a container needs /proc and /sys bind mounts that the user must
// opt into at run time, so the docker profile only changes OTLP bind
// behavior (handled by resolveOTLPBindAddress) and leaves receiver
// fragments empty. Operators who want host metrics from a containerized
// agent set profile.mode=linux on a container with the bind mounts in
// place; that path is documented in deploy/docker/README.md.
//
// k8s loads three fragments (hostmetrics + kubelet + logs) — the Helm
// chart in deploy/helm/conduit-agent provides the matching DaemonSet
// host mounts and ClusterRole RBAC in M5.C.
func resolvePlatform(p *config.Profile, warnW io.Writer) string {
	if p == nil {
		return ""
	}
	switch p.Mode {
	case config.ProfileModeNone, config.ProfileModeDocker:
		return ""
	case config.ProfileModeLinux, config.ProfileModeDarwin, config.ProfileModeK8s:
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

	if p.HostMetricsEnabled() {
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

	if p.SystemLogsEnabled() {
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
