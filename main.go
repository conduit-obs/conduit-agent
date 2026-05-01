package main

import (
	"os"

	"github.com/conduit-obs/conduit-agent/cmd"
)

func main() {
	// Cobra prints the error message itself (SilenceErrors is false on the
	// root command); we only need to translate "non-nil" into a non-zero exit.
	if err := cmd.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
