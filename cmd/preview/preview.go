// Package preview implements `conduit preview`. In M2.B the command renders
// the upstream OTel YAML; --probe / --dimensions / --no-color land in M11.
package preview

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conduit-obs/conduit-agent/internal/config"
	"github.com/conduit-obs/conduit-agent/internal/expander"
)

const long = `Preview what Conduit will hand to its embedded OpenTelemetry Collector for
a given conduit.yaml.

Implemented today (M2.B):
  conduit preview -c PATH    load PATH, validate, and print the rendered
                             upstream OTel Collector YAML to stdout

Pending (M11):
  --probe                    probe Honeycomb / gateway endpoints
  --dimensions               print RED dimensions and cardinality projections
  --no-color                 disable color in human output`

// NewCommand returns the conduit preview command.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Preview the rendered collector config (M2.B); dimensions and probe land in M11",
		Long:  long,
		RunE:  run,
	}

	cmd.Flags().StringP("config", "c", "/etc/conduit/conduit.yaml", "path to conduit.yaml")
	cmd.Flags().Bool("probe", false, "probe Honeycomb / gateway endpoints during preview (not implemented; lands in M11)")
	cmd.Flags().Bool("dimensions", false, "show RED dimensions and cardinality projections only (not implemented; lands in M11)")
	cmd.Flags().Bool("no-color", false, "disable color in human output (not implemented; lands in M11)")

	return cmd
}

func run(cmd *cobra.Command, _ []string) error {
	for _, name := range []string{"probe", "dimensions"} {
		if v, _ := cmd.Flags().GetBool(name); v {
			return fmt.Errorf("preview: --%s is not implemented yet; lands in M11", name)
		}
	}

	path, _ := cmd.Flags().GetString("config")
	if path == "" {
		return errors.New("preview: --config (-c) is required")
	}

	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	configs, err := expander.ExpandConfigsWithWarnings(cfg, cmd.ErrOrStderr())
	if err != nil {
		return fmt.Errorf("preview: %w", err)
	}

	// Print a short RED-dimension projection alongside any profile
	// fallback warnings so operators see — at a glance — which
	// dimensions span_metrics will tag every derived RED metric
	// with. Stderr (not stdout) so a `conduit preview > out.yaml`
	// pipeline still produces a clean YAML file. Skipped when RED
	// is disabled to keep the inspection surface honest.
	printREDProjection(cmd.ErrOrStderr(), cfg)

	// Single config source: print it as one document (preserves the M2.B
	// behavior — diff-friendly, drop-in to a manual `otelcol --config`).
	out := cmd.OutOrStdout()
	if len(configs) == 1 {
		_, err = fmt.Fprint(out, configs[0])
		return err
	}
	// Multi-source: separate the base from each overrides document with
	// "---\n# overrides (merged at runtime by the embedded collector)\n"
	// so the operator can see exactly what is layering on top of what.
	// The collector itself reads these as separate config URIs and
	// deep-merges; the separator here is purely for human inspection.
	if _, err = fmt.Fprint(out, configs[0]); err != nil {
		return err
	}
	for _, body := range configs[1:] {
		if _, err = fmt.Fprint(out, "---\n# overrides (merged at runtime by the embedded collector; see ADR-0012)\n"); err != nil {
			return err
		}
		if _, err = fmt.Fprint(out, body); err != nil {
			return err
		}
	}
	return nil
}

// printREDProjection writes a short, human-readable summary of the
// span_metrics connector's effective dimension set + cardinality cap
// to w. Always called after Load() — so cfg has been through
// applyDefaults — and only emits when RED is enabled (the disabled-
// mode case has nothing useful to say).
//
// The intent is to give operators a "what will this cost me at query
// time" preview without forcing them to grep the rendered YAML for
// span_metrics:. M11's `conduit doctor` will surface the same set
// alongside its CDT0510 cardinality projections; the wording stays
// stable here so doctor output matches.
func printREDProjection(w io.Writer, cfg *config.AgentConfig) {
	if cfg == nil || cfg.Metrics == nil || cfg.Metrics.RED == nil {
		return
	}
	red := cfg.Metrics.RED
	if !red.REDEnabled() {
		fmt.Fprintln(w, "conduit: RED metrics from spans: disabled (metrics.red.enabled=false)")
		return
	}
	spanDims := append(append([]string{}, config.REDDefaultSpanDimensions...), red.SpanDimensions...)
	resDims := append(append([]string{}, config.REDDefaultResourceDimensions...), red.ExtraResourceDimensions...)
	limit := red.CardinalityLimit
	if limit == 0 {
		limit = config.DefaultREDCardinalityLimit
	}
	fmt.Fprintf(w, "conduit: RED metrics from spans: enabled (cardinality_limit=%d, span dims=%d, resource dims=%d)\n",
		limit, len(spanDims), len(resDims))
	fmt.Fprintf(w, "  span dimensions:     [%s]\n", strings.Join(spanDims, ", "))
	fmt.Fprintf(w, "  resource dimensions: [%s]\n", strings.Join(resDims, ", "))
}
