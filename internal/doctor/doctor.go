// Package doctor implements `conduit doctor` (M11) — the structured
// diagnostic the FR-8 spec calls out. Each check has a stable ID
// (CDT0xxx), a human-readable message, an optional docs URL pointing at
// the canonical fix doc, and a Severity. The catalog of check IDs lives
// in [docs/troubleshooting/cdt-codes.md] and is published as part of the
// V0 docs bar at M13.
//
// Architecture:
//
//   - [Severity] is a four-valued enum: Pass / Warn / Fail / Skip.
//     Pass and Skip never block a `conduit doctor` exit; Warn is
//     visible but non-blocking; Fail is the only thing that drives
//     the non-zero exit code AC-14 mandates.
//
//   - [Result] is one check's outcome. Result.Title is the stable check
//     name (e.g. "config.syntax"); Result.ID is the CDT0xxx code so
//     scripts can grep by a stable identifier even if titles get
//     prettier later.
//
//   - [Check] is a function value that runs against a [Context] and
//     returns one or more Results (some checks naturally split into
//     sub-results — output.endpoint_reachable + output.auth share the
//     same network probe, for instance).
//
//   - [Run] composes a slice of Checks against a Context, optionally
//     filters by ID/Title, prints a human or JSON report, and returns
//     the boolean "any check failed" the cmd/doctor wrapper turns into
//     an exit code.
//
// Checks are deliberately functions, not interface-typed objects: the
// V0 catalog has ~10 entries, every check is a pure read-only inspection
// of (config + render + a small bit of system state), and the function-
// composition pattern keeps registration of a new check to "add a line
// to defaultChecks". Interface-typed checks were considered and rejected
// as over-engineering for the V0 surface; revisit at V1 if check sprawl
// becomes a concern.
//
// File-size discipline: this file owns the framework; individual checks
// live in their own files under this package (config_checks.go,
// output_checks.go, receiver_checks.go, etc.) so each stays well under
// 300 LOC.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/conduit-obs/conduit-agent/internal/config"
)

// docsBaseURL is the prefix every CDT0xxx anchor extends. The fragment
// resolution scheme matches what M13's docs site will publish; until
// then the URLs land on the GitHub-rendered docs/troubleshooting page.
const docsBaseURL = "https://github.com/conduit-obs/conduit-agent/blob/main/docs/troubleshooting/cdt-codes.md"

// Severity drives both the human-readable formatting and the
// process-exit decision. Order matters for sort stability and for the
// "any failure?" reduction in [Run].
type Severity int

const (
	// SeverityPass: the check ran and everything looked fine. Printed
	// as "PASS" in human output; never causes a non-zero exit.
	SeverityPass Severity = iota
	// SeveritySkip: the check was inapplicable in this environment
	// (e.g. k8s.permissions when not running on a k8s pod). Printed
	// as "SKIP"; never causes a non-zero exit.
	SeveritySkip
	// SeverityWarn: the check found something the operator should
	// know about but isn't fatal (e.g. output.tls_warning when
	// gateway.insecure: true and the connection still succeeded).
	// Printed as "WARN"; visible but non-blocking.
	SeverityWarn
	// SeverityFail: the check found a problem that will keep Conduit
	// from working correctly. Printed as "FAIL"; drives the non-zero
	// exit code AC-14.4 mandates.
	SeverityFail
)

// String renders the severity as the four-letter token used in the
// human-readable report. Match the JSON form in MarshalJSON below.
func (s Severity) String() string {
	switch s {
	case SeverityPass:
		return "PASS"
	case SeveritySkip:
		return "SKIP"
	case SeverityWarn:
		return "WARN"
	case SeverityFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// MarshalJSON emits the lowercase four-letter token so JSON consumers
// (`conduit doctor --json | jq '.results[].severity'`) get a stable
// vocabulary independent of the human-print form.
func (s Severity) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strings.ToLower(s.String()) + `"`), nil
}

// UnmarshalJSON is the inverse of MarshalJSON. Tests + downstream
// tooling (CI gates that parse + re-emit doctor output) depend on
// round-tripping; without this method json.Unmarshal would reject
// the emitted lowercase token because Severity is an int alias.
func (s *Severity) UnmarshalJSON(data []byte) error {
	tok := strings.Trim(strings.ToLower(string(data)), `"`)
	switch tok {
	case "pass":
		*s = SeverityPass
	case "skip":
		*s = SeveritySkip
	case "warn":
		*s = SeverityWarn
	case "fail":
		*s = SeverityFail
	default:
		return fmt.Errorf("doctor: unknown severity token %q (want pass / skip / warn / fail)", tok)
	}
	return nil
}

// Result is one check's outcome. Multiple Results from a single Check
// are how we surface composite findings — output.endpoint_reachable
// and output.auth share infrastructure but emit separately so the
// human report (and the CDT0xxx anchors) stay one-issue-per-line.
type Result struct {
	// ID is the stable CDT0xxx code. Scripts grep by this; docs
	// anchor by this. Never changes for a given check across
	// versions; new checks get new IDs.
	ID string `json:"id"`
	// Title is the human-friendly check name from FR-8 (e.g.
	// "config.syntax", "output.endpoint_reachable"). Stable but may
	// be polished for clarity over time; ID is the contract.
	Title string `json:"title"`
	// Severity is the four-valued outcome enum.
	Severity Severity `json:"severity"`
	// Message is the operator-facing one-line summary. Human report
	// renders it directly under the title; JSON consumers script on
	// it but should prefer Severity + ID for branching.
	Message string `json:"message"`
	// DocsURL points at the troubleshooting doc anchor for this
	// check. Always populated; the anchor is generated from the
	// CDT0xxx code so new checks get URLs for free.
	DocsURL string `json:"docs_url,omitempty"`
}

// Context is the read-only snapshot every Check inspects. We pass a
// pre-loaded Config + RenderedYAML so individual checks don't re-parse
// the file (and so a check can run on a hand-built struct in tests
// without a config file on disk).
type Context struct {
	// Ctx is the cancellation context for any check that performs
	// blocking I/O (TCP dials, HTTP requests). Per-check timeouts
	// are layered on top via context.WithTimeout in the check
	// implementation.
	Ctx context.Context
	// Config is the parsed + defaulted + validated AgentConfig.
	// May be nil when ConfigErr is set (the config.syntax check
	// reports the parse failure first; downstream checks skip).
	Config *config.AgentConfig
	// ConfigErr captures the parse / validate error if Config is
	// nil. Lets checks report Skip with a contextual reason rather
	// than crashing on a nil dereference.
	ConfigErr error
	// ConfigPath is the on-disk path to conduit.yaml (or "<stdin>"
	// for the piped-in case). Surfaced in messages and JSON output
	// for operator clarity.
	ConfigPath string
	// RenderedYAML is the expanded upstream OTel Collector YAML the
	// embedded collector would receive. Populated when Config != nil
	// and expansion succeeded; empty otherwise.
	RenderedYAML string
	// HTTPClient is the http.Client checks should use for outbound
	// probes. Defaults to a 10-second-timeout client; tests inject a
	// stub via Run's options.
	HTTPClient *http.Client
	// Now is the wall-clock time the check run started. Tests inject
	// a fixed time so timing-sensitive results are deterministic.
	Now time.Time
}

// Check is a single doctor inspection. Receiving Context (not
// *Context) is the convention for "I'm read-only and you can pass me
// across goroutines if you ever want bounded parallelism".
type Check func(ctx Context) []Result

// Definition pairs a Check function with the metadata Run needs to
// filter (--check) and label (in human output). The catalog of
// definitions lives in [DefaultChecks].
type Definition struct {
	// ID is the primary CDT0xxx code the check reports — used by
	// --check to filter. Some checks emit multiple Results with
	// different sub-IDs (e.g. CDT0101 + CDT0102 both come out of the
	// "output" check); ID here is the umbrella code.
	ID string
	// Title is the check name — used by --check for friendly
	// filtering ("--check output.auth").
	Title string
	// Run is the Check function.
	Run Check
}

// matches returns true when the check should run given the user's
// --check filter list. Empty filter = run everything. A filter entry
// matches if it equals the check's ID, equals its Title, or is a
// prefix of its Title (so --check=output runs every output.*).
func (d Definition) matches(filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if f == d.ID || f == d.Title || strings.HasPrefix(d.Title, f+".") {
			return true
		}
	}
	return false
}

// Options drives [Run]. Every field has a documented zero-value default
// so tests can pass {} and get the production-shaped behavior.
type Options struct {
	// Filter restricts the run to a subset of checks. Empty = run
	// every check in the catalog. Entries match either an exact ID,
	// an exact Title, or a Title prefix (output → every output.*).
	Filter []string
	// JSON toggles JSON output. When false (default), Run prints a
	// human-readable report; when true it prints `{"results": [...]}`.
	JSON bool
	// Now overrides time.Now. Tests use a fixed clock so timestamps
	// in JSON output are stable.
	Now time.Time
	// HTTPClient overrides the default 10-second-timeout client.
	// Tests inject a stub; production callers leave nil.
	HTTPClient *http.Client
}

// Run executes the catalog against ctx, prints a report to w (stdout in
// production), and returns true if any check returned SeverityFail. The
// cmd/doctor cobra wrapper turns the bool into a non-zero exit code so
// AC-14 ("Doctor exits non-zero on any failure") holds.
//
// catalog is the list of checks to run (typically [DefaultChecks()]).
// Allowing the caller to supply a custom list keeps the test surface
// trivial: tests construct a Definition list inline and exercise [Run]
// without depending on the production catalog.
func Run(rctx context.Context, catalog []Definition, baseCtx Context, w io.Writer, opts Options) (anyFailed bool, err error) {
	if baseCtx.HTTPClient == nil && opts.HTTPClient == nil {
		baseCtx.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	} else if opts.HTTPClient != nil {
		baseCtx.HTTPClient = opts.HTTPClient
	}
	if baseCtx.Now.IsZero() {
		if !opts.Now.IsZero() {
			baseCtx.Now = opts.Now
		} else {
			baseCtx.Now = time.Now()
		}
	}
	baseCtx.Ctx = rctx

	var all []Result
	for _, def := range catalog {
		if !def.matches(opts.Filter) {
			continue
		}
		results := def.Run(baseCtx)
		all = append(all, results...)
	}

	for _, r := range all {
		if r.Severity == SeverityFail {
			anyFailed = true
			break
		}
	}

	if opts.JSON {
		err = renderJSON(w, all, baseCtx)
	} else {
		err = renderHuman(w, all, baseCtx)
	}
	return anyFailed, err
}

// renderHuman emits the multi-line report that operators read at the
// terminal. One block per result with severity prefix, title, message,
// and the docs URL when populated.
func renderHuman(w io.Writer, results []Result, ctx Context) error {
	if _, err := fmt.Fprintf(w, "conduit doctor — %s — %d check(s) ran\n\n",
		ctx.ConfigPath, len(results)); err != nil {
		return err
	}
	// Group: failures first, then warns, then passes / skips. Within
	// each group keep insertion order (= check execution order) so
	// operators reading top-down see the most-actionable findings up
	// front but the original ordering is preserved within each group.
	groups := map[Severity][]Result{}
	for _, r := range results {
		groups[r.Severity] = append(groups[r.Severity], r)
	}
	order := []Severity{SeverityFail, SeverityWarn, SeverityPass, SeveritySkip}
	for _, sev := range order {
		for _, r := range groups[sev] {
			if _, err := fmt.Fprintf(w, "  [%s] %s — %s (%s)\n",
				r.Severity, r.Title, r.Message, r.ID); err != nil {
				return err
			}
			if r.DocsURL != "" {
				if _, err := fmt.Fprintf(w, "         see: %s\n", r.DocsURL); err != nil {
					return err
				}
			}
		}
	}

	failed := len(groups[SeverityFail])
	warned := len(groups[SeverityWarn])
	if _, err := fmt.Fprintf(w, "\n%d failure(s), %d warning(s), %d passed, %d skipped\n",
		failed, warned, len(groups[SeverityPass]), len(groups[SeveritySkip])); err != nil {
		return err
	}
	return nil
}

// renderJSON emits a single-object envelope with metadata + the result
// list, sorted by ID for stable scripting (sort matters: tests + jq
// queries break under a non-deterministic order).
func renderJSON(w io.Writer, results []Result, ctx Context) error {
	sorted := make([]Result, len(results))
	copy(sorted, results)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})
	envelope := struct {
		Generator  string    `json:"generator"`
		Generated  time.Time `json:"generated"`
		ConfigPath string    `json:"config_path"`
		Results    []Result  `json:"results"`
	}{
		Generator:  "conduit doctor",
		Generated:  ctx.Now.UTC(),
		ConfigPath: ctx.ConfigPath,
		Results:    sorted,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

// docsAnchor returns the canonical URL fragment for a CDT0xxx code.
// The anchor scheme is lowercase, dashed: CDT0001 → cdt0001-config-syntax.
// Until M13's docs site, the URLs land on the GitHub-rendered
// troubleshooting page.
func docsAnchor(code, slug string) string {
	return fmt.Sprintf("%s#%s-%s",
		docsBaseURL,
		strings.ToLower(code),
		strings.ToLower(slug))
}
