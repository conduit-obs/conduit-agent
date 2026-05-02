// Package doctor wires `conduit doctor` to the internal/doctor check
// catalog (M11). The command's own surface is intentionally thin —
// load the config, run the catalog, render — because every interesting
// bit of behavior lives in the internal/doctor package so tests can
// drive it without spinning up cobra.
package doctor

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/conduit-obs/conduit-agent/internal/config"
	idoctor "github.com/conduit-obs/conduit-agent/internal/doctor"
	"github.com/conduit-obs/conduit-agent/internal/expander"
)

const long = `Run structured diagnostic checks against a Conduit installation.

Each check has a stable ID (CDT0xxx), a human-readable message, a docs anchor,
and a JSON-renderable result. Doctor exits non-zero when any check fails so
operators can wire it into deployment pipelines (kubectl exec post-rollout
gates, systemd healthchecks, etc.).

Implemented checks (M11):
  CDT0001  config.syntax              conduit.yaml parses against schema
  CDT0501  config.cardinality         RED dimension denylist warnings
  CDT0101  output.endpoint_reachable  TCP+TLS handshake to the active output
  CDT0102  output.auth                auth headers / API keys are non-empty
  CDT0103  output.tls_warning         insecure: true overrides surface
  CDT0201  receiver.ports             OTLP 4317/4318 are free to bind
  CDT0202  receiver.permissions       filelog include paths are readable
  CDT0403  version.compat             conduit ↔ otelcol-core support window

Use --check to filter (e.g. ` + "`--check output.endpoint_reachable`" + ` or
` + "`--check output`" + ` to run every output.* check). Use --json for
machine-readable output suitable for jq pipelines and CI gates.`

// NewCommand returns the conduit doctor command.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run structured diagnostic checks (M11)",
		Long:  long,
		RunE:  run,
	}

	cmd.Flags().Bool("json", false, "emit machine-readable JSON output")
	cmd.Flags().Duration("since", time.Duration(0), "look back over this duration of running state (reserved for queue.health / memory.pressure; not used by V0 checks)")
	cmd.Flags().StringSlice("check", nil, "run only the named checks (repeatable; matches CDT IDs, full titles like output.endpoint_reachable, or title prefixes like output)")
	cmd.Flags().StringP("config", "c", "/etc/conduit/conduit.yaml", "path to conduit.yaml")

	return cmd
}

func run(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("config")
	if strings.TrimSpace(path) == "" {
		return errors.New("doctor: --config (-c) is required")
	}
	jsonOut, _ := cmd.Flags().GetBool("json")
	filter, _ := cmd.Flags().GetStringSlice("check")

	bctx := idoctor.Context{ConfigPath: path}

	// Load the config but DON'T short-circuit on failure: doctor's
	// whole job is to give a structured account of what's wrong, and
	// the CDT0001 check needs the parse error in hand to do that.
	cfg, err := config.Load(path)
	if err != nil {
		bctx.ConfigErr = err
	} else {
		bctx.Config = cfg
		// Render is best-effort; a render failure on a Validate-clean
		// config would be an internal bug, but we don't crash the
		// doctor over it — receiver.permissions falls back to SKIP.
		if rendered, rerr := expander.Expand(cfg); rerr == nil {
			bctx.RenderedYAML = rendered
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"doctor: warning: render failed (%v); receiver.permissions will be skipped.\n", rerr)
		}
	}

	failed, err := idoctor.Run(cmd.Context(), idoctor.DefaultChecks(), bctx, cmd.OutOrStdout(),
		idoctor.Options{
			JSON:   jsonOut,
			Filter: filter,
		})
	if err != nil {
		return fmt.Errorf("doctor: render report: %w", err)
	}
	if failed {
		// SilenceUsage so cobra doesn't dump the help screen on top
		// of the report; SilenceErrors so we don't double-print.
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return errors.New("doctor: one or more checks failed")
	}
	return nil
}
