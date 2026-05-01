// Package version implements the M1 stub for `conduit version`. Real behavior
// lands when the build pipeline starts injecting version metadata via ldflags.
package version

import (
	"github.com/spf13/cobra"

	"github.com/conduit-obs/conduit-agent/cmd/internal/stub"
)

const long = `Print the Conduit version, the pinned upstream OpenTelemetry Collector
version, the build date, and the git SHA.

In M1 this is a stub; the binary carries no version metadata yet. Real
behavior lands when the release pipeline starts injecting version values
via Go ldflags.`

// NewCommand returns the M1 stub for `conduit version`.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print Conduit version information (stub in M1)",
		Long:  long,
		RunE:  stub.RunE("version", "M1+"),
	}

	cmd.Flags().Bool("short", false, "print only the Conduit version string")

	return cmd
}
