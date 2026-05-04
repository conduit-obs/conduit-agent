package config

import (
	"errors"
	"strings"
	"testing"
)

func TestParse_HoneycombMinimal(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.ServiceName != "demo" {
		t.Errorf("ServiceName: got %q, want %q", cfg.ServiceName, "demo")
	}
	if cfg.DeploymentEnvironment != "dev" {
		t.Errorf("DeploymentEnvironment: got %q, want %q", cfg.DeploymentEnvironment, "dev")
	}
	if cfg.Output.Mode != OutputModeHoneycomb {
		t.Errorf("Output.Mode: got %q, want %q", cfg.Output.Mode, OutputModeHoneycomb)
	}
	if cfg.Output.Honeycomb == nil {
		t.Fatal("Output.Honeycomb: nil")
	}
	if cfg.Output.Honeycomb.APIKey != "${env:KEY}" {
		t.Errorf("APIKey: got %q, want %q", cfg.Output.Honeycomb.APIKey, "${env:KEY}")
	}
	if cfg.Output.Honeycomb.Endpoint != DefaultHoneycombEndpoint {
		t.Errorf("Endpoint default: got %q, want %q", cfg.Output.Honeycomb.Endpoint, DefaultHoneycombEndpoint)
	}
	if cfg.Output.Gateway != nil {
		t.Errorf("Output.Gateway: got %+v, want nil", cfg.Output.Gateway)
	}
}

func TestParse_HoneycombCustomEndpoint(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: prod
output:
  mode: honeycomb
  honeycomb:
    api_key: secret
    endpoint: https://api.eu1.honeycomb.io
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Output.Honeycomb.Endpoint != "https://api.eu1.honeycomb.io" {
		t.Errorf("Endpoint: got %q, want EU URL", cfg.Output.Honeycomb.Endpoint)
	}
}

func TestParse_GatewayMinimal(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: prod
output:
  mode: gateway
  gateway:
    endpoint: gateway.internal:4317
    headers:
      x-honeycomb-team: ${env:KEY}
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Output.Mode != OutputModeGateway {
		t.Errorf("Output.Mode: got %q, want %q", cfg.Output.Mode, OutputModeGateway)
	}
	if cfg.Output.Gateway == nil {
		t.Fatal("Output.Gateway: nil")
	}
	if cfg.Output.Gateway.Endpoint != "gateway.internal:4317" {
		t.Errorf("Endpoint: got %q", cfg.Output.Gateway.Endpoint)
	}
	if got := cfg.Output.Gateway.Headers["x-honeycomb-team"]; got != "${env:KEY}" {
		t.Errorf("Headers[x-honeycomb-team]: got %q, want ${env:KEY}", got)
	}
}

func TestParse_OTLPMinimal(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: prod
output:
  mode: otlp
  otlp:
    endpoint: https://otlp.example.com
    headers:
      Authorization: Bearer ${env:VENDOR_TOKEN}
      DD-API-KEY: ${env:DD_API_KEY}
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Output.Mode != OutputModeOTLP {
		t.Errorf("Output.Mode: got %q, want %q", cfg.Output.Mode, OutputModeOTLP)
	}
	if cfg.Output.OTLP == nil {
		t.Fatal("Output.OTLP: nil")
	}
	if cfg.Output.OTLP.Endpoint != "https://otlp.example.com" {
		t.Errorf("Endpoint: got %q", cfg.Output.OTLP.Endpoint)
	}
	if got := cfg.Output.OTLP.Headers["Authorization"]; got != "Bearer ${env:VENDOR_TOKEN}" {
		t.Errorf("Headers[Authorization]: got %q", got)
	}
	if got := cfg.Output.OTLP.Headers["DD-API-KEY"]; got != "${env:DD_API_KEY}" {
		t.Errorf("Headers[DD-API-KEY]: got %q", got)
	}
}

func TestParse_OTLPWithCompressionAndInsecure(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: lab
output:
  mode: otlp
  otlp:
    endpoint: http://localhost:4318
    compression: none
    insecure: true
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Output.OTLP.Compression != "none" {
		t.Errorf("Compression: got %q, want none", cfg.Output.OTLP.Compression)
	}
	if !cfg.Output.OTLP.Insecure {
		t.Errorf("Insecure: got false, want true")
	}
}

func TestParse_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantPaths []string
	}{
		{
			// profile.mode=none disables the profile-default service.name
			// fill-in (ADR-0021), so service_name is still required here.
			// Profile-defaulted platforms are covered by the positive
			// TestParse_ServiceNameProfileDefault_* tests below.
			name: "missing service_name (mode=none)",
			yaml: `
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
profile:
  mode: none
`,
			wantPaths: []string{"service_name"},
		},
		{
			name: "missing deployment_environment",
			yaml: `
service_name: demo
output:
  mode: honeycomb
  honeycomb:
    api_key: x
`,
			wantPaths: []string{"deployment_environment"},
		},
		{
			name: "missing output.mode",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  honeycomb:
    api_key: x
`,
			wantPaths: []string{"output.mode"},
		},
		{
			name: "unknown output.mode",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  mode: kafka
`,
			wantPaths: []string{"output.mode"},
		},
		{
			name: "honeycomb mode with gateway block",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
  gateway:
    endpoint: g:4317
`,
			wantPaths: []string{"output.gateway"},
		},
		{
			name: "honeycomb mode missing block",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
`,
			wantPaths: []string{"output.honeycomb"},
		},
		{
			name: "honeycomb mode missing api_key",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    endpoint: https://api.honeycomb.io
`,
			wantPaths: []string{"output.honeycomb.api_key"},
		},
		{
			name: "gateway mode missing endpoint",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  mode: gateway
  gateway:
    headers:
      foo: bar
`,
			wantPaths: []string{"output.gateway.endpoint"},
		},
		{
			name: "gateway mode with honeycomb block",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  mode: gateway
  gateway:
    endpoint: g:4317
  honeycomb:
    api_key: x
`,
			wantPaths: []string{"output.honeycomb"},
		},
		{
			name: "otlp mode missing block",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  mode: otlp
`,
			wantPaths: []string{"output.otlp"},
		},
		{
			name: "otlp mode missing endpoint",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  mode: otlp
  otlp:
    headers:
      foo: bar
`,
			wantPaths: []string{"output.otlp.endpoint"},
		},
		{
			name: "otlp mode with honeycomb block",
			yaml: `
service_name: demo
deployment_environment: dev
output:
  mode: otlp
  otlp:
    endpoint: https://otlp.example.com
  honeycomb:
    api_key: x
`,
			wantPaths: []string{"output.honeycomb"},
		},
		{
			// profile.mode=none keeps service_name required even after
			// applyDefaults (ADR-0021).
			name: "all required fields missing (mode=none)",
			yaml: `
output:
  mode: honeycomb
profile:
  mode: none
`,
			wantPaths: []string{"service_name", "deployment_environment", "output.honeycomb"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.yaml))
			if err == nil {
				t.Fatal("Parse: want validation error, got nil")
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Parse: want *ValidationError, got %T (%v)", err, err)
			}
			gotPaths := make(map[string]bool, len(ve.Issues))
			for _, iss := range ve.Issues {
				gotPaths[iss.Path] = true
			}
			for _, want := range tc.wantPaths {
				if !gotPaths[want] {
					t.Errorf("missing expected issue at path %q; got issues: %s", want, ve.Error())
				}
			}
		})
	}
}

func TestParse_ProfileDefaultsWhenOmitted(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Profile == nil {
		t.Fatal("Profile: nil after defaulting; want auto with both flags on")
	}
	if cfg.Profile.Mode != ProfileModeAuto {
		t.Errorf("Profile.Mode: got %q, want %q", cfg.Profile.Mode, ProfileModeAuto)
	}
	if !cfg.Profile.HostMetricsEnabled() {
		t.Error("HostMetricsEnabled: got false, want true (default for auto)")
	}
	if !cfg.Profile.SystemLogsEnabled() {
		t.Error("SystemLogsEnabled: got false, want true (default for auto)")
	}
}

func TestParse_ProfileExplicitNone(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
profile:
  mode: none
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Profile.Mode != ProfileModeNone {
		t.Errorf("Profile.Mode: got %q, want %q", cfg.Profile.Mode, ProfileModeNone)
	}
	if cfg.Profile.HostMetricsEnabled() {
		t.Error("HostMetricsEnabled: got true with mode=none, want false")
	}
	if cfg.Profile.SystemLogsEnabled() {
		t.Error("SystemLogsEnabled: got true with mode=none, want false")
	}
}

func TestParse_ProfilePerFeatureToggle(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
profile:
  mode: linux
  host_metrics: true
  system_logs: false
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Profile.HostMetricsEnabled() {
		t.Error("HostMetricsEnabled: got false, want true (explicit)")
	}
	if cfg.Profile.SystemLogsEnabled() {
		t.Error("SystemLogsEnabled: got true, want false (explicit)")
	}
}

func TestParse_ProfileUnknownMode(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
profile:
  mode: amiga
`
	_, err := Parse(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("Parse: want validation error for unknown profile.mode")
	}
	if !strings.Contains(err.Error(), "profile.mode") {
		t.Errorf("error should mention profile.mode; got %v", err)
	}
}

func TestParse_RejectsUnknownFields(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
mystery_field: oops
`
	_, err := Parse(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("Parse: want error for unknown top-level field")
	}
	if !strings.Contains(err.Error(), "mystery_field") {
		t.Errorf("error should mention the unknown field name; got: %v", err)
	}
}

func TestParse_OverridesPassThrough(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
overrides:
  receivers:
    kubeletstats:
      collection_interval: 15s
  service:
    pipelines:
      logs:
        processors: [memory_limiter, resourcedetection, k8sattributes, resource, transform/logs, batch]
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Overrides) == 0 {
		t.Fatal("Overrides: should be populated")
	}
	receivers, ok := cfg.Overrides["receivers"].(map[string]any)
	if !ok {
		t.Fatalf("Overrides.receivers: not a map; got %T", cfg.Overrides["receivers"])
	}
	kubelet, ok := receivers["kubeletstats"].(map[string]any)
	if !ok {
		t.Fatalf("Overrides.receivers.kubeletstats: not a map; got %T", receivers["kubeletstats"])
	}
	if got := kubelet["collection_interval"]; got != "15s" {
		t.Errorf("collection_interval: got %v, want 15s", got)
	}
}

// The YAML literal below intentionally misspells "receivers" as
// "reciever" — the whole point of this test is verifying the
// validator surfaces unknown top-level override keys verbatim.
// Suppress the misspell linter for this function so that intent
// stays first-class instead of going through a workaround.
//
//nolint:misspell
func TestParse_OverridesUnknownTopLevelKey(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
overrides:
  reciever:  # typo
    foo: {}
`
	_, err := Parse(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("Parse: want validation error for typo in overrides top-level key")
	}
	if !strings.Contains(err.Error(), "overrides.reciever") {
		t.Errorf("error should mention the bad path; got: %v", err)
	}
	if !strings.Contains(err.Error(), "ADR-0012") {
		t.Errorf("error should point at ADR-0012 for context; got: %v", err)
	}
}

// TestParse_REDDefaultsWhenOmitted verifies the M8 contract that
// applyDefaults materializes Metrics.RED with the documented
// CardinalityLimit. Without this the expander's RED-on path would
// have to nil-check at every read, and the rendered config could
// drift from the published default if a future patch flipped the
// "0 == use default" semantics.
func TestParse_REDDefaultsWhenOmitted(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Metrics == nil || cfg.Metrics.RED == nil {
		t.Fatal("metrics.red: nil after defaulting; want struct with documented defaults")
	}
	if !cfg.Metrics.RED.REDEnabled() {
		t.Error("RED.REDEnabled: got false, want true (default)")
	}
	if cfg.Metrics.RED.CardinalityLimit != DefaultREDCardinalityLimit {
		t.Errorf("RED.CardinalityLimit: got %d, want %d (default)", cfg.Metrics.RED.CardinalityLimit, DefaultREDCardinalityLimit)
	}
	if len(cfg.Metrics.RED.SpanDimensions) != 0 || len(cfg.Metrics.RED.ExtraResourceDimensions) != 0 {
		t.Errorf("RED dimensions on a default-rendered config should be empty (defaults are baked into the expander, not the loaded config); got span=%v resource=%v",
			cfg.Metrics.RED.SpanDimensions, cfg.Metrics.RED.ExtraResourceDimensions)
	}
}

// TestParse_REDExplicitDimensions exercises the user-knob path:
// extras round-trip from YAML, the user-supplied cardinality limit
// overrides the default, and the validator does NOT reject any of
// the example "good" dimensions (db.system, feature.flag.key,
// service.namespace, team.tier).
func TestParse_REDExplicitDimensions(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
metrics:
  red:
    enabled: true
    cardinality_limit: 12000
    span_dimensions:
      - db.system
      - feature.flag.key
    extra_resource_dimensions:
      - service.namespace
      - team.tier
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	red := cfg.Metrics.RED
	if !red.REDEnabled() {
		t.Error("RED.REDEnabled: got false, want true")
	}
	if red.CardinalityLimit != 12000 {
		t.Errorf("RED.CardinalityLimit: got %d, want 12000", red.CardinalityLimit)
	}
	if len(red.SpanDimensions) != 2 || red.SpanDimensions[0] != "db.system" || red.SpanDimensions[1] != "feature.flag.key" {
		t.Errorf("RED.SpanDimensions: got %v", red.SpanDimensions)
	}
	if len(red.ExtraResourceDimensions) != 2 {
		t.Errorf("RED.ExtraResourceDimensions: got %v", red.ExtraResourceDimensions)
	}
}

// TestParse_REDDenylistedDimensionRejected covers the schema-time
// half of ADR-0006 from the user's surface: writing user.id under
// metrics.red.span_dimensions in conduit.yaml fails Parse with a
// CDT0501-mapped error before the agent ever touches the
// span_metrics connector.
func TestParse_REDDenylistedDimensionRejected(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
metrics:
  red:
    span_dimensions:
      - user.id
`
	_, err := Parse(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("Parse: want denylist error for user.id, got nil")
	}
	for _, want := range []string{"user.id", "CDT0501", "ADR-0006", "metrics.red.span_dimensions[0]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q; got: %v", want, err)
		}
	}
}

// TestParse_REDDisabledOptOut covers the operator who is running
// span_metrics on a downstream gateway and needs the agent to skip
// the connector to avoid double-emitting RED metrics.
func TestParse_REDDisabledOptOut(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
metrics:
  red:
    enabled: false
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Metrics.RED.REDEnabled() {
		t.Error("RED.REDEnabled: got true after enabled: false; opt-out is a footgun if it doesn't take")
	}
}

// M10.A: persistent_queue.enabled defaults Dir to
// DefaultPersistentQueueDir; explicit Dir is honored. Disabled
// persistent_queue stays inert.
func TestParse_PersistentQueueDefaultsDir(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantNil bool
		wantDir string
	}{
		{
			name: "enabled-without-dir",
			yaml: `
service_name: demo
deployment_environment: prod
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
  persistent_queue:
    enabled: true
`,
			wantDir: DefaultPersistentQueueDir,
		},
		{
			name: "enabled-with-explicit-dir",
			yaml: `
service_name: demo
deployment_environment: prod
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
  persistent_queue:
    enabled: true
    dir: /var/conduit/queue-2
`,
			wantDir: "/var/conduit/queue-2",
		},
		{
			name: "disabled-stays-disabled",
			yaml: `
service_name: demo
deployment_environment: prod
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
  persistent_queue:
    enabled: false
`,
			wantDir: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse(strings.NewReader(tc.yaml))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			pq := cfg.Output.PersistentQueue
			if pq == nil {
				t.Fatal("Output.PersistentQueue: nil; want non-nil after applyDefaults")
			}
			if pq.Dir != tc.wantDir {
				t.Errorf("Dir: got %q, want %q", pq.Dir, tc.wantDir)
			}
		})
	}
}

// Persistent queue rejects relative dirs and tmpfs-style dirs at
// validation time so the failure mode is "agent refuses to start
// with a clear error" rather than "queue silently disappears on
// every reboot".
func TestParse_PersistentQueueRejectsBadDirs(t *testing.T) {
	tests := []struct {
		name    string
		dir     string
		wantSub string // substring expected in the validation error
	}{
		{name: "relative", dir: "queue", wantSub: "must be an absolute path"},
		{name: "tmpfs-tmp", dir: "/tmp/conduit-queue", wantSub: "tmpfs"},
		{name: "tmpfs-shm", dir: "/dev/shm/q", wantSub: "tmpfs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yaml := `
service_name: demo
deployment_environment: prod
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
  persistent_queue:
    enabled: true
    dir: ` + tc.dir + "\n"
			_, err := Parse(strings.NewReader(yaml))
			if err == nil {
				t.Fatalf("Parse: want validation error for dir=%q", tc.dir)
			}
			if !strings.Contains(err.Error(), "output.persistent_queue.dir") {
				t.Errorf("error should mention output.persistent_queue.dir; got: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error should mention %q; got: %v", tc.wantSub, err)
			}
		})
	}
}

// M10.B: honeycomb.traces.via_refinery requires a non-empty endpoint;
// validation rejects an empty value with a path that points operators
// at the right field.
func TestParse_RefineryRequiresEndpoint(t *testing.T) {
	yaml := `
service_name: demo
deployment_environment: prod
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
    traces:
      via_refinery:
        endpoint: ""
`
	_, err := Parse(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("Parse: want validation error for empty refinery endpoint")
	}
	if !strings.Contains(err.Error(), "output.honeycomb.traces.via_refinery.endpoint") {
		t.Errorf("error should mention via_refinery.endpoint path; got: %v", err)
	}
}

func TestParse_RefineryAcceptsValidEndpoint(t *testing.T) {
	yaml := `
service_name: demo
deployment_environment: prod
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
    traces:
      via_refinery:
        endpoint: refinery.observability.svc:4317
        insecure: true
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tr := cfg.Output.Honeycomb.Traces
	if tr == nil || tr.ViaRefinery == nil {
		t.Fatal("Output.Honeycomb.Traces.ViaRefinery: nil")
	}
	if tr.ViaRefinery.Endpoint != "refinery.observability.svc:4317" {
		t.Errorf("Endpoint: got %q", tr.ViaRefinery.Endpoint)
	}
	if !tr.ViaRefinery.Insecure {
		t.Errorf("Insecure: got false, want true")
	}
}

func TestValidationError_Format(t *testing.T) {
	err := &ValidationError{
		Issues: []FieldIssue{
			{Path: "output.honeycomb.api_key", Message: "required"},
			{Path: "service_name", Message: "required"},
		},
	}
	got := err.Error()
	if !strings.Contains(got, "service_name") || !strings.Contains(got, "output.honeycomb.api_key") {
		t.Errorf("Error() should mention both paths; got: %q", got)
	}
	// Alphabetical order: "output.honeycomb.api_key" (o) comes before
	// "service_name" (s). Issues are passed in alphabetical order in this
	// test so we can detect any sort regression independent of input order.
	i := strings.Index(got, "output.honeycomb.api_key")
	j := strings.Index(got, "service_name")
	if i == -1 || j == -1 {
		t.Fatalf("missing expected path in: %q", got)
	}
	if i > j {
		t.Errorf("Error() paths should be sorted alphabetically; got: %q", got)
	}
}

// OBI defaults to on for the k8s profile and off everywhere else
// (ADR-0020 sub-decision 4). After applyDefaults the struct is
// always materialized with a concrete bool so the expander never
// has to branch on profile mode itself.
func TestParse_OBIDefaultOnK8sProfile(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: prod
profile:
  mode: k8s
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OBI == nil {
		t.Fatal("OBI: nil after applyDefaults; want materialized struct")
	}
	if cfg.OBI.Enabled == nil {
		t.Fatal("OBI.Enabled: nil after applyDefaults; want concrete bool")
	}
	if !*cfg.OBI.Enabled {
		t.Errorf("OBI.Enabled: got false on k8s profile; want true (default-on per ADR-0020)")
	}
}

func TestParse_OBIDefaultOffOnLinuxProfile(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: prod
profile:
  mode: linux
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OBI == nil || cfg.OBI.Enabled == nil {
		t.Fatal("OBI struct/Enabled should be materialized by applyDefaults")
	}
	if *cfg.OBI.Enabled {
		t.Errorf("OBI.Enabled: got true on linux profile; want false (default-off per ADR-0020)")
	}
}

// Operators can flip OBI off explicitly on k8s and that wins over the
// default-on. Distinguishing "field omitted" from "set to false" is
// the whole reason Enabled is a *bool.
func TestParse_OBIExplicitOffOnK8sOverridesDefault(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: prod
profile:
  mode: k8s
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
obi:
  enabled: false
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OBI == nil || cfg.OBI.Enabled == nil {
		t.Fatal("OBI/Enabled: nil")
	}
	if *cfg.OBI.Enabled {
		t.Errorf("explicit obi.enabled: false should win over k8s default; got true")
	}
}

// OBI is Linux-only by upstream design; the validator rejects
// obi.enabled: true on darwin / windows / none with a remediation
// message that names the offending profile mode.
func TestParse_OBIEnabledOnNonLinuxProfileRejected(t *testing.T) {
	tests := []string{"darwin", "windows", "none"}
	for _, mode := range tests {
		t.Run(mode, func(t *testing.T) {
			yaml := `
service_name: demo
deployment_environment: prod
profile:
  mode: ` + mode + `
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
obi:
  enabled: true
`
			_, err := Parse(strings.NewReader(yaml))
			if err == nil {
				t.Fatalf("Parse: want validation error for obi.enabled: true on profile.mode=%s", mode)
			}
			if !strings.Contains(err.Error(), "obi.enabled") {
				t.Errorf("error should reference obi.enabled; got: %v", err)
			}
			if !strings.Contains(err.Error(), "Linux-only") {
				t.Errorf("error should explain OBI is Linux-only; got: %v", err)
			}
		})
	}
}

// obi.replace_span_metrics_connector: true while obi.enabled: false is
// a configuration the operator clearly didn't intend — the toggle
// would have no effect. Surface eagerly rather than letting it drift.
func TestParse_OBIReplaceConnectorRequiresEnabled(t *testing.T) {
	const yaml = `
service_name: demo
deployment_environment: prod
profile:
  mode: linux
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
obi:
  enabled: false
  replace_span_metrics_connector: true
`
	_, err := Parse(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("Parse: want validation error for replace_span_metrics_connector while OBI is disabled")
	}
	if !strings.Contains(err.Error(), "obi.replace_span_metrics_connector") {
		t.Errorf("error should reference obi.replace_span_metrics_connector; got: %v", err)
	}
}

// Profile-default service.name fill-in covers ADR-0021's primary defaulting
// path: when the operator omits service_name, applyDefaults supplies a
// platform-shaped default keyed off the resolved profile. Boards under
// dashboards/ rely on these defaults to hardcode log-panel datasets.
func TestParse_ServiceNameProfileDefault(t *testing.T) {
	cases := []struct {
		profile string
		want    string
	}{
		{"linux", "linux-host"},
		{"darwin", "macos-host"},
		{"windows", "windows-host"},
		{"docker", "docker-host"},
		{"k8s", "k8s-cluster"},
	}
	for _, tc := range cases {
		t.Run(tc.profile, func(t *testing.T) {
			yaml := `
deployment_environment: prod
profile:
  mode: ` + tc.profile + `
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
`
			cfg, err := Parse(strings.NewReader(yaml))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cfg.ServiceName != tc.want {
				t.Errorf("ServiceName: got %q, want %q", cfg.ServiceName, tc.want)
			}
		})
	}
}

// Explicit service_name in conduit.yaml always wins over the profile default.
func TestParse_ServiceNameExplicitWinsOverProfileDefault(t *testing.T) {
	const yaml = `
service_name: my-edge-gateway
deployment_environment: prod
profile:
  mode: linux
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.ServiceName != "my-edge-gateway" {
		t.Errorf("ServiceName: got %q, want %q (explicit must win over profile default)", cfg.ServiceName, "my-edge-gateway")
	}
}

// profile.mode=none disables the profile-default fill-in, so service_name
// is still required.
func TestParse_ServiceNameRequiredOnModeNone(t *testing.T) {
	const yaml = `
deployment_environment: prod
profile:
  mode: none
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
`
	_, err := Parse(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("Parse: want validation error for missing service_name on profile.mode=none")
	}
	if !strings.Contains(err.Error(), "service_name") {
		t.Errorf("error should reference service_name; got: %v", err)
	}
}
