package cmd_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conduit-obs/conduit-agent/cmd"
	"github.com/conduit-obs/conduit-agent/cmd/internal/stub"
)

// allSubcommands lists every subcommand exposed by the root command. Used for
// tests that don't care whether the subcommand is fully implemented or still
// a stub (registration, --help, etc.).
var allSubcommands = []string{
	"run",
	"doctor",
	"preview",
	"config",
	"version",
	"send-test-data",
}

// stubSubcommands is the subset that still returns *stub.NotImplementedError
// when invoked without arguments. Cross-reference with the milestone plan as
// commands are wired up:
//
//   - version         stub until M3 (ldflag injection)
//   - send-test-data  stub until M11
var stubSubcommands = []string{
	"version",
	"send-test-data",
}

func TestRootHelp(t *testing.T) {
	root := cmd.NewRootCommand()
	root.SetArgs([]string{"--help"})

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	if err := root.Execute(); err != nil {
		t.Fatalf("--help should succeed, got %v", err)
	}
	if !strings.Contains(stdout.String(), "conduit") {
		t.Errorf("--help output should mention conduit, got %q", stdout.String())
	}
}

func TestSubcommandsAreRegistered(t *testing.T) {
	root := cmd.NewRootCommand()
	for _, name := range allSubcommands {
		t.Run(name, func(t *testing.T) {
			c, _, err := root.Find([]string{name})
			if err != nil {
				t.Fatalf("subcommand %q not found: %v", name, err)
			}
			if c.Use == "" {
				t.Errorf("subcommand %q has empty Use field", name)
			}
			if c.Short == "" {
				t.Errorf("subcommand %q has empty Short field", name)
			}
		})
	}
}

func TestStubSubcommandsReturnNotImplemented(t *testing.T) {
	for _, name := range stubSubcommands {
		t.Run(name, func(t *testing.T) {
			root := cmd.NewRootCommand()
			root.SetArgs([]string{name})

			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			err := root.Execute()
			if err == nil {
				t.Fatalf("subcommand %q should return a not-implemented error", name)
			}
			var nie *stub.NotImplementedError
			if !errors.As(err, &nie) {
				t.Fatalf("subcommand %q should return *stub.NotImplementedError, got %T: %v", name, err, err)
			}
			if !strings.Contains(stderr.String(), "not implemented") {
				t.Errorf("subcommand %q stderr should contain 'not implemented', got %q", name, stderr.String())
			}
		})
	}
}

func TestSubcommandHelpExitsZero(t *testing.T) {
	for _, name := range allSubcommands {
		t.Run(name, func(t *testing.T) {
			root := cmd.NewRootCommand()
			root.SetArgs([]string{name, "--help"})

			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			if err := root.Execute(); err != nil {
				t.Fatalf("%q --help should succeed, got %v", name, err)
			}
		})
	}
}

// TestConfigValidate_HappyPath drives `conduit config --validate -c PATH` end
// to end with a minimal valid conduit.yaml.
func TestConfigValidate_HappyPath(t *testing.T) {
	path := writeYAML(t, `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
`)

	root := cmd.NewRootCommand()
	root.SetArgs([]string{"config", "--validate", "-c", path})

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	if err := root.Execute(); err != nil {
		t.Fatalf("config --validate: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "valid") {
		t.Errorf("expected stdout to contain 'valid'; got %q", stdout.String())
	}
}

// TestConfigValidate_FailsLoud verifies a malformed conduit.yaml returns a
// non-zero error mentioning the problem field.
func TestConfigValidate_FailsLoud(t *testing.T) {
	path := writeYAML(t, `
service_name: demo
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
`)

	root := cmd.NewRootCommand()
	root.SetArgs([]string{"config", "--validate", "-c", path})

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	err := root.Execute()
	if err == nil {
		t.Fatal("expected validation error for missing deployment_environment")
	}
	if !strings.Contains(err.Error(), "deployment_environment") {
		t.Errorf("error should mention deployment_environment; got %v", err)
	}
}

// TestPreview_RendersYAML verifies `conduit preview -c PATH` prints rendered
// upstream OTel Collector YAML to stdout.
func TestPreview_RendersYAML(t *testing.T) {
	path := writeYAML(t, `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:KEY}
`)

	root := cmd.NewRootCommand()
	root.SetArgs([]string{"preview", "-c", path})

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	if err := root.Execute(); err != nil {
		t.Fatalf("preview: %v\nstderr: %s", err, stderr.String())
	}
	for _, want := range []string{"receivers:", "otlp:", "otlphttp/honeycomb:", "service:", "pipelines:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("preview output missing %q; got:\n%s", want, stdout.String())
		}
	}
}

// TestRun_MissingConfig confirms `conduit run -c PATH` returns a clear error
// when PATH does not exist, rather than silently starting the collector with
// an empty config or panicking. Exhaustive run-time tests live in
// internal/collector and are gated to the M2.C smoke test.
func TestRun_MissingConfig(t *testing.T) {
	root := cmd.NewRootCommand()
	root.SetArgs([]string{"run", "-c", "/nonexistent/conduit.yaml"})

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
	if !strings.Contains(err.Error(), "open conduit config") {
		t.Errorf("expected 'open conduit config' in error; got %v", err)
	}
}

// TestPreview_DeferredFlagsRejected ensures --probe and --dimensions exit
// non-zero with a clear "not implemented yet" message until M11 lands.
func TestPreview_DeferredFlagsRejected(t *testing.T) {
	path := writeYAML(t, `
service_name: demo
deployment_environment: dev
output:
  mode: honeycomb
  honeycomb:
    api_key: x
`)

	for _, flag := range []string{"--probe", "--dimensions"} {
		t.Run(flag, func(t *testing.T) {
			root := cmd.NewRootCommand()
			root.SetArgs([]string{"preview", "-c", path, flag})

			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			err := root.Execute()
			if err == nil {
				t.Fatalf("preview %s should return a not-implemented error", flag)
			}
			if !strings.Contains(err.Error(), "not implemented") {
				t.Errorf("preview %s error should say 'not implemented'; got %v", flag, err)
			}
		})
	}
}

func writeYAML(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "conduit.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write tmp config: %v", err)
	}
	return path
}
