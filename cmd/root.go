// Package cmd assembles the conduit Cobra CLI. Some subcommands are fully
// wired (run, preview, config); others are still M1 stubs that exit
// non-zero with "not implemented" (doctor, version, send-test-data) and
// land in later milestones.
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/conduit-obs/conduit-agent/cmd/config"
	"github.com/conduit-obs/conduit-agent/cmd/doctor"
	"github.com/conduit-obs/conduit-agent/cmd/preview"
	"github.com/conduit-obs/conduit-agent/cmd/run"
	"github.com/conduit-obs/conduit-agent/cmd/sendtestdata"
	"github.com/conduit-obs/conduit-agent/cmd/version"
)

const rootLong = `Conduit is an opinionated, Honeycomb-ready, OpenTelemetry-native agent
distribution that closes the enterprise observability familiarity gap for
Honeycomb adoption.

Status: pre-alpha (Milestone M2). 'conduit run', 'conduit preview', and
'conduit config' are wired end-to-end. 'conduit doctor', 'conduit version',
and 'conduit send-test-data' are still stubs that exit non-zero with "not
implemented" and land in later milestones.`

// NewRootCommand returns the conduit root command with every V0 subcommand
// attached.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "conduit",
		Short:         "Honeycomb-ready OpenTelemetry agent distribution",
		Long:          rootLong,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.AddCommand(
		run.NewCommand(),
		doctor.NewCommand(),
		preview.NewCommand(),
		config.NewCommand(),
		version.NewCommand(),
		sendtestdata.NewCommand(),
	)

	return root
}
