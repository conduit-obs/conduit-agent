package doctor

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/conduit-obs/conduit-agent/internal/collector"
)

// CDT0204 — receiver.obi. Preflight for the OpenTelemetry eBPF
// Instrumentation receiver introduced in ADR-0020. The check is
// SeveritySkip when obi.enabled is false (or the field is omitted on
// a non-k8s profile, where applyDefaults resolves it to false), so
// non-OBI installs are not bothered. When obi.enabled is true the
// check fires a series of preflights against the running host:
//
//  1. Linux only (OBI is Linux-only by upstream design).
//  2. Binary has the OBI receiver compiled in (collector.HasReceiver).
//     Catches the "config says obi.enabled: true but the build
//     didn't link OBI" case ADR-0020 documents as a deferred
//     consequence of the OCB / BPF-binding work.
//  3. Kernel ≥ 5.8 (or RHEL-family ≥ 4.18 with backports — best-
//     effort detection from os-release).
//  4. BTF type information available at /sys/kernel/btf/vmlinux.
//  5. The required ambient/effective capabilities are present on
//     the running process.
//
// Each sub-check produces one Result so operators see the full
// preflight in the report; if any sub-check is FAIL, doctor exits
// non-zero per AC-14 and the operator gets a precise remediation
// line for the specific problem.
const cdt0204ID = "CDT0204"

// minOBIKernelMajor / minOBIKernelMinor encode the upstream OBI
// kernel-version floor (5.8 mainline, where CAP_PERFMON / CAP_BPF
// landed). RHEL-family kernels backport the BPF surface to 4.18; we
// detect that case via /etc/os-release and adjust the minimum.
const (
	minOBIKernelMajor    = 5
	minOBIKernelMinor    = 8
	minRHELKernelMajor   = 4
	minRHELKernelMinor   = 18
	btfVmlinuxPath       = "/sys/kernel/btf/vmlinux"
	osReleasePath        = "/etc/os-release"
	procStatusPath       = "/proc/self/status"
	procKernelOSReleaseP = "/proc/sys/kernel/osrelease"
)

// Capability bit positions from linux/include/uapi/linux/capability.h.
// CAP_PERFMON / CAP_BPF were added in kernel 5.8; older kernels won't
// have them defined, but those kernels also can't run OBI (the kernel
// check above catches that earlier in the report).
const (
	capDACReadSearch = 2
	capNetRaw        = 13
	capSysPtrace     = 19
	capSysAdmin      = 21
	capPerfmon       = 38
	capBPF           = 39
)

// CheckOBIPreflight reports CDT0204. See package doc for the sub-check
// list and exit-code semantics.
func CheckOBIPreflight(ctx Context) []Result {
	if ctx.Config == nil || ctx.Config.OBI == nil {
		return []Result{skip(cdt0204ID, "receiver.obi", "no config loaded; CDT0001 must pass first")}
	}
	if !ctx.Config.OBI.OBIEnabled(ctx.Config.Profile) {
		return []Result{skip(cdt0204ID, "receiver.obi", "obi.enabled is false; OBI preflight does not apply.")}
	}

	if runtime.GOOS != "linux" {
		return []Result{{
			ID:       cdt0204ID,
			Title:    "receiver.obi",
			Severity: SeverityFail,
			Message: fmt.Sprintf("OBI is Linux-only (eBPF receiver); current GOOS is %q. "+
				"Set obi.enabled: false, or run on Linux. See ADR-0020.", runtime.GOOS),
			DocsURL: docsAnchor(cdt0204ID, "receiver-obi"),
		}}
	}

	if !collector.HasReceiver("obi") {
		return []Result{{
			ID:       cdt0204ID,
			Title:    "receiver.obi",
			Severity: SeverityFail,
			Message: "obi.enabled is true but this Conduit binary was built without the OBI receiver. " +
				"Either set obi.enabled: false, or rebuild Conduit with go.opentelemetry.io/obi added " +
				"to builder-config.yaml (see ADR-0020 § 'Open question: build pipeline').",
			DocsURL: docsAnchor(cdt0204ID, "receiver-obi"),
		}}
	}

	return []Result{
		obiKernelResult(),
		obiBTFResult(),
		obiCapsResult(),
	}
}

func obiKernelResult() Result {
	major, minor, raw := readKernelVersion()
	if raw == "" {
		return Result{
			ID:       cdt0204ID,
			Title:    "receiver.obi",
			Severity: SeverityWarn,
			Message:  "could not read kernel version from /proc/sys/kernel/osrelease; OBI may not load.",
			DocsURL:  docsAnchor(cdt0204ID, "receiver-obi"),
		}
	}
	rhelLike := osReleaseHasRHELFamily()
	wantMajor, wantMinor := minOBIKernelMajor, minOBIKernelMinor
	if rhelLike {
		wantMajor, wantMinor = minRHELKernelMajor, minRHELKernelMinor
	}
	if major < wantMajor || (major == wantMajor && minor < wantMinor) {
		floor := fmt.Sprintf("%d.%d", wantMajor, wantMinor)
		family := "mainline"
		if rhelLike {
			family = "RHEL-family"
		}
		return Result{
			ID:       cdt0204ID,
			Title:    "receiver.obi",
			Severity: SeverityFail,
			Message: fmt.Sprintf("kernel %s is below the OBI %s floor of %s. "+
				"Upgrade the kernel or set obi.enabled: false. See ADR-0020.", raw, family, floor),
			DocsURL: docsAnchor(cdt0204ID, "receiver-obi"),
		}
	}
	return Result{
		ID:       cdt0204ID,
		Title:    "receiver.obi",
		Severity: SeverityPass,
		Message:  fmt.Sprintf("kernel %s is in the supported window for OBI.", raw),
		DocsURL:  docsAnchor(cdt0204ID, "receiver-obi"),
	}
}

func obiBTFResult() Result {
	if _, err := os.Stat(btfVmlinuxPath); err != nil {
		return Result{
			ID:       cdt0204ID,
			Title:    "receiver.obi",
			Severity: SeverityWarn,
			Message: fmt.Sprintf("%s is missing or unreadable (%v). "+
				"OBI may still load on kernels with embedded BTF, but most distributions ship BTF and the absence is unusual. "+
				"Check that linux-headers / kernel-debuginfo equivalents are installed.",
				btfVmlinuxPath, err),
			DocsURL: docsAnchor(cdt0204ID, "receiver-obi"),
		}
	}
	return Result{
		ID:       cdt0204ID,
		Title:    "receiver.obi",
		Severity: SeverityPass,
		Message:  fmt.Sprintf("BTF type info available at %s; OBI can attach without manual offsets.", btfVmlinuxPath),
		DocsURL:  docsAnchor(cdt0204ID, "receiver-obi"),
	}
}

func obiCapsResult() Result {
	if os.Geteuid() == 0 {
		return Result{
			ID:       cdt0204ID,
			Title:    "receiver.obi",
			Severity: SeverityPass,
			Message:  "process running as root; all OBI eBPF capabilities are implicitly granted.",
			DocsURL:  docsAnchor(cdt0204ID, "receiver-obi"),
		}
	}
	caps, err := readEffectiveCaps()
	if err != nil {
		return Result{
			ID:       cdt0204ID,
			Title:    "receiver.obi",
			Severity: SeverityWarn,
			Message: fmt.Sprintf("could not read effective caps from %s: %v. "+
				"If the agent fails to start, ensure /etc/systemd/system/conduit.service.d/obi.conf grants the eBPF caps "+
				"(install via `sudo ./install_linux.sh --with-obi`).", procStatusPath, err),
			DocsURL: docsAnchor(cdt0204ID, "receiver-obi"),
		}
	}
	required := []struct {
		bit  uint
		name string
	}{
		{capSysAdmin, "CAP_SYS_ADMIN"},
		{capDACReadSearch, "CAP_DAC_READ_SEARCH"},
		{capNetRaw, "CAP_NET_RAW"},
		{capSysPtrace, "CAP_SYS_PTRACE"},
		{capPerfmon, "CAP_PERFMON"},
		{capBPF, "CAP_BPF"},
	}
	var missing []string
	for _, c := range required {
		if caps&(uint64(1)<<c.bit) == 0 {
			missing = append(missing, c.name)
		}
	}
	if len(missing) == 0 {
		return Result{
			ID:       cdt0204ID,
			Title:    "receiver.obi",
			Severity: SeverityPass,
			Message:  "every OBI-required eBPF capability is present on the running process.",
			DocsURL:  docsAnchor(cdt0204ID, "receiver-obi"),
		}
	}
	return Result{
		ID:       cdt0204ID,
		Title:    "receiver.obi",
		Severity: SeverityFail,
		Message: fmt.Sprintf("missing eBPF capabilities: %s. "+
			"Run `sudo ./install_linux.sh --with-obi` to install the systemd drop-in that grants the full set "+
			"(CAP_SYS_ADMIN, CAP_DAC_READ_SEARCH, CAP_NET_RAW, CAP_SYS_PTRACE, CAP_PERFMON, CAP_BPF), "+
			"then `sudo systemctl restart conduit`.", strings.Join(missing, ", ")),
		DocsURL: docsAnchor(cdt0204ID, "receiver-obi"),
	}
}

// readKernelVersion parses /proc/sys/kernel/osrelease into (major,
// minor, raw). Lines look like "5.15.0-101-generic" or "4.18.0-553.el8.x86_64".
// Returns 0,0,"" when the file can't be read; callers fall back to a
// WARN result.
func readKernelVersion() (major, minor int, raw string) {
	data, err := os.ReadFile(procKernelOSReleaseP)
	if err != nil {
		return 0, 0, ""
	}
	raw = strings.TrimSpace(string(data))
	parts := strings.SplitN(raw, ".", 3)
	if len(parts) < 2 {
		return 0, 0, raw
	}
	major, _ = strconv.Atoi(parts[0])
	// parts[1] may carry a suffix on weird kernels (e.g. "15-generic");
	// take the leading run of digits.
	var minorStr strings.Builder
	for _, r := range parts[1] {
		if r < '0' || r > '9' {
			break
		}
		minorStr.WriteRune(r)
	}
	minor, _ = strconv.Atoi(minorStr.String())
	return major, minor, raw
}

// osReleaseHasRHELFamily best-effort-detects RHEL / CentOS / Rocky /
// AlmaLinux / Oracle Linux from /etc/os-release. The ID_LIKE field
// includes "rhel" on every member of the family on supported releases;
// ID alone covers older releases that don't set ID_LIKE.
func osReleaseHasRHELFamily() bool {
	data, err := os.ReadFile(osReleasePath)
	if err != nil {
		return false
	}
	body := strings.ToLower(string(data))
	for _, marker := range []string{`id="rhel"`, `id=rhel`, `id_like="rhel"`, `id_like=rhel`,
		`id="centos"`, `id="rocky"`, `id="almalinux"`, `id="ol"`} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// readEffectiveCaps reads /proc/self/status and returns the value of
// the CapEff bitmap. The line looks like "CapEff:   00000000a80425fb".
func readEffectiveCaps() (uint64, error) {
	data, err := os.ReadFile(procStatusPath)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "CapEff:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("malformed CapEff line: %q", line)
		}
		return strconv.ParseUint(fields[1], 16, 64)
	}
	return 0, fmt.Errorf("no CapEff line in %s", procStatusPath)
}

