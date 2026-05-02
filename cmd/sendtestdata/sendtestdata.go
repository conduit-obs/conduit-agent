// Package sendtestdata implements the M1 stub for `conduit send-test-data`.
// Real behavior lands alongside M2/M8/M10 work.
package sendtestdata

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/conduit-obs/conduit-agent/cmd/internal/stub"
)

const long = `Generate synthetic OTLP traces, metrics, and logs that match Conduit's V0
RED dimension model and resource enrichment expectations.

Used by conduit doctor smoke tests, the 30-minute demo, and CI E2E tests
against whichever observability backend the configured output mode
points at (Honeycomb sandboxes today; otlp / gateway destinations once
those code paths land in M10).

In M1 this is a stub. Real behavior lands by M10.`

// NewCommand returns the M1 stub for `conduit send-test-data`.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send-test-data",
		Short: "Emit synthetic OTLP telemetry for smoke tests and demos (stub in M1)",
		Long:  long,
		RunE:  stub.RunE("send-test-data", "M10"),
	}

	cmd.Flags().Int("rate", 10, "events per second (per signal)")
	cmd.Flags().Duration("duration", time.Minute, "how long to send for")
	cmd.Flags().String("target", "127.0.0.1:4318", "OTLP/HTTP endpoint to send to")
	cmd.Flags().String("profile", "default", "test data profile (default|red|host|k8s)")

	return cmd
}
