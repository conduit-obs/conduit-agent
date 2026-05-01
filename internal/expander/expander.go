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
// receiver lists and the spliced-in receiver fragment are computed in Go
// before the template runs so the template logic stays linear.
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
	// expose OTLP to the local network; the docker profile overrides to
	// "0.0.0.0" so peer containers in the same compose / pod / network
	// can reach the agent. Operators who want LAN-wide ingest on a host
	// install set profile.mode=docker explicitly (the schema is the knob;
	// no separate bind field).
	OTLPBindAddress string

	// TraceReceivers / MetricReceivers / LogReceivers list the receiver IDs
	// each pipeline consumes. Always begins with "otlp"; the relevant
	// profile receivers append based on signal.
	TraceReceivers  []string
	MetricReceivers []string
	LogReceivers    []string
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

	return v, nil
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
// Docker and k8s are intentionally fragment-less in V0/M5.A: scraping host
// metrics from inside a container needs /proc and /sys bind mounts that
// the user / chart must opt into, and the k8s-flavoured kubelet / filelog /
// k8sattributes receivers land in M5.B once the matching RBAC and DaemonSet
// host-mounts arrive. Both profiles only change OTLP bind behavior (handled
// by resolveOTLPBindAddress) for now. Operators who want host metrics from
// a containerized agent today set profile.mode=linux on a container with
// the bind mounts in place; that path is documented in
// deploy/docker/README.md and (when M5.B lands) deploy/helm/.
func resolvePlatform(p *config.Profile, warnW io.Writer) string {
	if p == nil {
		return ""
	}
	switch p.Mode {
	case config.ProfileModeNone, config.ProfileModeDocker, config.ProfileModeK8s:
		return ""
	case config.ProfileModeLinux, config.ProfileModeDarwin:
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
	}
}
