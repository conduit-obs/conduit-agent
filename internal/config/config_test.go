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

func TestParse_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantPaths []string
	}{
		{
			name: "missing service_name",
			yaml: `
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
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
			name: "all required fields missing",
			yaml: `
output:
  mode: honeycomb
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
