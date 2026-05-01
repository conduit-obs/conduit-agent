// Package preview implements `conduit preview`. In M2.B the command renders
// the upstream OTel YAML; --probe / --dimensions / --no-color land in M11.
package preview

import (
	"errors"
	"fmt"

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
  --no-color                 disable color in human output

See conduit-agent-plan/01-product-requirements.md FR-8 §"conduit preview".`

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

	yaml, err := expander.ExpandWithWarnings(cfg, cmd.ErrOrStderr())
	if err != nil {
		return fmt.Errorf("preview: %w", err)
	}

	_, err = fmt.Fprint(cmd.OutOrStdout(), yaml)
	return err
}
