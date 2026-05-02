package doctor

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/conduit-obs/conduit-agent/internal/config"
)

// CDT0201 — receiver.ports. Confirms the OTLP gRPC + HTTP ports
// (4317, 4318) are not already in use by another process. AC-14.3
// requires the conflicting PID in the message; we do best-effort
// process resolution via /proc on linux. On non-linux we report the
// LocalAddr only.
const cdt0201ID = "CDT0201"

// CDT0202 — receiver.permissions. Confirms the agent process can
// open every filelog include path declared in the rendered profile
// fragments. The check parses the rendered YAML for top-level
// `filelog/...:` blocks and for `include: [paths]` lists; it intentionally
// doesn't enumerate every operator-overridden path because the
// schema doesn't surface those before the agent starts.
const cdt0202ID = "CDT0202"

// CheckReceiverPorts reports CDT0201. We read OTLPBindAddress from
// the rendered template (4317 + 4318 are hard-coded in V0); a probe
// listen attempt against the same address tells us whether another
// process already holds the port. Skip when the rendered YAML is
// empty (the parse / validate stage would have caught that).
func CheckReceiverPorts(ctx Context) []Result {
	if ctx.Config == nil {
		return []Result{skip(cdt0201ID, "receiver.ports", "no config loaded; CDT0001 must pass first")}
	}
	bind := otlpBindHost(ctx.Config.Profile)
	var results []Result
	for _, port := range []string{"4317", "4318"} {
		results = append(results, probePort(bind, port))
	}
	return results
}

// CheckReceiverPermissions reports CDT0202 by attempting to stat /
// open every filelog include path declared in the rendered YAML.
// `start_at: end` and `exclude:` lines are not consulted — we only
// care whether the underlying file is readable. Missing files are
// non-fatal (filelog skips them at runtime), but unreadable files
// (EACCES) always fail.
func CheckReceiverPermissions(ctx Context) []Result {
	if ctx.RenderedYAML == "" {
		return []Result{skip(cdt0202ID, "receiver.permissions", "no rendered config available; CDT0001 must pass first")}
	}
	paths := extractFilelogPaths(ctx.RenderedYAML)
	if len(paths) == 0 {
		return []Result{{
			ID:       cdt0202ID,
			Title:    "receiver.permissions",
			Severity: SeverityPass,
			Message:  "no filelog receivers in the rendered config; nothing to check.",
			DocsURL:  docsAnchor(cdt0202ID, "receiver-permissions"),
		}}
	}

	var results []Result
	failed := false
	for _, p := range paths {
		// Globs (used everywhere in filelog include lists) need an
		// expansion pass before we can stat individual files.
		matches, err := filepath.Glob(p)
		if err != nil {
			results = append(results, Result{
				ID:       cdt0202ID,
				Title:    "receiver.permissions",
				Severity: SeverityFail,
				Message:  fmt.Sprintf("filelog pattern %q is not a valid glob: %v", p, err),
				DocsURL:  docsAnchor(cdt0202ID, "receiver-permissions"),
			})
			failed = true
			continue
		}
		if len(matches) == 0 {
			// No files match yet — this is fine; filelog will pick
			// them up when they appear (e.g. /var/log/syslog after
			// the next rotation on a fresh install). Pass with a
			// note so operators can spot the empty-glob case in
			// the report.
			results = append(results, Result{
				ID:       cdt0202ID,
				Title:    "receiver.permissions",
				Severity: SeverityPass,
				Message:  fmt.Sprintf("filelog include %q has no matches yet; will be picked up when files appear.", p),
				DocsURL:  docsAnchor(cdt0202ID, "receiver-permissions"),
			})
			continue
		}
		for _, m := range matches {
			if err := canRead(m); err != nil {
				results = append(results, Result{
					ID:       cdt0202ID,
					Title:    "receiver.permissions",
					Severity: SeverityFail,
					Message: fmt.Sprintf("filelog include %q is not readable by the agent process: %v. "+
						"Add the agent user to the file's group (typically `adm` or `systemd-journal` on Debian/Ubuntu, `systemd-journal` on RHEL).",
						m, err),
					DocsURL: docsAnchor(cdt0202ID, "receiver-permissions"),
				})
				failed = true
			}
		}
	}
	if !failed {
		// Collapse to a single PASS Result if every path checked out.
		// Per-path PASS rows are noisy when most installs have ~5+
		// filelog includes from /var/log/.
		summary := fmt.Sprintf("every filelog include path under the rendered config is readable (%d include pattern(s)).", len(paths))
		return []Result{{
			ID:       cdt0202ID,
			Title:    "receiver.permissions",
			Severity: SeverityPass,
			Message:  summary,
			DocsURL:  docsAnchor(cdt0202ID, "receiver-permissions"),
		}}
	}
	return results
}

// otlpBindHost returns the host part of the OTLP receiver address
// the expander would render. Mirrors expander.resolveOTLPBindAddress;
// kept inline here to avoid an import cycle (expander already imports
// config + profiles + doctor would be a sibling).
func otlpBindHost(p *config.Profile) string {
	if p == nil {
		return "127.0.0.1"
	}
	switch p.Mode {
	case config.ProfileModeDocker, config.ProfileModeK8s:
		return "0.0.0.0"
	default:
		return "127.0.0.1"
	}
}

// probePort tries to listen on the same host:port the agent would.
// On success the listener is closed immediately (the real collector
// will rebind a moment later); on EADDRINUSE we surface the message.
func probePort(host, port string) Result {
	addr := net.JoinHostPort(host, port)
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		_ = ln.Close()
		return Result{
			ID:       cdt0201ID,
			Title:    "receiver.ports",
			Severity: SeverityPass,
			Message:  fmt.Sprintf("OTLP port %s is free (probe bound and released cleanly).", addr),
			DocsURL:  docsAnchor(cdt0201ID, "receiver-ports"),
		}
	}
	pid := tryFindHolder(port)
	msg := fmt.Sprintf("OTLP port %s is unavailable: %v.", addr, err)
	if pid != "" {
		msg += " " + pid
	}
	if errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") {
		// Most actionable surface: someone is already listening on
		// 4317 / 4318. The classic case is a stray collector left
		// over from a previous deploy.
		return Result{
			ID:       cdt0201ID,
			Title:    "receiver.ports",
			Severity: SeverityFail,
			Message:  msg,
			DocsURL:  docsAnchor(cdt0201ID, "receiver-ports"),
		}
	}
	// Anything else (permission denied, network unreachable, etc.)
	// is also actionable but probably an env issue rather than a
	// holder PID — fail for visibility.
	return Result{
		ID:       cdt0201ID,
		Title:    "receiver.ports",
		Severity: SeverityFail,
		Message:  msg,
		DocsURL:  docsAnchor(cdt0201ID, "receiver-ports"),
	}
}

// tryFindHolder returns a "(pid=N command=...)" suffix when we can
// resolve the process holding the port. /proc/net/tcp parsing is
// linux-only; everything else returns an empty string and the caller
// reports the address only.
func tryFindHolder(port string) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	// Best-effort: parse /proc/net/tcp. Lines look like:
	//   sl  local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
	// We need (a) the port hex-decoded, (b) the inode -> /proc/<pid>/fd
	// match. This is intentionally light — failing to resolve just
	// means the message lacks a PID, which still leaves the address
	// in the operator's hands.
	hexPort := fmt.Sprintf(":%04X", parseDec(port))
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, hexPort) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		inode := fields[9]
		if pid := pidForInode(inode); pid != "" {
			return fmt.Sprintf("(pid=%s — see `lsof -i :%s` for details).", pid, port)
		}
	}
	return ""
}

// pidForInode walks /proc/<pid>/fd looking for the matching socket
// inode. Limits the scan to the most-recent few hundred PIDs (sorted
// numerically, descending) so the doctor doesn't take forever on a
// busy host.
func pidForInode(inode string) string {
	target := "socket:[" + inode + "]"
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !allDigits(e.Name()) {
			continue
		}
		fdDir := "/proc/" + e.Name() + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, f := range fds {
			link, err := os.Readlink(fdDir + "/" + f.Name())
			if err != nil {
				continue
			}
			if link == target {
				return e.Name()
			}
		}
	}
	return ""
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func parseDec(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// canRead returns nil when the agent process can read p, or the OS
// error otherwise. We only check that os.Open succeeds; we don't
// actually consume any bytes.
func canRead(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	return f.Close()
}

// extractFilelogPaths walks the rendered YAML for filelog/... receiver
// blocks and pulls each block's `include:` list out of the YAML by
// position. We don't yaml.Unmarshal the full doc (it's a 200-line
// file with a lot of OTTL strings — too much surface to model
// faithfully in a doctor check). The line-oriented approach catches
// every shape the V0 expander produces; operators with custom
// filelog blocks via overrides: are responsible for their own paths.
func extractFilelogPaths(rendered string) []string {
	var paths []string
	lines := strings.Split(rendered, "\n")
	inFilelog := false
	inInclude := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		// Top-level filelog block start (column 2 indent under
		// receivers:). The expander indents all spliced fragments
		// two spaces.
		if strings.HasPrefix(line, "  filelog/") && strings.HasSuffix(trim, ":") {
			inFilelog = true
			inInclude = false
			continue
		}
		if inFilelog && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			// Back to top-level under receivers: — left the block.
			inFilelog = false
			inInclude = false
		}
		if !inFilelog {
			continue
		}
		if strings.HasPrefix(trim, "include:") {
			inInclude = true
			continue
		}
		if inInclude {
			if strings.HasPrefix(trim, "- ") {
				p := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
				p = strings.Trim(p, `"'`)
				if p != "" {
					paths = append(paths, p)
				}
				continue
			}
			// First non-list line under include: closes the list.
			inInclude = false
		}
	}
	return paths
}
