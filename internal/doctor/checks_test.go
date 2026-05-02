package doctor

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conduit-obs/conduit-agent/internal/config"
)

// helper: build a valid honeycomb-mode AgentConfig for tests so each
// check sees a canonical Config without requiring on-disk files.
func validHoneycombConfig() *config.AgentConfig {
	cfg := &config.AgentConfig{
		ServiceName:           "doctor-test",
		DeploymentEnvironment: "ci",
		Output: config.Output{
			Mode: config.OutputModeHoneycomb,
			Honeycomb: &config.HoneycombOutput{
				APIKey:   "${env:KEY}",
				Endpoint: config.DefaultHoneycombEndpoint,
			},
		},
		Profile: &config.Profile{Mode: config.ProfileModeNone},
	}
	if err := cfg.Validate(); err != nil {
		panic(err)
	}
	return cfg
}

// CDT0001: a clean Config + no ConfigErr returns a single PASS.
func TestCheckConfigSyntax_Pass(t *testing.T) {
	results := CheckConfigSyntax(Context{ConfigPath: "x.yaml", Config: validHoneycombConfig()})
	if len(results) != 1 || results[0].Severity != SeverityPass || results[0].ID != cdt0001ID {
		t.Errorf("want 1 PASS CDT0001 result; got %+v", results)
	}
	if results[0].DocsURL == "" {
		t.Errorf("PASS result must carry a docs URL; got %+v", results[0])
	}
}

// CDT0001: a parse failure (non-validation error) surfaces a single
// FAIL with the raw error message embedded.
func TestCheckConfigSyntax_FailOnParseError(t *testing.T) {
	results := CheckConfigSyntax(Context{
		ConfigPath: "x.yaml",
		ConfigErr:  &someParseError{msg: "yaml line 3: mapping values are not allowed in this context"},
	})
	if len(results) != 1 || results[0].Severity != SeverityFail {
		t.Fatalf("want 1 FAIL result; got %+v", results)
	}
	if !strings.Contains(results[0].Message, "yaml line 3") {
		t.Errorf("Message should embed the parse error; got %q", results[0].Message)
	}
}

// CDT0001 / CDT0501: a ValidationError with multiple issues surfaces
// one Result per issue, with cardinality denylist hits anchored at
// CDT0501 instead of CDT0001.
func TestCheckConfigSyntax_FailWithStructuredIssues(t *testing.T) {
	verr := &config.ValidationError{
		Issues: []config.FieldIssue{
			{Path: "service_name", Message: "required; non-empty string"},
			{Path: "metrics.red.span_dimensions[0]", Message: `"trace_id" is on the cardinality denylist (CDT0501): explodes per request. See ADR-0006.`},
		},
	}
	results := CheckConfigSyntax(Context{ConfigPath: "x.yaml", ConfigErr: verr})
	if len(results) != 2 {
		t.Fatalf("want 2 results (one per issue); got %d: %+v", len(results), results)
	}
	var sawCDT0001, sawCDT0501 bool
	for _, r := range results {
		if r.Severity != SeverityFail {
			t.Errorf("issue results should all be FAIL; got %+v", r)
		}
		switch r.ID {
		case cdt0001ID:
			sawCDT0001 = true
		case cdt0501ID:
			sawCDT0501 = true
		}
	}
	if !sawCDT0001 || !sawCDT0501 {
		t.Errorf("want both CDT0001 and CDT0501 IDs; got %+v", results)
	}
}

type someParseError struct{ msg string }

func (e *someParseError) Error() string { return e.msg }

// CDT0102: the M10 default config (api_key: ${env:KEY}) passes auth
// because the ${env:NAME} placeholder is the documented operator
// pattern (the embedded collector resolves it at startup).
func TestCheckOutputAuth_EnvVarPlaceholderPasses(t *testing.T) {
	results := CheckOutputAuth(Context{Config: validHoneycombConfig()})
	if len(results) != 1 || results[0].Severity != SeverityPass || results[0].ID != cdt0102ID {
		t.Fatalf("want 1 PASS CDT0102; got %+v", results)
	}
	if !strings.Contains(results[0].Message, "env var") {
		t.Errorf("Message should mention env var resolution; got %q", results[0].Message)
	}
}

// CDT0102: an empty api_key fails (the schema validator catches this
// at parse time, but doctor surfaces it again because a hand-built
// struct could slip past).
func TestCheckOutputAuth_EmptyAPIKeyFails(t *testing.T) {
	cfg := validHoneycombConfig()
	cfg.Output.Honeycomb.APIKey = ""
	results := CheckOutputAuth(Context{Config: cfg})
	if len(results) != 1 || results[0].Severity != SeverityFail {
		t.Fatalf("want 1 FAIL; got %+v", results)
	}
}

// CDT0102: a literal API key (not ${env:NAME}) passes with a length
// in the message but never logs the key itself (NFR-3).
func TestCheckOutputAuth_LiteralKeyPassesAndDoesNotLogKey(t *testing.T) {
	cfg := validHoneycombConfig()
	cfg.Output.Honeycomb.APIKey = "supersecret-very-long-honeycomb-key"
	results := CheckOutputAuth(Context{Config: cfg})
	if len(results) != 1 || results[0].Severity != SeverityPass {
		t.Fatalf("want 1 PASS; got %+v", results)
	}
	if strings.Contains(results[0].Message, "supersecret") {
		t.Fatalf("doctor must never log the API key; got %q", results[0].Message)
	}
}

// CDT0103: an insecure: true override on a gateway exporter surfaces
// a Warn even when no probe ran.
func TestCheckOutputTLS_GatewayInsecureWarns(t *testing.T) {
	cfg := &config.AgentConfig{
		ServiceName:           "x",
		DeploymentEnvironment: "ci",
		Output: config.Output{
			Mode: config.OutputModeGateway,
			Gateway: &config.GatewayOutput{
				Endpoint: "gateway.internal:4317",
				Insecure: true,
			},
		},
		Profile: &config.Profile{Mode: config.ProfileModeNone},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	results := CheckOutputTLS(Context{Config: cfg})
	if len(results) != 1 || results[0].Severity != SeverityWarn || results[0].ID != cdt0103ID {
		t.Fatalf("want 1 WARN CDT0103; got %+v", results)
	}
	if !strings.Contains(results[0].Message, "TLS verification is disabled") {
		t.Errorf("Warn message should explain disabled TLS; got %q", results[0].Message)
	}
}

// CDT0103: a refinery routing block with insecure: true surfaces the
// same warning shape as gateway insecure (M10.B + M10.C contract).
func TestCheckOutputTLS_RefineryInsecureWarns(t *testing.T) {
	cfg := validHoneycombConfig()
	cfg.Output.Honeycomb.Traces = &config.HoneycombTraces{
		ViaRefinery: &config.RefineryRouting{
			Endpoint: "localhost:4317",
			Insecure: true,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	results := CheckOutputTLS(Context{Config: cfg})
	if len(results) != 1 || results[0].Severity != SeverityWarn {
		t.Fatalf("want 1 WARN; got %+v", results)
	}
}

// CDT0103: TLS-required-by-default returns a single PASS.
func TestCheckOutputTLS_DefaultsPass(t *testing.T) {
	results := CheckOutputTLS(Context{Config: validHoneycombConfig()})
	if len(results) != 1 || results[0].Severity != SeverityPass {
		t.Fatalf("want 1 PASS; got %+v", results)
	}
}

// CDT0201: probing two free ports passes; binding our own listener
// on 4317 forces a FAIL on that port. Run on 127.0.0.1 only so we
// don't conflict with whatever else might be on the host.
func TestCheckReceiverPorts_DetectsHeldPort(t *testing.T) {
	// Bind 127.0.0.1:0 to force a port we know is free, then ask
	// the doctor to probe that exact port. Avoids the 4317/4318
	// hardcoded-default for this targeted unit test by overriding
	// via a test-only path: run probePort directly.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	r := probePort("127.0.0.1", port)
	if r.Severity != SeverityFail {
		t.Errorf("probePort on a held port should FAIL; got %+v", r)
	}
	if !strings.Contains(r.Message, port) {
		t.Errorf("Message should name the port; got %q", r.Message)
	}
}

// CDT0202: filelog include paths that exist + are readable produce a
// summary PASS.
func TestCheckReceiverPermissions_ReadablePathsPass(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	if err := os.WriteFile(logPath, []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rendered := "receivers:\n" +
		"  filelog/app:\n" +
		"    include:\n" +
		"      - " + logPath + "\n"
	results := CheckReceiverPermissions(Context{RenderedYAML: rendered})
	if len(results) != 1 || results[0].Severity != SeverityPass {
		t.Fatalf("want 1 PASS; got %+v", results)
	}
}

// CDT0202: an unreadable filelog path (mode 0000) surfaces a FAIL
// with the path embedded so the operator can fix permissions.
func TestCheckReceiverPermissions_UnreadablePathFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; mode-0000 file is still readable")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "secret.log")
	if err := os.WriteFile(logPath, []byte("nope\n"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(logPath, 0o600) })
	rendered := "receivers:\n" +
		"  filelog/secret:\n" +
		"    include:\n" +
		"      - " + logPath + "\n"
	results := CheckReceiverPermissions(Context{RenderedYAML: rendered})
	var sawFail bool
	for _, r := range results {
		if r.Severity == SeverityFail && strings.Contains(r.Message, logPath) {
			sawFail = true
		}
	}
	if !sawFail {
		t.Errorf("want FAIL mentioning %s; got %+v", logPath, results)
	}
}

// CDT0202: empty rendered YAML skips (preserves the framework
// invariant: skip when prerequisites are missing).
func TestCheckReceiverPermissions_NoRenderSkips(t *testing.T) {
	results := CheckReceiverPermissions(Context{})
	if len(results) != 1 || results[0].Severity != SeveritySkip {
		t.Errorf("want 1 SKIP; got %+v", results)
	}
}

// CDT0403: version compat always reports a single PASS with the
// embedded core version in the message.
func TestCheckVersionCompat_StablePass(t *testing.T) {
	results := CheckVersionCompat(Context{})
	if len(results) != 1 || results[0].Severity != SeverityPass {
		t.Fatalf("want 1 PASS; got %+v", results)
	}
	if !strings.Contains(results[0].Message, "otelcol-core") {
		t.Errorf("Message should mention otelcol-core; got %q", results[0].Message)
	}
}

// extractFilelogPaths parses the rendered YAML's filelog include
// lists. Verify it walks both single- and multi-receiver blocks
// and survives non-filelog content interleaved in the doc.
func TestExtractFilelogPaths_HandlesMultipleBlocks(t *testing.T) {
	rendered := strings.Join([]string{
		"receivers:",
		"  otlp:",
		"    protocols: {grpc: {}}",
		"  filelog/system:",
		"    include:",
		"      - /var/log/syslog",
		"      - /var/log/messages",
		"    start_at: end",
		"  filelog/app:",
		"    include:",
		"      - /var/log/app/*.log",
		"  hostmetrics:",
		"    scrapers: {}",
	}, "\n")
	got := extractFilelogPaths(rendered)
	want := []string{"/var/log/syslog", "/var/log/messages", "/var/log/app/*.log"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("paths[%d]: got %q, want %q", i, got[i], w)
		}
	}
}

// CDT0101: nil Config skips with a "CDT0001 must pass first"
// reason. Network-positive cases live in the M12 E2E matrix; here
// we want stable unit-test behavior independent of the host's DNS.
func TestCheckOutputEndpoint_NilConfigSkips(t *testing.T) {
	results := CheckOutputEndpoint(Context{Ctx: context.Background()})
	if len(results) != 1 || results[0].Severity != SeveritySkip {
		t.Errorf("want 1 SKIP; got %+v", results)
	}
}

// CDT0101: an env-referencing endpoint also skips (we can't dial what
// the embedded collector hasn't yet resolved).
func TestCheckOutputEndpoint_EnvEndpointSkips(t *testing.T) {
	cfg := validHoneycombConfig()
	cfg.Output.Honeycomb.Endpoint = "${env:HONEYCOMB_API_URL}"
	results := CheckOutputEndpoint(Context{Ctx: context.Background(), Config: cfg})
	if len(results) != 1 || results[0].Severity != SeveritySkip {
		t.Errorf("want 1 SKIP; got %+v", results)
	}
}
