// Package config implements `conduit config`. In M2.B this command wires the
// --validate flag against internal/config; --print-schema is still pending.
package config

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	cfg "github.com/conduit-obs/conduit-agent/internal/config"
)

const long = `Validate a conduit.yaml against the Conduit schema or print the JSON
Schema itself for IDE / CI integration.

Implemented today (M2.B):
  conduit config --validate -c PATH   load PATH, apply defaults, validate,
                                      and print "valid" or a structured list
                                      of every problem found

Pending (later milestone):
  conduit config --print-schema       emit the JSON Schema for conduit.yaml`

// NewCommand returns the conduit config command.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Validate or inspect Conduit configuration",
		Long:  long,
		RunE:  run,
	}

	cmd.Flags().Bool("validate", false, "validate the given conduit.yaml against the schema")
	cmd.Flags().Bool("print-schema", false, "print the JSON Schema for conduit.yaml to stdout (not implemented yet)")
	cmd.Flags().StringP("config", "c", "/etc/conduit/conduit.yaml", "path to conduit.yaml when --validate is set")

	return cmd
}

func run(cmd *cobra.Command, _ []string) error {
	validate, _ := cmd.Flags().GetBool("validate")
	printSchema, _ := cmd.Flags().GetBool("print-schema")

	switch {
	case validate && printSchema:
		return errors.New("config: --validate and --print-schema are mutually exclusive")
	case printSchema:
		return errors.New("config: --print-schema is not implemented yet")
	case validate:
		path, _ := cmd.Flags().GetString("config")
		if _, err := cfg.Load(path); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: valid\n", path)
		return nil
	default:
		return errors.New("config: pass --validate (or --print-schema once implemented); see --help")
	}
}
