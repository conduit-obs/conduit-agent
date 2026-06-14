package expander

import (
	"strings"
	"testing"

	"github.com/conduit-obs/conduit-agent/internal/config"
)

// TestExpand_RED_SpecScenarios pins the RED-from-spans behaviors the
// product spec enumerates to the concrete rendered-config guarantees
// the expander must emit to produce them. Conduit is a config generator,
// not a running collector, so each scenario asserts the config property
// that drives the runtime behavior rather than spinning up a pipeline.
// See docs/adr/adr-0022.md for the design these scenarios lock in.
func TestExpand_RED_SpecScenarios(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	redProcs := pipelineProcessors(t, out, "traces/red")
	redExp := pipelineExporters(t, out, "traces/red")
	mainExp := pipelineExporters(t, out, "traces")

	t.Run("successful HTTP server request counted with safe dimensions", func(t *testing.T) {
		// Server spans pass filter/red_requests and feed the request
		// counter + duration histogram, dimensioned by the templated
		// route and bounded method/status attributes. http.route is the
		// templated form; raw path / url / query attributes are never
		// dimensions (they're on the cardinality denylist, ADR-0006).
		if !contains(redProcs, "filter/red_requests") {
			t.Fatalf("traces/red must run filter/red_requests; got %v", redProcs)
		}
		mustContain(t, out, []string{
			"- name: http.route",
			"- name: http.request.method",
			"- name: http.response.status_code",
		})
		mustNotContain(t, out, []string{
			"- name: http.target",
			"- name: http.path",
			"- name: url.full",
		})
	})

	t.Run("errored HTTP server request is distinguishable", func(t *testing.T) {
		// Errors are sliced out of the request counter via the
		// connector's built-in status.code dimension plus the HTTP
		// status-code dimensions (calls{status.code=STATUS_CODE_ERROR}) —
		// the idiomatic span-metrics RED error signal the checked-in
		// dashboards already use. No separate error counter is emitted.
		mustContain(t, out, []string{
			"- name: http.response.status_code",
			"- name: http.status_code",
		})
	})

	t.Run("request duration is a histogram", func(t *testing.T) {
		mustContain(t, out, []string{
			"histogram:",
			"buckets: [10ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s]",
		})
	})

	t.Run("request without http.route still counts, no unbounded fallback", func(t *testing.T) {
		// http.route is listed as a bare dimension with no `default:`
		// value, so a span lacking it (an RPC or messaging consumer)
		// simply omits the dimension rather than fabricating a
		// high-cardinality fallback. Asserting the next line is the
		// following dimension proves there's no `default:` clause.
		if !strings.Contains(out, "      - name: http.route\n      - name: http.request.method") {
			t.Error("http.route must be a bare dimension (no default fallback) so spans without a route omit it instead of inventing a value")
		}
	})

	t.Run("non-request span does not emit RED metrics", func(t *testing.T) {
		// filter/red_requests drops every span that isn't Server or
		// Consumer (client, internal, producer) before the connector
		// counts it.
		mustContain(t, out, []string{
			`- 'kind != SPAN_KIND_SERVER and kind != SPAN_KIND_CONSUMER'`,
		})
	})

	t.Run("RED derivation isolated from the trace egress path", func(t *testing.T) {
		// The connector is fed only by traces/red; the main traces
		// pipeline ships every span and never tees the connector. A
		// sampler an operator adds to traces.processors via overrides
		// therefore reduces trace volume while leaving RED accurate.
		if !equalSet(redExp, []string{"span_metrics"}) {
			t.Errorf("traces/red exporters = %v; want [span_metrics]", redExp)
		}
		if contains(mainExp, "span_metrics") {
			t.Errorf("main traces pipeline must not tee span_metrics; got %v", mainExp)
		}
	})
}

// TestExpand_RED_SurvivesTraceSampling covers the "sampled trace/event
// with metrics still emitted accurately" scenario. Refinery (tail
// sampling) sits only on the main traces pipeline; RED derives from
// traces/red, which shares the otlp receiver and feeds the connector
// before any sampling step — so RED stays at 100% even when traces are
// sampled downstream (ADR-0005 + ADR-0022).
func TestExpand_RED_SurvivesTraceSampling(t *testing.T) {
	cfg := honeycomb(&config.Profile{Mode: config.ProfileModeNone})
	cfg.Output.Honeycomb.Traces = &config.HoneycombTraces{
		ViaRefinery: &config.RefineryRouting{
			Endpoint: "refinery.observability.svc:4317",
		},
	}
	out, err := Expand(cfg)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	if got := pipelineExporters(t, out, "traces"); !contains(got, "otlp/refinery") {
		t.Fatalf("main traces pipeline must route to refinery; got %v", got)
	}
	if got := pipelineExporters(t, out, "traces/red"); !equalSet(got, []string{"span_metrics"}) {
		t.Errorf("traces/red must feed span_metrics regardless of refinery routing; got %v", got)
	}
	// Refinery must not appear anywhere in the RED derivation path.
	if got := pipelineProcessors(t, out, "traces/red"); contains(got, "otlp/refinery") {
		t.Errorf("refinery must not sit in the RED derivation path; got %v", got)
	}
}
