package doctor

// DefaultChecks returns the production catalog of doctor checks in the
// order they should run for a typical install. Order matters because
// later checks depend on earlier ones (e.g. the output checks skip
// gracefully when CDT0001 fails because Config is nil), and because
// the human-readable report prints in catalog order under each
// severity bucket.
//
// The order is:
//
//  1. Config-side checks first (CDT0001, CDT0501) — every other
//     check builds on a parsed Config, so a broken config short-
//     circuits the slower probes.
//
//  2. Receiver checks (CDT0201, CDT0202) — local probes that don't
//     hit the network, so we run them before any outbound dial.
//
//  3. Output checks (CDT0102 auth, CDT0103 tls warning, CDT0101
//     reachability) — auth + tls inspect the parsed config; the
//     reachability probe runs last because it has the longest
//     timeout (8 seconds + dialer timeouts) and is the most likely
//     check to be affected by env-specific networking quirks.
//
//  4. Version compat (CDT0403) — read-only fingerprint of the
//     install; runs last so its PASS line caps the report.
func DefaultChecks() []Definition {
	return []Definition{
		{ID: cdt0001ID, Title: "config.syntax", Run: CheckConfigSyntax},
		{ID: cdt0201ID, Title: "receiver.ports", Run: CheckReceiverPorts},
		{ID: cdt0202ID, Title: "receiver.permissions", Run: CheckReceiverPermissions},
		{ID: cdt0102ID, Title: "output.auth", Run: CheckOutputAuth},
		{ID: cdt0103ID, Title: "output.tls_warning", Run: CheckOutputTLS},
		{ID: cdt0101ID, Title: "output.endpoint_reachable", Run: CheckOutputEndpoint},
		{ID: cdt0403ID, Title: "version.compat", Run: CheckVersionCompat},
	}
}
