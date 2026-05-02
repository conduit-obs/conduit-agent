//go:build windows

package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"golang.org/x/sys/windows/svc"
)

// runWithLifecycle on Windows has two paths driven by who started the
// process:
//
//   - When the Windows Service Control Manager (SCM) starts the binary
//     (svc.IsWindowsService() == true), we MUST hand control to
//     svc.Run, which dispatches the SCM's Stop / Shutdown / Interrogate
//     commands to our handler. Failing to do this in service mode
//     causes the SCM to mark the service as failed within a startup
//     timeout and Windows will refuse to start it again. The MSI
//     installer (M6.C) registers `conduit run -c <path>` as the
//     service ImagePath, so this is the standard production path.
//
//   - When started from a console (developer hitting `conduit run` in
//     PowerShell, or a smoke test in CI), svc.IsWindowsService() == false
//     and we drop into a foreground loop with Ctrl+C handling that
//     mirrors the Unix path. SIGTERM is not deliverable on Windows;
//     os.Interrupt covers Ctrl+C and SCM-style termination from
//     non-service consoles.
//
// `serviceName` must match the Name registered in
// deploy/windows/wix/conduit.wxs's ServiceInstall element (M6.C);
// SCM looks the handler up by that name when dispatching commands.
func runWithLifecycle(parent context.Context, serviceName string, fn func(context.Context) error) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("run: detect windows service mode: %w", err)
	}
	if !isService {
		ctx, cancel := signal.NotifyContext(parent, os.Interrupt)
		defer cancel()
		return fn(ctx)
	}
	return runAsWindowsService(parent, serviceName, fn)
}

// runAsWindowsService hands control to svc.Run, which blocks until the
// SCM tells the handler to stop (or the wrapped fn returns on its own).
// The dispatch is single-shot per process — SCM treats a returned
// service as terminated.
func runAsWindowsService(parent context.Context, serviceName string, fn func(context.Context) error) error {
	h := &serviceHandler{parent: parent, run: fn}
	if err := svc.Run(serviceName, h); err != nil {
		return fmt.Errorf("run: svc.Run(%q): %w", serviceName, err)
	}
	if h.runErr != nil && !errors.Is(h.runErr, context.Canceled) {
		return h.runErr
	}
	return nil
}

// serviceHandler bridges the SCM's command channel to our cancelable
// runFn. The handler runs fn in a goroutine and translates Stop /
// Shutdown into a context cancellation; svc.Interrogate is answered
// with the current status as required by the SCM contract.
type serviceHandler struct {
	parent context.Context
	run    func(context.Context) error
	runErr error
}

// Execute implements svc.Handler. The svc package documents the
// status-channel protocol: send StartPending → Running with the
// commands we accept → StopPending on stop request → return.
// Returning from Execute is what lets svc.Run return.
func (h *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(h.parent)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()

	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				h.runErr = <-done
				if h.runErr != nil && !errors.Is(h.runErr, context.Canceled) {
					return false, 1
				}
				return false, 0
			default:
				// Other commands (Pause / Continue / ParamChange) aren't
				// in our Accepts mask; the SCM should never send them,
				// but if it does, log and ignore.
			}
		case err := <-done:
			// The collector exited on its own (config error, fatal
			// pipeline failure, ...). Translate to a non-zero service
			// exit code so the SCM marks the service as failed and
			// can apply its restart-on-failure policy.
			h.runErr = err
			if err != nil && !errors.Is(err, context.Canceled) {
				return false, 1
			}
			return false, 0
		}
	}
}
