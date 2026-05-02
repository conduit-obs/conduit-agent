package doctor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/conduit-obs/conduit-agent/internal/config"
)

// CDT0101 — output.endpoint_reachable. Confirms a TCP+TLS handshake
// against whatever endpoint output.mode selects. Doesn't push test
// data (that's the M11+ `conduit send-test-data` story); this is the
// "can the agent talk to the destination at all" check that catches
// firewall + DNS + TLS-trust-store problems before the operator
// tries to ship telemetry.
const cdt0101ID = "CDT0101"

// CDT0102 — output.auth. Confirms the operator supplied a non-empty
// auth value where the schema requires one (Honeycomb API key,
// vendor-supplied OTLP header, gateway-side header). The agent never
// dereferences ${env:NAME} placeholders during preview; this check
// honors that — placeholder strings count as "set, but the
// environment will be filled in at run time".
const cdt0102ID = "CDT0102"

// CDT0103 — output.tls_warning. Surfaces lab-only TLS opt-outs
// (`output.gateway.insecure: true`, refinery insecure: true, etc.) as
// warnings so they're visible to anyone running `conduit doctor` on a
// production-shaped install. Per AC-06.3, the warning fires even when
// the connection succeeds.
const cdt0103ID = "CDT0103"

// CheckOutputEndpoint reports CDT0101. It opens a TCP connection to
// the endpoint host:port (and a TLS handshake when the URL scheme is
// https or the gateway/refinery exporter sits on a default-TLS gRPC
// transport). Failure modes: DNS, TCP refused, TLS verify, timeout —
// each maps to a distinct message so operators don't have to guess
// which layer broke.
func CheckOutputEndpoint(ctx Context) []Result {
	if ctx.Config == nil {
		return []Result{skip(cdt0101ID, "output.endpoint_reachable", "no config loaded; CDT0001 must pass first")}
	}
	target, err := resolveEndpoint(&ctx.Config.Output)
	if err != nil {
		return []Result{skip(cdt0101ID, "output.endpoint_reachable", err.Error())}
	}
	if target.skip {
		return []Result{skip(cdt0101ID, "output.endpoint_reachable", target.skipReason)}
	}

	dctx, cancel := context.WithTimeout(ctx.Ctx, 8*time.Second)
	defer cancel()

	if msg := dialTarget(dctx, target); msg != "" {
		return []Result{{
			ID:       cdt0101ID,
			Title:    "output.endpoint_reachable",
			Severity: SeverityFail,
			Message:  msg,
			DocsURL:  docsAnchor(cdt0101ID, "output-endpoint-reachable"),
		}}
	}
	return []Result{{
		ID:       cdt0101ID,
		Title:    "output.endpoint_reachable",
		Severity: SeverityPass,
		Message:  fmt.Sprintf("TCP+TLS handshake to %s succeeded.", target.display),
		DocsURL:  docsAnchor(cdt0101ID, "output-endpoint-reachable"),
	}}
}

// CheckOutputAuth reports CDT0102. We don't dereference env vars (the
// embedded collector does that at startup); we just verify the
// schema-required auth field has a non-empty value or a syntactically
// valid ${env:NAME} placeholder.
func CheckOutputAuth(ctx Context) []Result {
	if ctx.Config == nil {
		return []Result{skip(cdt0102ID, "output.auth", "no config loaded; CDT0001 must pass first")}
	}
	switch ctx.Config.Output.Mode {
	case config.OutputModeHoneycomb:
		if ctx.Config.Output.Honeycomb == nil {
			return []Result{skip(cdt0102ID, "output.auth", "output.honeycomb block is missing; CDT0001 caught this already")}
		}
		return []Result{checkAuthValue(
			"output.auth",
			"output.honeycomb.api_key",
			ctx.Config.Output.Honeycomb.APIKey)}
	case config.OutputModeOTLP:
		if ctx.Config.Output.OTLP == nil || len(ctx.Config.Output.OTLP.Headers) == 0 {
			// OTLP mode without any header is plausible (some
			// destinations authenticate via mTLS or signed URLs).
			// Skip rather than fail; M11 follow-up could promote
			// to a warn once we have field signal.
			return []Result{skip(cdt0102ID, "output.auth", "no headers configured (some OTLP destinations don't need any); skipping check")}
		}
		var results []Result
		for k, v := range ctx.Config.Output.OTLP.Headers {
			results = append(results, checkAuthValue(
				"output.auth",
				"output.otlp.headers."+k,
				v))
		}
		return results
	case config.OutputModeGateway:
		if ctx.Config.Output.Gateway == nil || len(ctx.Config.Output.Gateway.Headers) == 0 {
			return []Result{skip(cdt0102ID, "output.auth", "no gateway headers configured (the gateway may authenticate via mTLS); skipping check")}
		}
		var results []Result
		for k, v := range ctx.Config.Output.Gateway.Headers {
			results = append(results, checkAuthValue(
				"output.auth",
				"output.gateway.headers."+k,
				v))
		}
		return results
	}
	return []Result{skip(cdt0102ID, "output.auth", "unsupported output.mode (CDT0001 caught this)")}
}

// CheckOutputTLS reports CDT0103. Fires a Warn whenever the rendered
// config has `tls.insecure: true` on any egress exporter. AC-06.3
// requires the warning even on a successful connection, so we don't
// gate on CDT0101's outcome.
func CheckOutputTLS(ctx Context) []Result {
	if ctx.Config == nil {
		return []Result{skip(cdt0103ID, "output.tls_warning", "no config loaded; CDT0001 must pass first")}
	}
	o := &ctx.Config.Output
	switch o.Mode {
	case config.OutputModeOTLP:
		if o.OTLP != nil && o.OTLP.Insecure {
			return []Result{warnInsecure("output.otlp.insecure", o.OTLP.Endpoint)}
		}
	case config.OutputModeGateway:
		if o.Gateway != nil && o.Gateway.Insecure {
			return []Result{warnInsecure("output.gateway.insecure", o.Gateway.Endpoint)}
		}
	case config.OutputModeHoneycomb:
		// Honeycomb direct has no insecure knob; refinery sub-field
		// does (M10.B). Warn on it the same way the gateway insecure
		// warning fires.
		if o.Honeycomb != nil && o.Honeycomb.Traces != nil &&
			o.Honeycomb.Traces.ViaRefinery != nil &&
			o.Honeycomb.Traces.ViaRefinery.Insecure {
			return []Result{warnInsecure("output.honeycomb.traces.via_refinery.insecure",
				o.Honeycomb.Traces.ViaRefinery.Endpoint)}
		}
	}
	return []Result{{
		ID:       cdt0103ID,
		Title:    "output.tls_warning",
		Severity: SeverityPass,
		Message:  "TLS verification is enabled on every egress exporter (no insecure: true overrides found).",
		DocsURL:  docsAnchor(cdt0103ID, "output-tls-warning"),
	}}
}

// endpointTarget describes one network probe target. dialTarget reads
// these fields; resolveEndpoint produces them from the Output block.
type endpointTarget struct {
	host       string
	port       string
	useTLS     bool
	display    string
	skip       bool
	skipReason string
}

// resolveEndpoint computes the network target for the active output
// mode. Refinery routing (M10.B) is intentionally NOT probed here —
// the Refinery cluster might be reachable only from inside a
// customer's cluster, and a doctor run from the operator's laptop
// would falsely fail. M12's E2E matrix tests Refinery reachability
// from inside the cluster.
func resolveEndpoint(o *config.Output) (endpointTarget, error) {
	switch o.Mode {
	case config.OutputModeHoneycomb:
		if o.Honeycomb == nil {
			return endpointTarget{}, fmt.Errorf("output.honeycomb is missing")
		}
		ep := o.Honeycomb.Endpoint
		if strings.Contains(ep, "${env:") {
			return endpointTarget{skip: true, skipReason: fmt.Sprintf("endpoint %q references an env var; can't resolve at preview time", ep)}, nil
		}
		host, port, useTLS, err := parseHTTPEndpoint(ep)
		if err != nil {
			return endpointTarget{}, err
		}
		return endpointTarget{host: host, port: port, useTLS: useTLS, display: ep}, nil
	case config.OutputModeOTLP:
		if o.OTLP == nil {
			return endpointTarget{}, fmt.Errorf("output.otlp is missing")
		}
		host, port, useTLS, err := parseHTTPEndpoint(o.OTLP.Endpoint)
		if err != nil {
			return endpointTarget{}, err
		}
		if o.OTLP.Insecure {
			useTLS = false
		}
		return endpointTarget{host: host, port: port, useTLS: useTLS, display: o.OTLP.Endpoint}, nil
	case config.OutputModeGateway:
		if o.Gateway == nil {
			return endpointTarget{}, fmt.Errorf("output.gateway is missing")
		}
		host, port, useTLS, err := parseGRPCEndpoint(o.Gateway.Endpoint, !o.Gateway.Insecure)
		if err != nil {
			return endpointTarget{}, err
		}
		return endpointTarget{host: host, port: port, useTLS: useTLS, display: o.Gateway.Endpoint}, nil
	}
	return endpointTarget{}, fmt.Errorf("unsupported output.mode %q", o.Mode)
}

// parseHTTPEndpoint resolves an OTLP/HTTP URL ("https://api.honeycomb.io"
// → host="api.honeycomb.io", port="443", tls=true). Default ports are
// applied for bare URLs without an explicit port.
func parseHTTPEndpoint(raw string) (host, port string, useTLS bool, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false, fmt.Errorf("invalid endpoint URL %q: %w", raw, err)
	}
	host = u.Hostname()
	if host == "" {
		return "", "", false, fmt.Errorf("endpoint URL %q is missing the host", raw)
	}
	port = u.Port()
	switch u.Scheme {
	case "https":
		if port == "" {
			port = "443"
		}
		return host, port, true, nil
	case "http":
		if port == "" {
			port = "80"
		}
		return host, port, false, nil
	default:
		return "", "", false, fmt.Errorf("endpoint URL %q must use scheme http or https; got %q", raw, u.Scheme)
	}
}

// parseGRPCEndpoint resolves an OTLP/gRPC endpoint. gRPC endpoints
// are typically host:port pairs ("gateway.internal:4317") rather
// than full URLs, but the upstream otlpexporter also accepts
// "dns:///host:port" and "https://host:port". TLS-on default unless
// the explicit `insecure: true` knob is set (the secureDefault
// argument).
func parseGRPCEndpoint(raw string, secureDefault bool) (host, port string, useTLS bool, err error) {
	cleaned := strings.TrimPrefix(raw, "dns:///")
	if i := strings.Index(cleaned, "://"); i >= 0 {
		cleaned = cleaned[i+3:]
	}
	host, port, err = net.SplitHostPort(cleaned)
	if err != nil {
		return "", "", false, fmt.Errorf("invalid gateway endpoint %q (expected host:port): %w", raw, err)
	}
	return host, port, secureDefault, nil
}

// dialTarget runs the TCP + TLS probe. Returns an empty string on
// success and a structured failure message otherwise. We split the
// dial and the TLS handshake so the message can name which layer
// broke (TCP refused vs. TLS verify failure).
func dialTarget(ctx context.Context, t endpointTarget) string {
	addr := net.JoinHostPort(t.host, t.port)
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Sprintf("TCP connect to %s failed: %v", t.display, err)
	}
	defer conn.Close()
	if !t.useTLS {
		return ""
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: t.host})
	defer tlsConn.Close()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Sprintf("TLS handshake to %s failed: %v", t.display, err)
	}
	return ""
}

// checkAuthValue is the per-header validator for CDT0102. We accept
// three shapes:
//
//   - empty / whitespace-only → fail
//   - ${env:NAME} placeholder (the embedded collector resolves these
//     at run time; doctor can't dereference) → pass with a "deferred"
//     note so operators know the actual auth lands at runtime
//   - any other non-empty string → pass
func checkAuthValue(title, path, value string) Result {
	v := strings.TrimSpace(value)
	if v == "" {
		return Result{
			ID:       cdt0102ID,
			Title:    title,
			Severity: SeverityFail,
			Message:  fmt.Sprintf("%s is required but empty.", path),
			DocsURL:  docsAnchor(cdt0102ID, "output-auth"),
		}
	}
	if strings.Contains(v, "${env:") {
		return Result{
			ID:       cdt0102ID,
			Title:    title,
			Severity: SeverityPass,
			Message:  fmt.Sprintf("%s references an env var; the embedded collector resolves it at startup.", path),
			DocsURL:  docsAnchor(cdt0102ID, "output-auth"),
		}
	}
	return Result{
		ID:       cdt0102ID,
		Title:    title,
		Severity: SeverityPass,
		Message:  fmt.Sprintf("%s is set (length=%d).", path, len(v)),
		DocsURL:  docsAnchor(cdt0102ID, "output-auth"),
	}
}

// warnInsecure builds the standard "TLS verification is disabled" warn
// surfaced by CheckOutputTLS. Reused across the three places insecure
// can show up so the message stays consistent.
func warnInsecure(field, endpoint string) Result {
	return Result{
		ID:       cdt0103ID,
		Title:    "output.tls_warning",
		Severity: SeverityWarn,
		Message: fmt.Sprintf(
			"%s = true on %s — TLS verification is disabled. This is a lab-only override per ADR-0009; production destinations should present a valid certificate.",
			field, endpoint),
		DocsURL: docsAnchor(cdt0103ID, "output-tls-warning"),
	}
}

// skip emits a non-blocking SKIP result. Used by the output checks
// when prerequisites (config loaded, network resolvable, env vars
// dereferenced) aren't satisfied.
func skip(id, title, reason string) Result {
	return Result{
		ID:       id,
		Title:    title,
		Severity: SeveritySkip,
		Message:  reason,
		DocsURL:  docsAnchor(id, slugFromTitle(title)),
	}
}

// slugFromTitle turns "output.endpoint_reachable" → "output-endpoint-reachable"
// for docs URL anchors.
func slugFromTitle(t string) string {
	return strings.ReplaceAll(strings.ReplaceAll(t, ".", "-"), "_", "-")
}
