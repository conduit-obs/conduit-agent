package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// fakeCheck is a Check that always returns the configured Result list.
// Used to drive [Run] tests without the real production catalog.
func fakeCheck(results []Result) Check {
	return func(_ Context) []Result { return results }
}

func TestSeverity_StringAndJSON(t *testing.T) {
	tests := []struct {
		s        Severity
		want     string
		wantJSON string
	}{
		{SeverityPass, "PASS", `"pass"`},
		{SeverityWarn, "WARN", `"warn"`},
		{SeverityFail, "FAIL", `"fail"`},
		{SeveritySkip, "SKIP", `"skip"`},
	}
	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("String(%d): got %q, want %q", tc.s, got, tc.want)
		}
		b, _ := tc.s.MarshalJSON()
		if string(b) != tc.wantJSON {
			t.Errorf("MarshalJSON(%d): got %s, want %s", tc.s, b, tc.wantJSON)
		}
	}
}

func TestDefinition_Matches(t *testing.T) {
	def := Definition{ID: "CDT0001", Title: "config.syntax"}

	cases := []struct {
		filter []string
		want   bool
	}{
		{nil, true},
		{[]string{}, true},
		{[]string{"CDT0001"}, true},
		{[]string{"config.syntax"}, true},
		{[]string{"config"}, true},  // prefix match: config → config.* OK
		{[]string{"output"}, false}, // no match
		{[]string{"CDT9999", "config.syntax"}, true},
	}
	for _, tc := range cases {
		if got := def.matches(tc.filter); got != tc.want {
			t.Errorf("matches(%v) = %v; want %v", tc.filter, got, tc.want)
		}
	}
}

func TestRun_AnyFailReturnsTrue(t *testing.T) {
	catalog := []Definition{
		{ID: "CDT0001", Title: "ok", Run: fakeCheck([]Result{{ID: "CDT0001", Severity: SeverityPass}})},
		{ID: "CDT0002", Title: "bad", Run: fakeCheck([]Result{{ID: "CDT0002", Severity: SeverityFail}})},
	}
	var buf bytes.Buffer
	failed, err := Run(context.Background(), catalog, Context{ConfigPath: "x"}, &buf, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !failed {
		t.Errorf("Run with one FAIL should return failed=true")
	}
}

func TestRun_AllPassReturnsFalse(t *testing.T) {
	catalog := []Definition{
		{ID: "CDT0001", Title: "ok", Run: fakeCheck([]Result{{ID: "CDT0001", Severity: SeverityPass}})},
		{ID: "CDT0002", Title: "warn-only", Run: fakeCheck([]Result{{ID: "CDT0002", Severity: SeverityWarn}})},
	}
	var buf bytes.Buffer
	failed, err := Run(context.Background(), catalog, Context{ConfigPath: "x"}, &buf, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if failed {
		t.Errorf("Run with no FAIL should return failed=false (warn does not block)")
	}
}

func TestRun_FilterAppliesByID(t *testing.T) {
	catalog := []Definition{
		{ID: "CDT0001", Title: "config.syntax", Run: fakeCheck([]Result{{ID: "CDT0001", Title: "config.syntax", Severity: SeverityPass}})},
		{ID: "CDT0101", Title: "output.endpoint_reachable", Run: fakeCheck([]Result{{ID: "CDT0101", Title: "output.endpoint_reachable", Severity: SeverityFail}})},
	}
	var buf bytes.Buffer
	failed, err := Run(context.Background(), catalog, Context{ConfigPath: "x"}, &buf, Options{Filter: []string{"CDT0001"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if failed {
		t.Errorf("filter excluded the failing check; failed should be false")
	}
	out := buf.String()
	if !strings.Contains(out, "CDT0001") {
		t.Errorf("output missing CDT0001: %s", out)
	}
	if strings.Contains(out, "CDT0101") {
		t.Errorf("output should not contain filtered-out CDT0101: %s", out)
	}
}

func TestRun_FilterByTitlePrefix(t *testing.T) {
	catalog := []Definition{
		{ID: "CDT0001", Title: "config.syntax", Run: fakeCheck([]Result{{ID: "CDT0001", Title: "config.syntax", Severity: SeverityPass}})},
		{ID: "CDT0101", Title: "output.endpoint_reachable", Run: fakeCheck([]Result{{ID: "CDT0101", Title: "output.endpoint_reachable", Severity: SeverityPass}})},
		{ID: "CDT0102", Title: "output.auth", Run: fakeCheck([]Result{{ID: "CDT0102", Title: "output.auth", Severity: SeverityPass}})},
	}
	var buf bytes.Buffer
	if _, err := Run(context.Background(), catalog, Context{ConfigPath: "x"}, &buf, Options{Filter: []string{"output"}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "config.syntax") {
		t.Errorf("output should NOT contain config.syntax under filter=output; got %s", out)
	}
	if !strings.Contains(out, "output.endpoint_reachable") || !strings.Contains(out, "output.auth") {
		t.Errorf("filter=output should match every output.*; got %s", out)
	}
}

func TestRun_JSONShape(t *testing.T) {
	catalog := []Definition{
		{ID: "CDT0002", Title: "b", Run: fakeCheck([]Result{{ID: "CDT0002", Title: "b", Severity: SeverityPass, Message: "ok"}})},
		{ID: "CDT0001", Title: "a", Run: fakeCheck([]Result{{ID: "CDT0001", Title: "a", Severity: SeverityFail, Message: "broken"}})},
	}
	var buf bytes.Buffer
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	failed, err := Run(context.Background(), catalog, Context{ConfigPath: "test.yaml"}, &buf, Options{
		JSON: true,
		Now:  now,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !failed {
		t.Errorf("Run with FAIL should report failed=true")
	}

	var envelope struct {
		Generator  string    `json:"generator"`
		Generated  time.Time `json:"generated"`
		ConfigPath string    `json:"config_path"`
		Results    []Result  `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal JSON: %v\noutput:\n%s", err, buf.String())
	}
	if envelope.Generator != "conduit doctor" {
		t.Errorf("Generator: got %q, want %q", envelope.Generator, "conduit doctor")
	}
	if envelope.ConfigPath != "test.yaml" {
		t.Errorf("ConfigPath: got %q", envelope.ConfigPath)
	}
	if !envelope.Generated.Equal(now) {
		t.Errorf("Generated: got %v, want %v", envelope.Generated, now)
	}
	// Results sort by ID; CDT0001 must come before CDT0002 even though
	// the catalog ran them in the reverse order.
	if len(envelope.Results) != 2 || envelope.Results[0].ID != "CDT0001" || envelope.Results[1].ID != "CDT0002" {
		t.Errorf("Results not sorted by ID: %+v", envelope.Results)
	}
}

func TestRun_HumanGroupsByseverity(t *testing.T) {
	catalog := []Definition{
		{ID: "CDT0001", Title: "ok-1", Run: fakeCheck([]Result{{ID: "CDT0001", Title: "ok-1", Severity: SeverityPass, Message: "pass1"}})},
		{ID: "CDT0002", Title: "warn", Run: fakeCheck([]Result{{ID: "CDT0002", Title: "warn", Severity: SeverityWarn, Message: "be careful"}})},
		{ID: "CDT0003", Title: "fail", Run: fakeCheck([]Result{{ID: "CDT0003", Title: "fail", Severity: SeverityFail, Message: "broken"}})},
	}
	var buf bytes.Buffer
	if _, err := Run(context.Background(), catalog, Context{ConfigPath: "x"}, &buf, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	failIdx := strings.Index(out, "fail")
	warnIdx := strings.Index(out, "warn")
	passIdx := strings.Index(out, "ok-1")
	if failIdx < 0 || warnIdx < 0 || passIdx < 0 {
		t.Fatalf("expected all severities to appear; got:\n%s", out)
	}
	// Order: FAIL first, then WARN, then PASS.
	if failIdx >= warnIdx || warnIdx >= passIdx {
		t.Errorf("expected FAIL < WARN < PASS in output order; failIdx=%d warnIdx=%d passIdx=%d\n%s",
			failIdx, warnIdx, passIdx, out)
	}
	if !strings.Contains(out, "1 failure(s), 1 warning(s), 1 passed, 0 skipped") {
		t.Errorf("expected summary line; got:\n%s", out)
	}
}

func TestDefaultChecks_AllHaveDocsAnchors(t *testing.T) {
	for _, def := range DefaultChecks() {
		if def.ID == "" || def.Title == "" || def.Run == nil {
			t.Errorf("DefaultChecks entry missing required field: %+v", def)
		}
	}
}
