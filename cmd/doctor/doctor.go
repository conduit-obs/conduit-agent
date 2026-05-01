// Package doctor implements the M1 stub for `conduit doctor`. Real behavior lands in M11.
package doctor

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/conduit-obs/conduit-agent/cmd/internal/stub"
)

const long = `Run structured diagnostic checks against a Conduit installation.

Each check has a stable ID (CDT0xxx), a human-readable message, a docs anchor,
and a JSON-renderable result. Checks include config validity, output endpoint
reachability, auth header format, OTLP receiver port availability, filelog
permissions, k8s RBAC, queue health, memory pressure, and Conduit-vs-upstream
collector compatibility.

In M1 this is a stub. Real behavior lands in M11.`

// NewCommand returns the M1 stub for `conduit doctor`.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostic checks (stub in M1)",
		Long:  long,
		RunE:  stub.RunE("doctor", "M11"),
	}

	cmd.Flags().Bool("json", false, "emit machine-readable JSON output")
	cmd.Flags().Duration("since", time.Duration(0), "look back over this duration of running state (e.g., 1h)")
	cmd.Flags().StringSlice("check", nil, "run only the named checks (repeatable)")
	cmd.Flags().StringP("config", "c", "/etc/conduit/conduit.yaml", "path to conduit.yaml")

	return cmd
}
