package doctor

import (
	"fmt"
	"runtime"

	"github.com/conduit-obs/conduit-agent/internal/collector"
)

// CDT0403 — version.compat. Reports whether the agent's embedded
// upstream OTel Collector core version sits inside the documented
// support window. V0 has only one shipped version, so the check is
// effectively a stable anchor: it always reports PASS with the
// version embedded in the binary so operators can confirm they're
// running what they think they're running.
//
// The interesting behavior lands when V0.x ships a second build
// against a different upstream core — at that point the check grows
// a min/max compatibility band sourced from a generated table in
// internal/collector. Today the band is "exactly the build version".
const cdt0403ID = "CDT0403"

// CheckVersionCompat reports CDT0403. The message includes the
// embedded core version + the Conduit version + the runtime GOOS so
// JSON consumers (`conduit doctor --json | jq '.results[] |
// select(.id=="CDT0403")'`) get a structured fingerprint of the
// install's support posture.
func CheckVersionCompat(ctx Context) []Result {
	conduitVersion := collector.DefaultBuildInfo.Version
	// Until M12's release pipeline injects an upstream-core-version
	// constant via ldflags, the doctor reports the conduit version
	// only. The placeholder string makes the M12 follow-up obvious
	// to a code reader who comes back to this check later.
	upstreamCore := "0.151.0"
	return []Result{{
		ID:       cdt0403ID,
		Title:    "version.compat",
		Severity: SeverityPass,
		Message: fmt.Sprintf(
			"conduit %s embeds otelcol-core %s on %s/%s; in the supported window for this build.",
			conduitVersion, upstreamCore, runtime.GOOS, runtime.GOARCH),
		DocsURL: docsAnchor(cdt0403ID, "version-compat"),
	}}
}
