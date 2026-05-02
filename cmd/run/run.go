// Package run implements `conduit run`, which boots the embedded OpenTelemetry
// Collector with the pipeline expanded from conduit.yaml.
//
// Lifecycle dispatch is split across two build-tagged files:
//
//   - run_unix.go (build !windows): SIGINT / SIGTERM cancel a context
//     derived from cobra's, then call into the collector. The Linux
//     systemd unit and the macOS launchd plist both feed into this path.
//
//   - run_windows.go (build windows): if the process was started by the
//     Windows Service Control Manager (svc.IsWindowsService), dispatch
//     to the SCM via svc.Run and translate its Stop / Shutdown commands
//     into a context cancellation; if started interactively (a developer
//     in a console), fall back to Ctrl+C handling. The MSI installer
//     (M6.C) registers `conduit run` as the service ImagePath, so a
//     Windows Service start lands here.
//
// runCollector is the OS-agnostic core that both wrappers call.
package run

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/conduit-obs/conduit-agent/internal/collector"
	"github.com/conduit-obs/conduit-agent/internal/config"
	"github.com/conduit-obs/conduit-agent/internal/expander"
)

// serviceName is the Windows Service Control Manager name the MSI
// registers under (deploy/windows/wix/conduit.wxs §ServiceInstall.Name).
// Reused as the OS-agnostic identifier for log lines emitted from the
// service-lifecycle layer (run_unix.go ignores it; run_windows.go passes
// it to svc.Run).
const serviceName = "conduit"

const long = `Run the Conduit agent with the embedded OpenTelemetry Collector.

The command:
  1. loads conduit.yaml from --config (default /etc/conduit/conduit.yaml)
  2. validates it against the schema in internal/config
  3. expands it into upstream OTel Collector YAML (internal/expander)
  4. starts the embedded collector, blocking until SIGINT / SIGTERM

The collector listens for OTLP on :4317 (gRPC) and :4318 (HTTP) and exports
to whatever destination output.mode selects: the Honeycomb named preset
(honeycomb), generic OTLP/HTTP to any vendor (otlp), or OTLP/gRPC to a
customer-operated gateway (gateway).

Note: --log-level is parsed but not yet wired through to the embedded
collector; that lands in M3 along with the structured logging story.`

// NewCommand returns the conduit run command.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the Conduit agent",
		Long:  long,
		RunE:  runCmd,
	}

	cmd.Flags().StringP("config", "c", "/etc/conduit/conduit.yaml", "path to conduit.yaml")
	cmd.Flags().String("log-level", "info", "log level (debug|info|warn|error); not yet wired to the embedded collector — lands in M3")

	return cmd
}

func runCmd(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("config")
	if path == "" {
		return errors.New("run: --config (-c) is required")
	}

	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	configs, err := expander.ExpandConfigsWithWarnings(cfg, cmd.ErrOrStderr())
	if err != nil {
		return fmt.Errorf("run: expand: %w", err)
	}

	// One URI per rendered config source — the embedded collector
	// deep-merges them in order. With no overrides, this is a single
	// "yaml:..." URI; with overrides, the second URI carries the user's
	// raw escape-hatch block (see ADR-0012 / config.AgentConfig.Overrides).
	uris := make([]string, len(configs))
	for i, body := range configs {
		uris[i] = "yaml:" + body
	}
	settings := collector.DefaultSettings(collector.DefaultBuildInfo)
	settings.ConfigProviderSettings.ResolverSettings.URIs = uris

	return runWithLifecycle(cmd.Context(), serviceName, func(ctx context.Context) error {
		if err := collector.Run(ctx, settings); err != nil {
			return fmt.Errorf("run: collector exited: %w", err)
		}
		return nil
	})
}
