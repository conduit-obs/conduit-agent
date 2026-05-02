//go:build !windows

package run

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// runWithLifecycle on non-Windows platforms is the simple foreground
// path: derive a cancelable context from `parent`, wire SIGINT and
// SIGTERM into it, and call fn. systemd (linux) and launchd (darwin)
// both deliver SIGTERM as the standard "stop the service" signal, so
// the same code handles operator-driven Ctrl+C, systemd `systemctl
// stop`, and launchd `launchctl unload`.
//
// `serviceName` is unused here — it only matters on Windows where SCM
// dispatch needs the registered service name. Keeping the parameter in
// the cross-platform signature avoids `//go:build` directives leaking
// into runCmd.
func runWithLifecycle(parent context.Context, _ string, fn func(context.Context) error) error {
	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return fn(ctx)
}
