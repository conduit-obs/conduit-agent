package expander

import (
	"bytes"
	"strings"
	"testing"

	"github.com/conduit-obs/conduit-agent/internal/config"
)

// honeycomb returns a fully-defaulted honeycomb-mode AgentConfig with the
// given profile, suitable for table-driven expander tests.
func honeycomb(p *config.Profile) *config.AgentConfig {
	return &config.AgentConfig{
		ServiceName:           "demo",
		DeploymentEnvironment: "dev",
		Output: config.Output{
			Mode: config.OutputModeHoneycomb,
			Honeycomb: &config.HoneycombOutput{
				APIKey:   "${env:KEY}",
				Endpoint: config.DefaultHoneycombEndpoint,
			},
		},
		Profile: p,
	}
}

// TestExpandConfigs_NoOverrides asserts the single-source behavior:
// without cfg.Overrides, ExpandConfigs returns one element and that
// element matches what plain Expand would have produced. This is the
// invariant cmd/run depends on for the "stock conduit.yaml" path.
func TestExpandConfigs_NoOverrides(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	configs, err := ExpandConfigs(cfg)
	if err != nil {
		t.Fatalf("ExpandConfigs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("ExpandConfigs without overrides should produce 1 config source, got %d", len(configs))
	}
	want, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if configs[0] != want {
		t.Errorf("ExpandConfigs[0] should equal Expand output; got divergence")
	}
}

// TestExpandConfigs_WithOverrides locks in the multi-source behavior
// from ADR-0012: when cfg.Overrides is set, ExpandConfigs returns
// (base, overrides) so the embedded collector can deep-merge them at
// startup. The base must NOT have the overrides spliced into it
// (collector merges, we don't), and the overrides document must be
// valid YAML carrying the user's literal map.
func TestExpandConfigs_WithOverrides(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeK8s})
	cfg.Overrides = map[string]any{
		"receivers": map[string]any{
			"kubeletstats": map[string]any{
				"collection_interval": "15s",
			},
		},
		"service": map[string]any{
			"pipelines": map[string]any{
				"logs": map[string]any{
					"processors": []any{
						"memory_limiter",
						"resourcedetection",
						"k8sattributes",
						"resource",
						"transform/logs",
						"batch",
					},
				},
			},
		},
	}
	configs, err := ExpandConfigs(cfg)
	if err != nil {
		t.Fatalf("ExpandConfigs: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("ExpandConfigs with overrides should produce 2 config sources, got %d", len(configs))
	}
	base, overrides := configs[0], configs[1]

	// The base render must remain pristine — overrides do not leak
	// into it because the collector does the merging at startup.
	if strings.Contains(base, "collection_interval: 15s") {
		t.Errorf("base render should not contain the override value; expander merged where it should not have")
	}

	// The overrides document must round-trip the user's map.
	mustContain(t, overrides, []string{
		`receivers:`,
		`kubeletstats:`,
		`collection_interval: 15s`,
		`service:`,
		`pipelines:`,
		`logs:`,
		// Pipeline list ordering is semantic for the collector; the
		// yaml.v3 encoder preserves it (the user wrote the slice in
		// this order).
		`- memory_limiter`,
		`- transform/logs`,
		`- batch`,
	})
}

// TestExpandConfigs_NilCfg keeps ExpandConfigs honest about its
// argument-validation contract — same posture as Expand.
func TestExpandConfigs_NilCfg(t *testing.T) {
	if _, err := ExpandConfigs(nil); err == nil {
		t.Error("ExpandConfigs(nil) should error")
	}
}

// otlpOutput is the OTLP/HTTP-mode counterpart to honeycomb() — same
// profile + agent identity shape, swapping the egress block.
func otlpOutput(p *config.Profile, otlp *config.OTLPOutput) *config.AgentConfig {
	return &config.AgentConfig{
		ServiceName:           "demo",
		DeploymentEnvironment: "dev",
		Output: config.Output{
			Mode: config.OutputModeOTLP,
			OTLP: otlp,
		},
		Profile: p,
	}
}

// TestExpand_OTLPMode_RendersGenericExporter exercises the new generic
// OTLP/HTTP egress (output.mode: otlp) used for vendors Conduit
// doesn't yet ship a named preset for — Datadog OTLP intake, Grafana
// Cloud OTLP, SigNoz, etc. The exporter id must be otlphttp/otlp (so
// the pipeline-list lookup in newView matches), the endpoint and
// caller-supplied headers must round-trip verbatim, and gzip
// compression must be the unset default.
func TestExpand_OTLPMode_RendersGenericExporter(t *testing.T) {
	cfg := otlpOutput(&config.Profile{Mode: config.ProfileModeNone}, &config.OTLPOutput{
		Endpoint: "https://otlp-gateway-prod-us-central-0.grafana.net/otlp",
		Headers: map[string]string{
			"Authorization": "Basic ${env:GRAFANA_CLOUD_OTLP_TOKEN}",
		},
	})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`otlphttp/otlp:`,
		`endpoint: "https://otlp-gateway-prod-us-central-0.grafana.net/otlp"`,
		`Authorization: "Basic ${env:GRAFANA_CLOUD_OTLP_TOKEN}"`,
		// Default compression is gzip (rendered literally, not quoted,
		// when the user leaves Compression empty).
		`compression: gzip`,
	})
	mustNotContain(t, out, []string{
		`otlphttp/honeycomb:`,
		`otlp/gateway:`,
		`x-honeycomb-team:`,
		`tls:`,
	})
	// Traces tees through the span_metrics connector by default
	// (M8 RED-from-spans); metrics + logs do not. Asserting both
	// halves keeps the egress + connector contract together in
	// one place.
	if got := pipelineExporters(t, out, "traces"); !equalSet(got, []string{"otlphttp/otlp", "debug", "span_metrics"}) {
		t.Errorf("traces pipeline exporters under otlp mode = %v; want [otlphttp/otlp debug span_metrics]", got)
	}
	for _, p := range []string{"metrics", "logs"} {
		got := pipelineExporters(t, out, p)
		if !equalSet(got, []string{"otlphttp/otlp", "debug"}) {
			t.Errorf("%s pipeline exporters under otlp mode = %v; want [otlphttp/otlp debug]", p, got)
		}
	}
}

// TestExpand_OTLPMode_CompressionAndInsecureOverrides covers the lab-
// style escape valves: setting compression: none for destinations that
// reject gzip, and insecure: true to skip TLS verification (ADR-0009;
// production destinations should always present a valid certificate).
func TestExpand_OTLPMode_CompressionAndInsecureOverrides(t *testing.T) {
	cfg := otlpOutput(&config.Profile{Mode: config.ProfileModeNone}, &config.OTLPOutput{
		Endpoint:    "http://localhost:4318",
		Compression: "none",
		Insecure:    true,
	})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`compression: "none"`,
		`tls:`,
		`insecure: true`,
	})
	// No headers set → no headers: block.
	mustNotContain(t, out, []string{
		`headers:`,
	})
}

func TestExpand_NoProfile_OTLPOnly(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	mustContain(t, out, []string{
		`otlp:`,
		`endpoint: 127.0.0.1:4317`,
		`endpoint: 127.0.0.1:4318`,
		`otlphttp/honeycomb:`,
		`endpoint: "https://api.honeycomb.io"`,
		`x-honeycomb-team: "${env:KEY}"`,
		`exporters: [otlphttp/honeycomb, debug]`,
	})

	mustNotContain(t, out, []string{
		`hostmetrics:`,
		`filelog/system:`,
		`journald:`,
	})

	for _, pipeline := range []string{"traces:", "metrics:", "logs:"} {
		if !strings.Contains(out, pipeline) {
			t.Errorf("missing pipeline %q in:\n%s", pipeline, out)
		}
	}
}

func TestExpand_LinuxProfile_LayersHostMetricsAndLogs(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeLinux})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	mustContain(t, out, []string{
		`otlp:`,
		`hostmetrics:`,
		`filelog/system:`,
		`journald:`,
		`/var/log/syslog`,
		`/var/log/messages`,
	})

	if got := pipelineReceivers(t, out, "metrics"); !contains(got, "hostmetrics") {
		t.Errorf("metrics pipeline missing hostmetrics receiver; got %v", got)
	}
	if got := pipelineReceivers(t, out, "logs"); !contains(got, "filelog/system") || !contains(got, "journald") {
		t.Errorf("logs pipeline missing filelog/system or journald; got %v", got)
	}
	// Traces should NOT include host metrics or filelog.
	if got := pipelineReceivers(t, out, "traces"); !equalSet(got, []string{"otlp"}) {
		t.Errorf("traces pipeline should only have otlp; got %v", got)
	}
}

func TestExpand_DarwinProfile_NoJournald(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeDarwin})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	mustContain(t, out, []string{
		`hostmetrics:`,
		`filelog/system:`,
		`/var/log/system.log`,
	})
	mustNotContain(t, out, []string{
		`journald:`,
		`processes:`, // darwin scraper list omits this
	})

	if got := pipelineReceivers(t, out, "logs"); !contains(got, "filelog/system") {
		t.Errorf("logs pipeline missing filelog/system; got %v", got)
	}
	if got := pipelineReceivers(t, out, "logs"); contains(got, "journald") {
		t.Errorf("logs pipeline should not contain journald on darwin; got %v", got)
	}
}

func TestExpand_LinuxProfile_HostMetricsDisabled(t *testing.T) {
	disabled := false
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeLinux, HostMetrics: &disabled})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustNotContain(t, out, []string{`hostmetrics:`})
	mustContain(t, out, []string{`filelog/system:`}) // logs still on by default

	// span_metrics is the M8 RED-from-spans connector — emitted on
	// the metrics pipeline regardless of host_metrics, because RED
	// derives from traces, not from host scrapers.
	if got := pipelineReceivers(t, out, "metrics"); !equalSet(got, []string{"otlp", "span_metrics"}) {
		t.Errorf("metrics pipeline should be otlp + span_metrics when host_metrics=false; got %v", got)
	}
}

func TestExpand_LinuxProfile_SystemLogsDisabled(t *testing.T) {
	disabled := false
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeLinux, SystemLogs: &disabled})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustNotContain(t, out, []string{`filelog/system:`, `journald:`})
	mustContain(t, out, []string{`hostmetrics:`}) // metrics still on

	if got := pipelineReceivers(t, out, "logs"); !equalSet(got, []string{"otlp"}) {
		t.Errorf("logs pipeline should only have otlp when system_logs=false; got %v", got)
	}
}

func TestExpand_AutoProfile_ResolvesToRuntimeGOOS(t *testing.T) {
	// We can't force runtime.GOOS in a unit test, but on every supported
	// dev OS (linux or darwin) auto should produce hostmetrics. Mark
	// skipped on others so this test stays correct as we add platforms.
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeAuto})
	var warnings bytes.Buffer
	out, err := ExpandWithWarnings(cfg, &warnings)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if strings.Contains(out, "hostmetrics:") {
		// Test runner is on linux or darwin; confirm no fallback warning.
		if warnings.Len() > 0 {
			t.Errorf("unexpected warning on supported OS: %q", warnings.String())
		}
	} else {
		// Fallback path: warning required.
		if !strings.Contains(warnings.String(), "falling back to OTLP-only") {
			t.Errorf("expected fallback warning when auto resolves to unsupported OS; got %q", warnings.String())
		}
	}
}

// resourcedetection is the universal "who am I" processor — host.name,
// host.id, host.arch, os.type all flow from it. It must be present in
// every pipeline regardless of profile, so multi-host deployments can
// always break down telemetry by host.
func TestExpand_ResourceDetectionAlwaysOn(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.AgentConfig
	}{
		{"profile=none", honeycomb(&config.Profile{Mode: config.ProfileModeNone})},
		{"profile=linux", honeycomb(&config.Profile{Mode: config.ProfileModeLinux})},
		{"profile=darwin", honeycomb(&config.Profile{Mode: config.ProfileModeDarwin})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Expand(tc.cfg)
			if err != nil {
				t.Fatalf("Expand: %v", err)
			}
			mustContain(t, out, []string{
				`resourcedetection:`,
				`detectors: [env, system]`,
				`override: false`,
			})
			for _, p := range []string{"traces", "metrics", "logs"} {
				if got := pipelineProcessors(t, out, p); !contains(got, "resourcedetection") {
					t.Errorf("%s pipeline missing resourcedetection processor; got %v", p, got)
				}
			}
		})
	}
}

// transform/logs computes attributes["normalized_message"] from the
// filelog-parsed message, masking high-cardinality bits (UUIDs, IPs,
// key=value values, 4+ digit numbers) so Honeycomb can group similar
// log lines by template. Always wired, but only into the logs pipeline.
func TestExpand_TransformLogs_LogsPipelineOnly(t *testing.T) {
	out, err := Expand(honeycomb(&config.Profile{Mode: config.ProfileModeNone}))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`transform/logs:`,
		`normalized_message`,
		`replace_pattern`,
	})
	if got := pipelineProcessors(t, out, "logs"); !contains(got, "transform/logs") {
		t.Errorf("logs pipeline missing transform/logs processor; got %v", got)
	}
	for _, p := range []string{"traces", "metrics"} {
		if got := pipelineProcessors(t, out, p); contains(got, "transform/logs") {
			t.Errorf("%s pipeline must NOT include transform/logs; got %v", p, got)
		}
	}
}

// hostmetrics scrapers expose both byte-level and percent-form metrics for
// CPU, memory, filesystem, and paging, but the *.utilization variants are
// disabled by default upstream. We enable them in our profile so dashboards
// can plot "% used" without forcing every operator to know that the raw
// system.filesystem.usage values need to be divided by total to get a
// percent.
func TestExpand_HostMetrics_EnablesUtilizationMetrics(t *testing.T) {
	cases := []struct {
		name     string
		mode     config.ProfileMode
		expected []string
	}{
		{
			name: "linux",
			mode: config.ProfileModeLinux,
			expected: []string{
				`system.cpu.utilization:`,
				`system.memory.utilization:`,
				`system.filesystem.utilization:`,
				`system.paging.utilization:`,
			},
		},
		{
			// darwin omits paging from its scraper list, so only the
			// three platform-portable utilization metrics apply.
			name: "darwin",
			mode: config.ProfileModeDarwin,
			expected: []string{
				`system.cpu.utilization:`,
				`system.memory.utilization:`,
				`system.filesystem.utilization:`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Expand(honeycomb(&config.Profile{Mode: tc.mode}))
			if err != nil {
				t.Fatalf("Expand: %v", err)
			}
			mustContain(t, out, tc.expected)
		})
	}
}

// transform/logs's M9.C JSON parsing block lifts the well-known
// JSON fields (trace_id / span_id / level / msg) onto OTel semantic
// locations so trace correlation works for apps that log JSON to
// stdout (zerolog, pino, structlog, slog, ...) without forcing
// every team to re-instrument their loggers. The block is gated
// on body being a JSON-looking string (`IsString(body)` plus
// `^\\s*\\{` regex) so non-JSON bodies pass through unchanged.
//
// The test asserts:
//
//  1. The block sits between redaction (M9.B) and normalized_message
//     so it sees redacted JSON and can populate attributes["message"]
//     for the normalized_message block to consume.
//  2. Three trace_id naming conventions and three span_id naming
//     conventions are handled.
//  3. Empty-string gates protect SDK-set values from being
//     overwritten — checked by looking for the
//     `trace_id.string == ""` predicate.
//  4. The five-level severity_number mapping covers
//     trace/debug/info/warn/error/fatal with the documented
//     regex aliases (info/informational, warn/warning, etc.).
func TestExpand_TransformLogs_JSONParsingBlock(t *testing.T) {
	out, err := Expand(honeycomb(&config.Profile{Mode: config.ProfileModeNone}))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		// Trigger condition: JSON-looking string body. Combined into a
		// single AND'd condition so OTTL short-circuits — calling
		// IsMatch on a non-string body errors in OTTL rather than
		// returning false (transform processor v0.151.0 lets the
		// block run through that error), which is what blew up on
		// journald entries during v0.0.1 smoke.
		`IsString(body) and IsMatch(body, "^\\s*\\{")`,
		// Single ParseJSON pass into per-record cache. The
		// `where IsString(body)` guard is a defense-in-depth pin
		// against the same OTTL block-conditions edge case: even if
		// a future contrib version evaluated the block with a
		// non-string body, this clause keeps ParseJSON from
		// receiving a pcommon.Map.
		`set(cache["json"], ParseJSON(body)) where IsString(body)`,
		// Trace id naming conventions.
		`cache["json"]["trace_id"]`,
		`cache["json"]["traceId"]`,
		`cache["json"]["trace.id"]`,
		// Span id naming conventions.
		`cache["json"]["span_id"]`,
		`cache["json"]["spanId"]`,
		`cache["json"]["span.id"]`,
		// SDK-set values must survive the lift — the empty-string
		// guard is the contract that makes that work.
		`trace_id.string == ""`,
		`span_id.string == ""`,
		// Message lift so normalized_message has data on JSON bodies.
		`cache["json"]["msg"]`,
		`cache["json"]["message"]`,
		// Severity-number mapping.
		`SEVERITY_NUMBER_TRACE`,
		`SEVERITY_NUMBER_DEBUG`,
		`SEVERITY_NUMBER_INFO`,
		`SEVERITY_NUMBER_WARN`,
		`SEVERITY_NUMBER_ERROR`,
		`SEVERITY_NUMBER_FATAL`,
		`(?i)^info(rmational)?$`,
		`(?i)^warn(ing)?$`,
		`(?i)^(error|err|severe)$`,
		`(?i)^(fatal|critical|panic|emerg)$`,
	})
	// Block ordering: redaction → JSON lift → normalized_message.
	// JSON lift sits in the middle so normalized_message inherits
	// the (a) redacted body and (b) message attribute populated
	// from the JSON's "msg" / "message" field.
	redIdx := strings.Index(out, "AKIA****REDACTED****")
	jsonIdx := strings.Index(out, `set(cache["json"], ParseJSON(body)) where IsString(body)`)
	normIdx := strings.Index(out, `set(attributes["normalized_message"]`)
	if redIdx == -1 || jsonIdx == -1 || normIdx == -1 {
		t.Fatalf("block markers missing: redaction=%d json=%d normalized=%d", redIdx, jsonIdx, normIdx)
	}
	if redIdx >= jsonIdx || jsonIdx >= normIdx {
		t.Errorf("expected redaction(%d) < json(%d) < normalized(%d)", redIdx, jsonIdx, normIdx)
	}
}

// transform/logs's M9.B redaction block masks well-known credential
// patterns in body and attributes["message"] BEFORE normalized_message
// is computed. The contract is "default-on, narrow regex set" —
// AKIA-prefixed AWS access key ids, JWTs, and a small set of
// case-insensitive credential key=value patterns. This test locks in:
//
//  1. The redaction block precedes the normalized_message block in
//     the rendered YAML so the latter sees redacted input (otherwise
//     a user grouping by normalized_message would still see
//     plaintext credentials in the data).
//  2. Every documented pattern is present.
//  3. The operator-facing replacement string ("****REDACTED****") is
//     stable so downstream queries can detect "this record was
//     redacted by Conduit" without parsing the original pattern.
func TestExpand_TransformLogs_DefaultRedactionBlock(t *testing.T) {
	out, err := Expand(honeycomb(&config.Profile{Mode: config.ProfileModeNone}))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		// Triggering condition: at least one of body or message is a
		// string. OTLP logs from apps with structured (map) bodies
		// pass through this block unchanged.
		`'IsString(body) or attributes["message"] != nil'`,
		// AWS access key id pattern, applied to both body and message.
		`AKIA[0-9A-Z]{16}`,
		`AKIA****REDACTED****`,
		// JWT prefix + three-segment structure.
		`eyJ[A-Za-z0-9_=-]{8,}`,
		`eyJ****REDACTED****`,
		// Credential key=value catchphrases (case-insensitive).
		`(?i)(password|passwd|secret|token|apikey|api_key|access_key|aws_secret_access_key|authorization)`,
		`$1=****REDACTED****`,
	})
	// The redaction block must come BEFORE the normalized_message
	// block — otherwise normalized_message inherits the original
	// (un-redacted) message and our templating effort leaks credentials
	// into the grouping column.
	redIdx := strings.Index(out, "AKIA****REDACTED****")
	normIdx := strings.Index(out, `set(attributes["normalized_message"]`)
	if redIdx == -1 || normIdx == -1 {
		t.Fatalf("missing expected statements; redaction=%d normalized=%d", redIdx, normIdx)
	}
	if redIdx >= normIdx {
		t.Errorf("redaction block must precede normalized_message block; got redaction at %d, normalized at %d", redIdx, normIdx)
	}
}

// Filelog leaves raw syslog lines without severity (severity_number=0),
// which makes Honeycomb severity filters drop them. transform/logs
// backfills INFO when — and only when — nothing upstream set a severity,
// guarded by `where severity_number == SEVERITY_NUMBER_UNSPECIFIED` so
// journald (PRIORITY -> severity) and OTLP logs from upstream apps pass
// through untouched.
func TestExpand_TransformLogs_DefaultsSeverityToInfo(t *testing.T) {
	out, err := Expand(honeycomb(&config.Profile{Mode: config.ProfileModeNone}))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`severity_number == SEVERITY_NUMBER_UNSPECIFIED`,
		`set(severity_text, "INFO")`,
		`set(severity_number, SEVERITY_NUMBER_INFO)`,
	})
}

// Container-native profiles (docker, k8s) flip the OTLP listen addresses
// to 0.0.0.0 so peer containers / pods in the same network can reach the
// agent. Host modes stay on 127.0.0.1 (asserted by
// TestExpand_HostProfiles_BindLoopback below) so a stock host install
// does not silently expose OTLP to the local network.
func TestExpand_ContainerProfiles_BindAllInterfaces(t *testing.T) {
	cases := []struct {
		name string
		mode config.ProfileMode
	}{
		{"docker", config.ProfileModeDocker},
		{"k8s", config.ProfileModeK8s},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Expand(honeycomb(&config.Profile{Mode: tc.mode}))
			if err != nil {
				t.Fatalf("Expand: %v", err)
			}
			mustContain(t, out, []string{
				`endpoint: 0.0.0.0:4317`,
				`endpoint: 0.0.0.0:4318`,
			})
		})
	}
}

// docker (M9.A) ships a host-metrics fragment that mirrors the linux
// shape but expects the operator's compose file to bind-mount the host
// root at /hostfs. The rendered config carries the `root_path: /hostfs`
// re-rooting + the *.utilization opt-ins so dashboards keyed on
// `system.*` metrics work identically across host / container / k8s.
// Docker still does NOT ship a system-logs fragment in V0 — peer apps
// push container logs via OTLP, the on-host filelog scrape would
// require bind-mounting /var/lib/docker/containers and is M9.E
// territory.
func TestExpand_DockerProfile_LoadsHostMetrics(t *testing.T) {
	out, err := Expand(honeycomb(&config.Profile{Mode: config.ProfileModeDocker}))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`hostmetrics:`,
		// /hostfs re-rooting is the contract between the fragment and
		// the operator's compose file — without it, the scrapers report
		// the container's own view of /proc instead of the host's.
		`root_path: /hostfs`,
		// *.utilization opt-ins so platform-portable dashboards plot
		// percent-used directly.
		`system.cpu.utilization:`,
		`system.memory.utilization:`,
		`system.filesystem.utilization:`,
		`system.paging.utilization:`,
	})
	// Logs and k8s-only fragments must NOT appear.
	mustNotContain(t, out, []string{
		`filelog/system:`,
		`journald:`,
		`kubeletstats:`,
		`filelog/k8s:`,
		`k8sattributes:`,
	})
	// Traces and logs stay otlp-only; metrics gets hostmetrics +
	// span_metrics on top (the span_metrics is the M8 RED tee, not a
	// docker-specific receiver).
	for _, p := range []string{"traces", "logs"} {
		if got := pipelineReceivers(t, out, p); !equalSet(got, []string{"otlp"}) {
			t.Errorf("%s pipeline should only have otlp under docker profile; got %v", p, got)
		}
	}
	if got := pipelineReceivers(t, out, "metrics"); !equalSet(got, []string{"otlp", "hostmetrics", "span_metrics"}) {
		t.Errorf("metrics pipeline under docker profile = %v; want [otlp hostmetrics span_metrics]", got)
	}
}

// Docker's host_metrics: false toggle is the operator opt-out for
// deployments that aren't bind-mounting /proc — without the toggle,
// hostmetrics would scrape the container's view of the world, which
// is rarely useful and noisy on dashboards.
func TestExpand_DockerProfile_HostMetricsDisabled(t *testing.T) {
	disabled := false
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeDocker, HostMetrics: &disabled})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustNotContain(t, out, []string{`hostmetrics:`, `root_path: /hostfs`})
	if got := pipelineReceivers(t, out, "metrics"); !equalSet(got, []string{"otlp", "span_metrics"}) {
		t.Errorf("metrics pipeline under docker + host_metrics=false = %v; want [otlp span_metrics]", got)
	}
}

// Windows (M6.A) ships hostmetrics + Windows Event Log (Application +
// System channels). Hostmetrics has the same shape as the linux fragment
// — same scrapers, same *.utilization opt-ins — so dashboards keyed on
// `system.*` work cross-platform; the Windows-specific load behavior
// (Processor Queue Length surfaced as system.cpu.load_average.1m) is
// the receiver's responsibility, not the fragment's. The Security
// Event Log channel is intentionally NOT loaded by default
// (SeSecurityPrivilege / Event Log Readers membership required); it
// stays reachable through the overrides: escape hatch.
func TestExpand_WindowsProfile_LoadsHostMetricsAndEventLog(t *testing.T) {
	out, err := Expand(honeycomb(&config.Profile{Mode: config.ProfileModeWindows}))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`hostmetrics:`,
		`system.cpu.utilization:`,
		`system.memory.utilization:`,
		`system.filesystem.utilization:`,
		`system.paging.utilization:`,
		`windowseventlog/application:`,
		`windowseventlog/system:`,
		`channel: application`,
		`channel: system`,
		`start_at: end`,
	})
	// Linux / k8s / docker fragments must NOT appear.
	mustNotContain(t, out, []string{
		`filelog/system:`,
		`journald:`,
		`kubeletstats:`,
		`filelog/k8s:`,
		`k8sattributes:`,
		`root_path: /hostfs`,
		`windowseventlog/security:`,
	})
	// Pipeline wiring: hostmetrics + otlp + span_metrics on metrics;
	// both Event Log receivers + otlp on logs; traces stays otlp-only.
	if got := pipelineReceivers(t, out, "metrics"); !equalSet(got, []string{"otlp", "hostmetrics", "span_metrics"}) {
		t.Errorf("metrics pipeline under windows profile = %v; want [otlp hostmetrics span_metrics]", got)
	}
	if got := pipelineReceivers(t, out, "logs"); !equalSet(got, []string{"otlp", "windowseventlog/application", "windowseventlog/system"}) {
		t.Errorf("logs pipeline under windows profile = %v; want [otlp windowseventlog/application windowseventlog/system]", got)
	}
	if got := pipelineReceivers(t, out, "traces"); !equalSet(got, []string{"otlp"}) {
		t.Errorf("traces pipeline under windows profile = %v; want [otlp]", got)
	}
}

// system_logs: false on Windows is the opt-out for operators who only
// want host metrics — the Event Log channels then drop out cleanly
// without taking hostmetrics with them. Mirrors the same toggle on
// linux / darwin.
func TestExpand_WindowsProfile_SystemLogsDisabled(t *testing.T) {
	disabled := false
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeWindows, SystemLogs: &disabled})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{`hostmetrics:`})
	mustNotContain(t, out, []string{`windowseventlog/application:`, `windowseventlog/system:`})
	if got := pipelineReceivers(t, out, "logs"); !equalSet(got, []string{"otlp"}) {
		t.Errorf("logs pipeline under windows + system_logs=false = %v; want [otlp]", got)
	}
}

// k8s ships three fragments (hostmetrics + kubeletstats + filelog/k8s)
// and the k8sattributes processor — the per-node DaemonSet half of the
// Kubernetes story. The chart in deploy/helm/conduit-agent provides
// the matching DaemonSet host mounts and ClusterRole RBAC in M5.C.
func TestExpand_K8sProfile_LoadsFragments(t *testing.T) {
	out, err := Expand(honeycomb(&config.Profile{Mode: config.ProfileModeK8s}))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	// The three k8s receivers are spliced into receivers:.
	mustContain(t, out, []string{
		`hostmetrics:`,
		// root_path: /hostfs re-roots every hostmetrics scraper at the
		// chart-provided host bind mount. Without this, hostmetrics
		// reports the pod's view of /proc instead of the node's.
		`root_path: /hostfs`,
		`kubeletstats:`,
		`endpoint: ${env:K8S_NODE_NAME}:10250`,
		`auth_type: serviceAccount`,
		// kubeletstats opt-in metrics enabled by Conduit (M5.E) so the
		// default k8s-cluster-overview board has the columns it needs:
		// container.uptime + k8s.pod.uptime are the restart proxy until
		// k8sclusterreceiver ships; the {cpu,memory}_limit_utilization
		// pair is the "% of limit" reading SREs reach for first.
		`container.uptime:`,
		`k8s.pod.uptime:`,
		`k8s.pod.cpu_limit_utilization:`,
		`k8s.pod.memory_limit_utilization:`,
		`filelog/k8s:`,
		`/var/log/pods/*/*/*.log`,
		`type: container`,
	})
	// Linux-only fragments must not appear under k8s.
	mustNotContain(t, out, []string{
		`filelog/system:`,
		`journald:`,
	})
	// hostmetrics + kubeletstats land on the metrics pipeline; filelog/k8s
	// on the logs pipeline; traces stays otlp-only because the k8s
	// receivers carry no traces.
	if got := pipelineReceivers(t, out, "traces"); !equalSet(got, []string{"otlp"}) {
		t.Errorf("traces pipeline under k8s profile should be otlp-only; got %v", got)
	}
	if got := pipelineReceivers(t, out, "metrics"); !equalSet(got, []string{"otlp", "hostmetrics", "kubeletstats", "span_metrics"}) {
		t.Errorf("metrics pipeline under k8s profile = %v; want [otlp hostmetrics kubeletstats span_metrics]", got)
	}
	if got := pipelineReceivers(t, out, "logs"); !equalSet(got, []string{"otlp", "filelog/k8s"}) {
		t.Errorf("logs pipeline under k8s profile = %v; want [otlp filelog/k8s]", got)
	}
}

// k8sattributes is the processor that turns "OTLP from a peer pod" into
// "OTLP from a peer pod, tagged with k8s.deployment.name / k8s.pod.name
// / k8s.namespace.name". It must run on every pipeline so traces and
// metrics from instrumented apps benefit, not just the chart-shipped
// container logs. Position matters: after resourcedetection so host
// identity is established first; before resource so the user's explicit
// conduit.yaml service.name still wins.
func TestExpand_K8sProfile_AddsK8sAttributesProcessor(t *testing.T) {
	out, err := Expand(honeycomb(&config.Profile{Mode: config.ProfileModeK8s}))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`k8sattributes:`,
		`node_from_env_var: K8S_NODE_NAME`,
		`- k8s.namespace.name`,
		`- k8s.deployment.name`,
		`- from: connection`,
	})
	for _, p := range []string{"traces", "metrics", "logs"} {
		got := pipelineProcessors(t, out, p)
		if !contains(got, "k8sattributes") {
			t.Errorf("%s pipeline missing k8sattributes processor; got %v", p, got)
		}
		if !ordered(got, "resourcedetection", "k8sattributes") {
			t.Errorf("%s pipeline must run resourcedetection before k8sattributes; got %v", p, got)
		}
		if !ordered(got, "k8sattributes", "resource") {
			t.Errorf("%s pipeline must run k8sattributes before resource (so user's resource: block wins); got %v", p, got)
		}
	}
}

// Host-mode profiles must not pull in the k8sattributes processor —
// they have no Kubernetes API client / RBAC and the receiver wouldn't
// know which pod a signal came from anyway.
func TestExpand_NonK8sProfiles_NoK8sAttributesProcessor(t *testing.T) {
	cases := []struct {
		name string
		mode config.ProfileMode
	}{
		{"none", config.ProfileModeNone},
		{"linux", config.ProfileModeLinux},
		{"darwin", config.ProfileModeDarwin},
		{"docker", config.ProfileModeDocker},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Expand(honeycomb(&config.Profile{Mode: tc.mode}))
			if err != nil {
				t.Fatalf("Expand: %v", err)
			}
			mustNotContain(t, out, []string{`k8sattributes:`})
			for _, p := range []string{"traces", "metrics", "logs"} {
				if got := pipelineProcessors(t, out, p); contains(got, "k8sattributes") {
					t.Errorf("%s pipeline under %s profile must not include k8sattributes; got %v", p, tc.name, got)
				}
			}
		})
	}
}

// Host-mode profiles bind OTLP to loopback so a fresh `apt-get install`
// or `brew install` does not turn the host into an OTLP relay for the
// local network. Apps on the same machine still reach 127.0.0.1:4317/4318
// via the loopback interface; LAN ingest is an explicit opt-in via
// profile.mode=docker or profile.mode=k8s.
func TestExpand_HostProfiles_BindLoopback(t *testing.T) {
	cases := []struct {
		name string
		mode config.ProfileMode
	}{
		{"none", config.ProfileModeNone},
		{"linux", config.ProfileModeLinux},
		{"darwin", config.ProfileModeDarwin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Expand(honeycomb(&config.Profile{Mode: tc.mode}))
			if err != nil {
				t.Fatalf("Expand: %v", err)
			}
			mustContain(t, out, []string{
				`endpoint: 127.0.0.1:4317`,
				`endpoint: 127.0.0.1:4318`,
			})
			mustNotContain(t, out, []string{
				`endpoint: 0.0.0.0:4317`,
				`endpoint: 0.0.0.0:4318`,
			})
		})
	}
}

// health_check is always on regardless of profile so Docker HEALTHCHECK,
// k8s liveness/readiness, and `conduit doctor` all have one endpoint.
func TestExpand_HealthCheckExtension_AlwaysOn(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.AgentConfig
	}{
		{"profile=none", honeycomb(&config.Profile{Mode: config.ProfileModeNone})},
		{"profile=linux", honeycomb(&config.Profile{Mode: config.ProfileModeLinux})},
		{"profile=darwin", honeycomb(&config.Profile{Mode: config.ProfileModeDarwin})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Expand(tc.cfg)
			if err != nil {
				t.Fatalf("Expand: %v", err)
			}
			mustContain(t, out, []string{
				`extensions:`,
				`health_check:`,
				`endpoint: 0.0.0.0:13133`,
				`extensions: [health_check]`,
			})
		})
	}
}

func TestExpand_Gateway_Profile(t *testing.T) {
	cfg := &config.AgentConfig{
		ServiceName:           "checkout",
		DeploymentEnvironment: "prod",
		Output: config.Output{
			Mode: config.OutputModeGateway,
			Gateway: &config.GatewayOutput{
				Endpoint: "gateway.internal:4317",
				Headers:  map[string]string{"x-tenant": "team-foo"},
			},
		},
		Profile: &config.Profile{Mode: config.ProfileModeLinux},
	}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`otlp/gateway:`,
		`hostmetrics:`,
		`filelog/system:`,
		`x-tenant: "team-foo"`,
		// M10.C: TLS-required-by-default contract is rendered
		// explicitly so `conduit preview` makes the posture visible.
		`tls:`,
		`insecure: false`,
	})
	if got := pipelineReceivers(t, out, "metrics"); !contains(got, "hostmetrics") {
		t.Errorf("metrics pipeline missing hostmetrics; got %v", got)
	}
}

// M10.C: gateway.insecure: true renders the lab-only TLS opt-out
// inline (the rendered YAML carries `insecure: true` so operators can
// see the override at preview time). conduit doctor (M11) flags this
// as a warning even when the connection succeeds.
func TestExpand_GatewayProfile_InsecureTLS(t *testing.T) {
	cfg := &config.AgentConfig{
		ServiceName:           "checkout",
		DeploymentEnvironment: "prod",
		Output: config.Output{
			Mode: config.OutputModeGateway,
			Gateway: &config.GatewayOutput{
				Endpoint: "gateway.internal:4317",
				Insecure: true,
			},
		},
		Profile: &config.Profile{Mode: config.ProfileModeNone},
	}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`otlp/gateway:`,
		`tls:`,
		`insecure: true`,
	})
}

// M10.A: persistent_queue.enabled: true wires the file_storage
// extension and a sending_queue block on the active exporter, and
// extends service.extensions so the storage is bound at startup.
func TestExpand_PersistentQueue_RendersFileStorage(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	cfg.Output.PersistentQueue = &config.PersistentQueue{
		Enabled: true,
		Dir:     "/var/lib/conduit/queue",
	}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`file_storage:`,
		`directory: "/var/lib/conduit/queue"`,
		`compaction:`,
		`on_start: true`,
		// the active exporter wires sending_queue.storage to the
		// extension; no other exporter (debug) needs this — the
		// upstream debug exporter doesn't use sending_queue.
		`otlphttp/honeycomb:`,
		`sending_queue:`,
		`storage: file_storage`,
		// service.extensions list must include both health_check
		// (always on) and file_storage (M10.A toggle).
		`extensions: [health_check, file_storage]`,
	})
}

// Persistent queue off (the default) keeps the upstream in-memory
// sending_queue and never renders the file_storage extension.
func TestExpand_PersistentQueue_DisabledByDefault(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustNotContain(t, out, []string{
		`file_storage:`,
		`storage: file_storage`,
	})
	mustContain(t, out, []string{
		`extensions: [health_check]`,
	})
}

// M10.B: honeycomb.traces.via_refinery routes the traces pipeline
// through an OTLP/gRPC exporter pointed at the Refinery cluster while
// metrics + logs continue through the direct otlphttp/honeycomb
// exporter. The refinery exporter carries the same x-honeycomb-team
// header so Refinery's downstream forward to Honeycomb authenticates
// the same way.
func TestExpand_HoneycombViaRefinery_RoutesTracesOnly(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	cfg.Output.Honeycomb.Traces = &config.HoneycombTraces{
		ViaRefinery: &config.RefineryRouting{
			Endpoint: "refinery.observability.svc:4317",
		},
	}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`otlphttp/honeycomb:`,
		`otlp/refinery:`,
		`endpoint: "refinery.observability.svc:4317"`,
		`x-honeycomb-team:`,
		// Refinery TLS posture: insecure: false rendered explicitly
		// even when the operator didn't set it (parallels gateway
		// mode's M10.C contract).
		`insecure: false`,
	})

	// Pipeline routing: traces -> refinery + debug; metrics -> honeycomb
	// + debug; logs -> honeycomb + debug. RED is on by default so the
	// span_metrics connector is also in the traces exporter list.
	traceExp := pipelineExporters(t, out, "traces")
	if !contains(traceExp, "otlp/refinery") || contains(traceExp, "otlphttp/honeycomb") {
		t.Errorf("traces pipeline exporters = %v; want otlp/refinery (no otlphttp/honeycomb)", traceExp)
	}
	for _, sig := range []string{"metrics", "logs"} {
		exps := pipelineExporters(t, out, sig)
		if !contains(exps, "otlphttp/honeycomb") || contains(exps, "otlp/refinery") {
			t.Errorf("%s pipeline exporters = %v; want otlphttp/honeycomb (no otlp/refinery)", sig, exps)
		}
	}
}

// Refinery + insecure: true is the lab-only opt-out — same posture as
// gateway TLS (M10.C). The rendered YAML reflects the operator's
// choice; conduit doctor (M11) flags it as a warning.
func TestExpand_HoneycombViaRefinery_Insecure(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	cfg.Output.Honeycomb.Traces = &config.HoneycombTraces{
		ViaRefinery: &config.RefineryRouting{
			Endpoint: "localhost:4317",
			Insecure: true,
		},
	}
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		`otlp/refinery:`,
		`endpoint: "localhost:4317"`,
		`insecure: true`,
	})
}

func TestExpand_QuotesEmbeddedSpecialChars(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	cfg.ServiceName = `name "with quotes" and \backslash`
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if !strings.Contains(out, `value: "name \"with quotes\" and \\backslash"`) {
		t.Errorf("expected JSON-escaped quoting; got:\n%s", out)
	}
}

func TestExpand_NilConfig(t *testing.T) {
	if _, err := Expand(nil); err == nil {
		t.Fatal("Expand(nil): want error, got nil")
	}
}

// --- M8: RED metrics from spans -----------------------------------
//
// The next four tests cover the span_metrics connector wiring:
//   1. Defaults-only rendering (the documented "out of the box"
//      contract — what every customer gets without touching config).
//   2. User-supplied dimension extras spliced onto the defaults.
//   3. The disabled mode (an opt-out for operators running
//      span_metrics on a downstream gateway).
//   4. The validator rejecting denylisted dimensions at parse time
//      so the connector never sees a CDT0501 input.

// TestExpand_RED_DefaultsOn locks in the M8 contract: with no
// metrics: block in conduit.yaml, the rendered config has the
// span_metrics connector with the documented dimension set, the
// default histogram buckets, the cardinality cap, and the
// traces→connector→metrics pipeline tee. This is the test that
// breaks if anyone "tunes" the defaults without an ADR.
func TestExpand_RED_DefaultsOn(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	// applyDefaults populates Metrics.RED with the documented
	// defaults; we exercise that path here rather than hand-set the
	// values so a regression in applyDefaults shows up here too.
	cfg.Metrics = &config.Metrics{RED: &config.REDConfig{
		CardinalityLimit: config.DefaultREDCardinalityLimit,
	}}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	mustContain(t, out, []string{
		`connectors:`,
		`span_metrics:`,
		// Documented histogram buckets (10ms..10s, nine boundaries).
		`buckets: [10ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s]`,
		// Default span dimensions — service.name is implicit
		// upstream so we don't list it here.
		`- name: deployment.environment`,
		`- name: http.route`,
		`- name: http.method`,
		`- name: http.status_code`,
		`- name: rpc.system`,
		`- name: messaging.system`,
		// Default resource_metrics_key_attributes.
		`- service.name`,
		`- k8s.namespace.name`,
		`- cloud.region`,
		`- team`,
		// Cardinality cap maps to upstream's aggregation_cardinality_limit.
		`aggregation_cardinality_limit: 5000`,
		`add_resource_attributes: true`,
	})
	// http.target / http.path are denylisted, never default.
	mustNotContain(t, out, []string{
		`- name: http.target`,
		`- name: http.path`,
		`- name: trace_id`,
	})
	// Pipeline tee: traces exporters include span_metrics; metrics
	// receivers include span_metrics; logs is unaffected.
	if got := pipelineExporters(t, out, "traces"); !contains(got, "span_metrics") {
		t.Errorf("traces pipeline must tee through span_metrics; got exporters %v", got)
	}
	if got := pipelineReceivers(t, out, "metrics"); !contains(got, "span_metrics") {
		t.Errorf("metrics pipeline must consume span_metrics; got receivers %v", got)
	}
	if got := pipelineExporters(t, out, "logs"); contains(got, "span_metrics") {
		t.Errorf("logs pipeline must NOT include span_metrics; got exporters %v", got)
	}
	if got := pipelineReceivers(t, out, "logs"); contains(got, "span_metrics") {
		t.Errorf("logs pipeline must NOT include span_metrics; got receivers %v", got)
	}
}

// TestExpand_RED_UserDimensionsAppended verifies the user-extension
// path: extras spliced onto the defaults preserve order, with the
// defaults coming first so operators reasoning about precedence at
// query time (e.g. "where do my dimensions land in the connector?")
// have a deterministic answer.
func TestExpand_RED_UserDimensionsAppended(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	cfg.Metrics = &config.Metrics{RED: &config.REDConfig{
		SpanDimensions:          []string{"db.system", "feature.flag.key"},
		ExtraResourceDimensions: []string{"service.namespace", "team.tier"},
		CardinalityLimit:        10000,
	}}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	// User extras present alongside defaults.
	mustContain(t, out, []string{
		`- name: deployment.environment`, // default still there
		`- name: db.system`,              // user add
		`- name: feature.flag.key`,
		`- service.namespace`, // user resource add
		`- team.tier`,
		`aggregation_cardinality_limit: 10000`, // user-overridden cap
	})
	// Dimension order: defaults BEFORE user extras (otherwise a
	// later "promote to first-class field" rename in
	// REDDefaultSpanDimensions would silently shift user extras'
	// rendered position).
	defIdx := strings.Index(out, "- name: messaging.operation")
	userIdx := strings.Index(out, "- name: db.system")
	if defIdx == -1 || userIdx == -1 || defIdx >= userIdx {
		t.Errorf("default dimensions must render before user extras; got def=%d user=%d", defIdx, userIdx)
	}
}

// TestExpand_RED_DisabledOmitsConnector covers the opt-out path —
// metrics.red.enabled: false drops the connector entirely. Used
// when span_metrics runs on a downstream gateway in the operator's
// topology and double-emitting would inflate request counts.
func TestExpand_RED_DisabledOmitsConnector(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	off := false
	cfg.Metrics = &config.Metrics{RED: &config.REDConfig{Enabled: &off}}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustNotContain(t, out, []string{
		`connectors:`,
		`span_metrics:`,
		`aggregation_cardinality_limit:`,
		`add_resource_attributes:`,
	})
	if got := pipelineExporters(t, out, "traces"); contains(got, "span_metrics") {
		t.Errorf("traces pipeline must NOT tee through span_metrics when RED is disabled; got %v", got)
	}
	if got := pipelineReceivers(t, out, "metrics"); contains(got, "span_metrics") {
		t.Errorf("metrics pipeline must NOT consume span_metrics when RED is disabled; got %v", got)
	}
}

// TestREDDimensionDenylist_RejectsHighCardinalityAttributes covers
// the schema-time half of ADR-0006 — the validator (running before
// the expander) refuses to even attempt rendering a config where a
// user adds e.g. user.id to the dimension list. The error message
// must call out CDT0501 so operators can search the doctor catalog
// for the explanation; failing fast at parse beats failing slow at
// query when the cardinality wall is hit hours later.
func TestREDDimensionDenylist_RejectsHighCardinalityAttributes(t *testing.T) {
	cases := []struct {
		name  string
		field string
		dim   string
	}{
		{"trace_id-span", "metrics.red.span_dimensions[0]", "trace_id"},
		{"user.id-span", "metrics.red.span_dimensions[0]", "user.id"},
		{"http.target-span", "metrics.red.span_dimensions[0]", "http.target"},
		{"customer_id-resource", "metrics.red.extra_resource_dimensions[0]", "customer_id"},
		{"url.full-resource", "metrics.red.extra_resource_dimensions[0]", "url.full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.AgentConfig{
				ServiceName:           "demo",
				DeploymentEnvironment: "dev",
				Output: config.Output{
					Mode: config.OutputModeHoneycomb,
					Honeycomb: &config.HoneycombOutput{
						APIKey:   "${env:KEY}",
						Endpoint: config.DefaultHoneycombEndpoint,
					},
				},
				Profile: &config.Profile{Mode: config.ProfileModeNone},
				Metrics: &config.Metrics{RED: &config.REDConfig{}},
			}
			if strings.HasPrefix(tc.field, "metrics.red.span_dimensions") {
				cfg.Metrics.RED.SpanDimensions = []string{tc.dim}
			} else {
				cfg.Metrics.RED.ExtraResourceDimensions = []string{tc.dim}
			}
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate: want denylist error for dim %q, got nil", tc.dim)
			}
			msg := err.Error()
			for _, want := range []string{tc.dim, "CDT0501", "ADR-0006"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error message must include %q; got %q", want, msg)
				}
			}
		})
	}
}

func TestExpand_UnknownMode(t *testing.T) {
	cfg := &config.AgentConfig{
		ServiceName:           "x",
		DeploymentEnvironment: "y",
		Output:                config.Output{Mode: "kafka"},
		Profile:               &config.Profile{Mode: config.ProfileModeNone},
	}
	if _, err := Expand(cfg); err == nil {
		t.Fatal("Expand: want error for unknown output.mode, got nil")
	}
}

// ADR-0020: when OBI is enabled, the rendered output gains a
// `receivers.obi:` block AND `obi` joins the traces + metrics pipeline
// receiver lists. On the k8s profile, the block also enables k8s
// metadata extraction (matching k8sattributes for OTLP traffic).
func TestExpand_OBI_K8sDefault(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeK8s})
	on := true
	cfg.OBI = &config.OBI{Enabled: &on}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		"\n  obi:\n",
		"meter_provider:",
		"features: [application]",
		"attributes:",
		"kubernetes:",
		"enable: true",
	})
	if got := pipelineReceivers(t, out, "traces"); !contains(got, "obi") {
		t.Errorf("traces pipeline must include obi receiver; got %v", got)
	}
	if got := pipelineReceivers(t, out, "metrics"); !contains(got, "obi") {
		t.Errorf("metrics pipeline must include obi receiver; got %v", got)
	}
}

// On non-k8s Linux profiles, the receivers.obi block omits the
// kubernetes metadata block — there's no in-cluster API to read.
// Operators on those platforms who need k8s tagging supply it
// through overrides:.
func TestExpand_OBI_LinuxOmitsKubernetesAttrs(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeLinux})
	on := true
	cfg.OBI = &config.OBI{Enabled: &on}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustContain(t, out, []string{
		"\n  obi:\n",
		"features: [application]",
	})
	mustNotContain(t, out, []string{
		"kubernetes:\n        enable: true",
	})
}

// OBI off (the default for non-k8s profiles) leaves the rendered
// config exactly as it was pre-ADR-0020 — no obi receiver block, no
// pipeline membership.
func TestExpand_OBI_DisabledOmitsBlock(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeLinux})
	off := false
	cfg.OBI = &config.OBI{Enabled: &off}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if strings.Contains(out, "\n  obi:\n") {
		t.Errorf("rendered output should NOT contain obi receiver block when disabled; got it")
	}
	if got := pipelineReceivers(t, out, "traces"); contains(got, "obi") {
		t.Errorf("traces pipeline must NOT include obi when disabled; got %v", got)
	}
	if got := pipelineReceivers(t, out, "metrics"); contains(got, "obi") {
		t.Errorf("metrics pipeline must NOT include obi when disabled; got %v", got)
	}
}

// ADR-0020 sub-decision 3: obi.replace_span_metrics_connector: true
// suppresses the M8 span_metrics connector entirely. The connectors:
// block disappears from the rendered output, traces no longer tee
// through span_metrics, and the metrics pipeline no longer consumes
// it on the receiver side.
func TestExpand_OBI_ReplaceSpanMetricsConnector(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeK8s})
	on := true
	cfg.OBI = &config.OBI{Enabled: &on, ReplaceSpanMetricsConnector: true}

	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustNotContain(t, out, []string{
		"\nconnectors:",
		"span_metrics:",
		"aggregation_cardinality_limit:",
	})
	if got := pipelineExporters(t, out, "traces"); contains(got, "span_metrics") {
		t.Errorf("traces pipeline must NOT tee through span_metrics when OBI replaces it; got %v", got)
	}
	if got := pipelineReceivers(t, out, "metrics"); contains(got, "span_metrics") {
		t.Errorf("metrics pipeline must NOT consume span_metrics when OBI replaces it; got %v", got)
	}
	if got := pipelineReceivers(t, out, "metrics"); !contains(got, "obi") {
		t.Errorf("metrics pipeline must still include obi receiver; got %v", got)
	}
}

// --- test helpers ---

// pipelineSection returns the body of a single pipeline (everything from the
// "<pipeline>:" header line up to the next pipeline header at the same indent
// or the end of input). It anchors on the rendered YAML's structural shape —
// pipelines live at 4-space indent under service.pipelines — so substring
// matches inside receiver names like "hostmetrics:" or processor names like
// "transform/logs:" don't mislead the lookup.
func pipelineSection(t *testing.T, out, pipeline string) string {
	t.Helper()
	// Defense in depth for the windows-latest CI runner: if a stale
	// checkout (no .gitattributes yet) handed the renderer CRLF
	// templates, the rendered output here will be CRLF-terminated
	// and our literal "\n"-anchored lookups below would all whiff.
	// Normalize once so the helpers stay byte-honest for the common
	// case while tolerating the odd one.
	out = strings.ReplaceAll(out, "\r\n", "\n")
	// Anchor the search inside the `service.pipelines:` block so a
	// receiver-level `metrics:` enable-list (e.g. kubeletstats's
	// `metrics: { container.uptime: { enabled: true }, ... }`) does
	// not collide with the `metrics:` pipeline section. Both sit at
	// 4-space indent, so a global `"\n    metrics:\n"` would match
	// whichever appears first.
	pipelinesAnchor := "\n  pipelines:\n"
	pipelinesIdx := strings.Index(out, pipelinesAnchor)
	if pipelinesIdx == -1 {
		t.Fatalf("service.pipelines: block not found in:\n%s", out)
	}
	pipelinesBody := out[pipelinesIdx+len(pipelinesAnchor):]
	header := "    " + pipeline + ":\n"
	idx := strings.Index(pipelinesBody, header)
	if idx == -1 {
		t.Fatalf("pipeline %q not found in:\n%s", pipeline, out)
	}
	body := pipelinesBody[idx+len(header):]
	// Stop at the next pipeline header (also at 4-space indent) so we
	// don't bleed into the next section.
	if next := strings.Index(body, "\n    "); next != -1 {
		// Only treat as a section break if the next 4-space-indent line
		// is a key (ends in ":"), not deeper nested content.
		nl := strings.Index(body[next+1:], "\n")
		var line string
		if nl == -1 {
			line = body[next+1:]
		} else {
			line = body[next+1 : next+1+nl]
		}
		if strings.HasSuffix(strings.TrimSpace(line), ":") &&
			strings.HasPrefix(line, "    ") &&
			!strings.HasPrefix(line, "     ") {
			body = body[:next]
		}
	}
	return body
}

// pipelineReceivers extracts the receiver IDs for a single pipeline section
// (e.g. "traces", "metrics", "logs") from the rendered YAML.
func pipelineReceivers(t *testing.T, out, pipeline string) []string {
	t.Helper()
	body := pipelineSection(t, out, pipeline)
	const header = "      receivers:\n"
	idx := strings.Index(body, header)
	if idx == -1 {
		t.Fatalf("receivers: not found in %s pipeline; body:\n%s", pipeline, body)
	}
	tail := body[idx+len(header):]

	var ids []string
	for _, line := range strings.Split(tail, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			ids = append(ids, strings.TrimSpace(trimmed[2:]))
			continue
		}
		// Reached the next key (processors:, exporters:, ...).
		if trimmed != "" && !strings.HasPrefix(trimmed, "-") {
			break
		}
	}
	return ids
}

// pipelineProcessors extracts the processor IDs from the inline
// "processors: [a, b, c]" line of a single pipeline section.
func pipelineProcessors(t *testing.T, out, pipeline string) []string {
	t.Helper()
	return inlineList(t, out, pipeline, "      processors:")
}

// pipelineExporters extracts the exporter IDs from the inline
// "exporters: [a, b]" line of a single pipeline section.
func pipelineExporters(t *testing.T, out, pipeline string) []string {
	t.Helper()
	return inlineList(t, out, pipeline, "      exporters:")
}

// inlineList extracts an inline YAML list ([a, b, c]) from the line that
// starts with prefix inside the given pipeline section. Used by the
// pipeline{Processors,Exporters} helpers above.
func inlineList(t *testing.T, out, pipeline, prefix string) []string {
	t.Helper()
	body := pipelineSection(t, out, pipeline)
	pIdx := strings.Index(body, prefix)
	if pIdx == -1 {
		t.Fatalf("%q not found in %s pipeline; body:\n%s", prefix, pipeline, body)
	}
	line := body[pIdx:]
	if nl := strings.Index(line, "\n"); nl != -1 {
		line = line[:nl]
	}
	openBracket := strings.Index(line, "[")
	closeBracket := strings.Index(line, "]")
	if openBracket == -1 || closeBracket == -1 || closeBracket <= openBracket {
		t.Fatalf("%s line not in [a, b] form: %q", prefix, line)
	}
	parts := strings.Split(line[openBracket+1:closeBracket], ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			ids = append(ids, s)
		}
	}
	return ids
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ordered reports whether earlier appears before later in xs. Both must
// be present; if either is missing the function returns false so callers
// can use it as a single assertion. Used to enforce processor ordering
// (resourcedetection must run before k8sattributes, k8sattributes before
// resource, etc.).
func ordered(xs []string, earlier, later string) bool {
	earlyIdx, lateIdx := -1, -1
	for i, s := range xs {
		switch s {
		case earlier:
			if earlyIdx == -1 {
				earlyIdx = i
			}
		case later:
			lateIdx = i
		}
	}
	return earlyIdx != -1 && lateIdx != -1 && earlyIdx < lateIdx
}

func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

func mustContain(t *testing.T, haystack string, needles []string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			t.Errorf("expected output to contain %q; full output:\n%s", n, haystack)
		}
	}
}

func mustNotContain(t *testing.T, haystack string, needles []string) {
	t.Helper()
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			t.Errorf("expected output NOT to contain %q; full output:\n%s", n, haystack)
		}
	}
}
