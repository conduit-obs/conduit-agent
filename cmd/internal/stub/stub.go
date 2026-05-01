// Package stub provides a small helper used by M1 subcommand stubs so that
// every subcommand emits the same "not implemented" message and exit shape.
// Removing this package is part of the M2 work that wires real behavior.
package stub

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NotImplementedError is returned by stub subcommand RunEs so the caller can
// distinguish "this milestone hasn't shipped yet" from "real failure". Cobra
// renders the Error() output to stderr and we exit non-zero in main.go, so
// nothing else is needed to surface the message.
type NotImplementedError struct {
	Command   string
	Milestone string
}

func (e *NotImplementedError) Error() string {
	return fmt.Sprintf("%s: not implemented yet; see milestone %s", e.Command, e.Milestone)
}

// RunE returns a cobra.Command RunE that returns a NotImplementedError.
// Cobra's default error rendering ("Error: <message>") is what the user sees
// — there is no extra print here, so the message is not duplicated.
func RunE(command, milestone string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		return &NotImplementedError{Command: command, Milestone: milestone}
	}
}
