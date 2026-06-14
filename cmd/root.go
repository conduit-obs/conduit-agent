// Package cmd assembles the conduit Cobra CLI. Every subcommand (run,
// preview, config, doctor, version, send-test-data) is wired end-to-end.
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

Status: pre-alpha. 'conduit run', 'conduit preview', 'conduit config',
'conduit doctor', 'conduit version', and 'conduit send-test-data' are all
wired end-to-end.`

// NewRootCommand returns the conduit root command with every V0 subcommand
// attached.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "conduit",
		Short: "Honeycomb-ready OpenTelemetry agent distribution",
		Long:  rootLong,
		// Setting Version enables `conduit --version`. The detailed
		// `conduit version` subcommand stays the canonical, scriptable
		// source of build metadata; this is the convenience flag.
		Version:       version.Version(),
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
