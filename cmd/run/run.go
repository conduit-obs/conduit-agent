// Package run implements `conduit run`, which boots the embedded OpenTelemetry
// Collector with the pipeline expanded from conduit.yaml.
package run

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/conduit-obs/conduit-agent/internal/collector"
	"github.com/conduit-obs/conduit-agent/internal/config"
	"github.com/conduit-obs/conduit-agent/internal/expander"
)

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

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := collector.Run(ctx, settings); err != nil {
		return fmt.Errorf("run: collector exited: %w", err)
	}
	return nil
}
