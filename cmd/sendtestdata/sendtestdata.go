// Package sendtestdata implements `conduit send-test-data`, which emits
// synthetic OTLP trace telemetry to a running collector over OTLP/HTTP.
//
// It exists so smoke tests, the demo, and CI E2E checks can drive a real
// span through a Conduit pipeline without standing up an instrumented
// application. The agent's debug exporter logs every received batch, so a
// caller can assert delivery by grepping the agent's logs (see
// scripts/smoke_otlp.sh and the integration workflow). Keeping the
// generator in the conduit binary itself means the same tool works on
// Linux, macOS, and Windows with no curl / PowerShell payload duplication.
package sendtestdata

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

const long = `Generate synthetic OTLP traces and POST them to a running collector's
OTLP/HTTP traces endpoint (http://<target>/v1/traces).

Spans are emitted as request-like (SPAN_KIND_SERVER) HTTP spans carrying
the RED dimensions Conduit derives metrics from (http.request.method,
http.route, http.response.status_code), so the same call exercises both the
trace path and the span-metrics (RED) path.

Profiles:
  default   successful HTTP server spans (status OK, http 200)
  red       a mix of 200 and 500 spans so error rate is non-zero

Used by conduit's smoke tests and the integration workflow to assert a
trace reaches the agent (the debug exporter logs each received batch).`

// supportedProfiles is the closed set of --profile values. Unknown values
// fail loud rather than silently falling back, so a typo in CI surfaces as
// an error instead of a green run that tested nothing.
var supportedProfiles = map[string]bool{"default": true, "red": true}

// NewCommand returns the `conduit send-test-data` command.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send-test-data",
		Short: "Emit synthetic OTLP telemetry for smoke tests and demos",
		Long:  long,
		Args:  cobra.NoArgs,
		RunE:  runE,
	}

	cmd.Flags().Int("rate", 10, "spans to send per one-second batch")
	cmd.Flags().Duration("duration", time.Minute, "how long to keep sending for")
	cmd.Flags().String("target", "127.0.0.1:4318", "OTLP/HTTP host:port to send to")
	cmd.Flags().String("profile", "default", "test data profile (default|red)")

	return cmd
}

func runE(cmd *cobra.Command, _ []string) error {
	rate, _ := cmd.Flags().GetInt("rate")
	duration, _ := cmd.Flags().GetDuration("duration")
	target, _ := cmd.Flags().GetString("target")
	profile, _ := cmd.Flags().GetString("profile")

	if rate < 1 {
		return fmt.Errorf("send-test-data: --rate must be >= 1, got %d", rate)
	}
	if !supportedProfiles[profile] {
		return fmt.Errorf("send-test-data: unknown --profile %q (want default|red)", profile)
	}

	url := "http://" + target + "/v1/traces"
	client := &http.Client{Timeout: 10 * time.Second}
	out := cmd.OutOrStdout()
	deadline := time.Now().Add(duration)

	// Do-while: always send at least one batch so a sub-second --duration
	// (the common smoke case) still delivers a span.
	sent := 0
	for {
		body, err := buildBatch(profile, rate)
		if err != nil {
			return fmt.Errorf("send-test-data: build batch: %w", err)
		}
		if err := post(cmd.Context(), client, url, body); err != nil {
			return fmt.Errorf("send-test-data: POST %s: %w", url, err)
		}
		sent += rate

		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(time.Until(minTime(time.Now().Add(time.Second), deadline))):
		}
	}

	fmt.Fprintf(out, "send-test-data: sent %d spans (profile=%s) to %s\n", sent, profile, url)
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func post(ctx context.Context, client *http.Client, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("unexpected status %s: %s", resp.Status, bytes.TrimSpace(snippet))
	}
	return nil
}

// --- OTLP/HTTP JSON payload ---------------------------------------------
//
// The OTLP spec mandates hex-encoded trace_id / span_id in the JSON
// encoding (a documented exception to proto3's base64 bytes mapping) and
// string-typed uint64 / int64 fields. These structs encode exactly that
// shape so the upstream otlpreceiver accepts the payload unchanged.

type otlpPayload struct {
	ResourceSpans []resourceSpans `json:"resourceSpans"`
}

type resourceSpans struct {
	Resource   resource     `json:"resource"`
	ScopeSpans []scopeSpans `json:"scopeSpans"`
}

type resource struct {
	Attributes []attribute `json:"attributes"`
}

type scopeSpans struct {
	Scope scope  `json:"scope"`
	Spans []span `json:"spans"`
}

type scope struct {
	Name string `json:"name"`
}

type span struct {
	TraceID           string      `json:"traceId"`
	SpanID            string      `json:"spanId"`
	Name              string      `json:"name"`
	Kind              int         `json:"kind"`
	StartTimeUnixNano string      `json:"startTimeUnixNano"`
	EndTimeUnixNano   string      `json:"endTimeUnixNano"`
	Attributes        []attribute `json:"attributes"`
	Status            status      `json:"status"`
}

type status struct {
	Code int `json:"code"`
}

type attribute struct {
	Key   string `json:"key"`
	Value value  `json:"value"`
}

type value struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *string `json:"intValue,omitempty"`
}

func strVal(s string) value { return value{StringValue: &s} }
func intVal(i int) value    { s := strconv.Itoa(i); return value{IntValue: &s} }

const (
	spanKindServer  = 2
	statusCodeOK    = 1
	statusCodeError = 2
)

// buildBatch renders `count` synthetic server spans as a single OTLP/HTTP
// JSON request body. In the red profile roughly one span in four carries a
// 500 / ERROR status so the derived error rate is non-zero.
func buildBatch(profile string, count int) ([]byte, error) {
	now := time.Now()
	spans := make([]span, 0, count)
	for i := 0; i < count; i++ {
		traceID, err := randHex(16)
		if err != nil {
			return nil, err
		}
		spanID, err := randHex(8)
		if err != nil {
			return nil, err
		}

		statusCode := 200
		spanStatus := statusCodeOK
		if profile == "red" && i%4 == 0 {
			statusCode = 500
			spanStatus = statusCodeError
		}

		start := now.Add(-50 * time.Millisecond)
		spans = append(spans, span{
			TraceID:           traceID,
			SpanID:            spanID,
			Name:              "GET /api/items",
			Kind:              spanKindServer,
			StartTimeUnixNano: strconv.FormatInt(start.UnixNano(), 10),
			EndTimeUnixNano:   strconv.FormatInt(now.UnixNano(), 10),
			Attributes: []attribute{
				{Key: "http.request.method", Value: strVal("GET")},
				{Key: "http.route", Value: strVal("/api/items")},
				{Key: "http.response.status_code", Value: intVal(statusCode)},
			},
			Status: status{Code: spanStatus},
		})
	}

	payload := otlpPayload{
		ResourceSpans: []resourceSpans{{
			Resource: resource{Attributes: []attribute{
				{Key: "service.name", Value: strVal("conduit-send-test-data")},
			}},
			ScopeSpans: []scopeSpans{{
				Scope: scope{Name: "conduit.send-test-data"},
				Spans: spans,
			}},
		}},
	}
	return json.Marshal(payload)
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
