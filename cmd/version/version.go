// Package version implements `conduit version`, printing the build
// metadata the release pipeline injects via -ldflags. The same values back
// the root command's `--version` flag (see cmd/root.go).
package version

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build metadata injected at link time via
//
//	-X github.com/conduit-obs/conduit-agent/cmd/version.version=...
//	-X github.com/conduit-obs/conduit-agent/cmd/version.commit=...
//	-X github.com/conduit-obs/conduit-agent/cmd/version.date=...
//
// (wired in .goreleaser.yaml builds.ldflags and the Makefile `build`
// target). The defaults keep an un-stamped `go build` honest: it reports a
// dev build rather than masquerading as a tagged release.
var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

// otelCollectorVersion is the pinned upstream OpenTelemetry Collector
// (core + contrib) MINOR compiled into this binary. It is a property of the
// source tree (go.mod / builder-config.yaml), not the build invocation, so
// it lives in code rather than an ldflag. Bump it in lockstep with
// builder-config.yaml and the Makefile OCB_VERSION (ADR-0014).
const otelCollectorVersion = "0.151.0"

// Version returns the Conduit version string (e.g. "1.2.3" or "0.0.0-dev").
// Exported so the root command can surface it via `conduit --version`
// without duplicating the ldflag plumbing.
func Version() string { return version }

const long = `Print the Conduit version, the pinned upstream OpenTelemetry Collector
version, the build date, and the git commit.

Use --short to print only the Conduit version string (useful in scripts and
release automation).`

// NewCommand returns the `conduit version` command.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print Conduit version information",
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if short, _ := cmd.Flags().GetBool("short"); short {
				_, _ = fmt.Fprintln(out, version)
				return nil
			}
			_, _ = fmt.Fprintf(out, "conduit %s\n", version)
			_, _ = fmt.Fprintf(out, "  commit:         %s\n", commit)
			_, _ = fmt.Fprintf(out, "  built:          %s\n", date)
			_, _ = fmt.Fprintf(out, "  otel collector: %s\n", otelCollectorVersion)
			_, _ = fmt.Fprintf(out, "  go:             %s\n", runtime.Version())
			_, _ = fmt.Fprintf(out, "  platform:       %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}

	cmd.Flags().Bool("short", false, "print only the Conduit version string")

	return cmd
}
