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
		`processes:`,    // darwin scraper list omits this
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

	if got := pipelineReceivers(t, out, "metrics"); !equalSet(got, []string{"otlp"}) {
		t.Errorf("metrics pipeline should only have otlp when host_metrics=false; got %v", got)
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

// docker is intentionally fragment-less in V0: scraping host metrics
// from inside a container needs /proc and /sys bind mounts the user
// must opt into at run time, so the docker profile only changes OTLP
// bind behavior and leaves receiver pipelines OTLP-only.
func TestExpand_DockerProfile_NoPlatformFragments(t *testing.T) {
	out, err := Expand(honeycomb(&config.Profile{Mode: config.ProfileModeDocker}))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	mustNotContain(t, out, []string{
		`hostmetrics:`,
		`filelog/system:`,
		`journald:`,
		`kubeletstats:`,
		`filelog/k8s:`,
		`k8sattributes:`,
	})
	for _, p := range []string{"traces", "metrics", "logs"} {
		if got := pipelineReceivers(t, out, p); !equalSet(got, []string{"otlp"}) {
			t.Errorf("%s pipeline should only have otlp under docker profile; got %v", p, got)
		}
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
	if got := pipelineReceivers(t, out, "metrics"); !equalSet(got, []string{"otlp", "hostmetrics", "kubeletstats"}) {
		t.Errorf("metrics pipeline under k8s profile = %v; want [otlp hostmetrics kubeletstats]", got)
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
	})
	if got := pipelineReceivers(t, out, "metrics"); !contains(got, "hostmetrics") {
		t.Errorf("metrics pipeline missing hostmetrics; got %v", got)
	}
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

// --- test helpers ---

// pipelineSection returns the body of a single pipeline (everything from the
// "<pipeline>:" header line up to the next pipeline header at the same indent
// or the end of input). It anchors on the rendered YAML's structural shape —
// pipelines live at 4-space indent under service.pipelines — so substring
// matches inside receiver names like "hostmetrics:" or processor names like
// "transform/logs:" don't mislead the lookup.
func pipelineSection(t *testing.T, out, pipeline string) string {
	t.Helper()
	header := "\n    " + pipeline + ":\n"
	idx := strings.Index(out, header)
	if idx == -1 {
		t.Fatalf("pipeline %q not found in:\n%s", pipeline, out)
	}
	body := out[idx+len(header):]
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
	body := pipelineSection(t, out, pipeline)
	const prefix = "      processors:"
	pIdx := strings.Index(body, prefix)
	if pIdx == -1 {
		t.Fatalf("processors: not found in %s pipeline; body:\n%s", pipeline, body)
	}
	line := body[pIdx:]
	if nl := strings.Index(line, "\n"); nl != -1 {
		line = line[:nl]
	}
	openBracket := strings.Index(line, "[")
	closeBracket := strings.Index(line, "]")
	if openBracket == -1 || closeBracket == -1 || closeBracket <= openBracket {
		t.Fatalf("processors line not in [a, b] form: %q", line)
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
