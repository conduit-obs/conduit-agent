package config

import (
	"fmt"
	"sort"
	"strings"
)

// ValidationError is a structured error returned by Validate. It accumulates
// every problem found in conduit.yaml so users see all issues in one pass
// rather than fixing them one at a time.
type ValidationError struct {
	Issues []FieldIssue
}

// FieldIssue is one problem at a specific YAML path, e.g. "output.honeycomb.api_key".
type FieldIssue struct {
	Path    string
	Message string
}

func (e *ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "validation error: <no issues recorded>"
	}
	sorted := make([]FieldIssue, len(e.Issues))
	copy(sorted, e.Issues)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	var b strings.Builder
	fmt.Fprintf(&b, "conduit.yaml has %d validation problem(s):", len(sorted))
	for _, iss := range sorted {
		fmt.Fprintf(&b, "\n  - %s: %s", iss.Path, iss.Message)
	}
	return b.String()
}

// Is supports errors.Is(err, &ValidationError{}) for callers that want to
// branch on validation vs. other failures.
func (e *ValidationError) Is(target error) bool {
	_, ok := target.(*ValidationError)
	return ok
}

// Validate checks that the AgentConfig is internally consistent and complete.
// It is called automatically by Load/Parse but is also exported so callers
// (e.g. cmd/preview) can validate a struct they built another way.
func (c *AgentConfig) Validate() error {
	v := &validator{}

	if strings.TrimSpace(c.ServiceName) == "" {
		v.add("service_name", "required; non-empty string")
	}
	if strings.TrimSpace(c.DeploymentEnvironment) == "" {
		v.add("deployment_environment", "required; non-empty string")
	}

	v.validateOutput(&c.Output)
	v.validateProfile(c.Profile)
	v.validateMetrics(c.Metrics)
	v.validateOverrides(c.Overrides)

	if len(v.issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: v.issues}
}

type validator struct {
	issues []FieldIssue
}

func (v *validator) add(path, msg string) {
	v.issues = append(v.issues, FieldIssue{Path: path, Message: msg})
}

func (v *validator) validateOutput(o *Output) {
	switch o.Mode {
	case OutputModeHoneycomb:
		if o.OTLP != nil {
			v.add("output.otlp", `must be omitted when output.mode is "honeycomb"`)
		}
		if o.Gateway != nil {
			v.add("output.gateway", `must be omitted when output.mode is "honeycomb"`)
		}
		if o.Honeycomb == nil {
			v.add("output.honeycomb", `required when output.mode is "honeycomb"`)
			return
		}
		if strings.TrimSpace(o.Honeycomb.APIKey) == "" {
			v.add("output.honeycomb.api_key", "required; non-empty string (may use ${env:NAME})")
		}

	case OutputModeOTLP:
		if o.Honeycomb != nil {
			v.add("output.honeycomb", `must be omitted when output.mode is "otlp"`)
		}
		if o.Gateway != nil {
			v.add("output.gateway", `must be omitted when output.mode is "otlp"`)
		}
		if o.OTLP == nil {
			v.add("output.otlp", `required when output.mode is "otlp"`)
			return
		}
		if strings.TrimSpace(o.OTLP.Endpoint) == "" {
			v.add("output.otlp.endpoint", "required; non-empty OTLP/HTTP URL (e.g. https://otlp.example.com)")
		}

	case OutputModeGateway:
		if o.Honeycomb != nil {
			v.add("output.honeycomb", `must be omitted when output.mode is "gateway"`)
		}
		if o.OTLP != nil {
			v.add("output.otlp", `must be omitted when output.mode is "gateway"`)
		}
		if o.Gateway == nil {
			v.add("output.gateway", `required when output.mode is "gateway"`)
			return
		}
		if strings.TrimSpace(o.Gateway.Endpoint) == "" {
			v.add("output.gateway.endpoint", "required; non-empty OTLP/gRPC URL")
		}

	case "":
		v.add("output.mode", `required; one of "honeycomb", "otlp", or "gateway"`)

	default:
		v.add("output.mode", fmt.Sprintf(`unknown value %q; want one of "honeycomb", "otlp", or "gateway"`, string(o.Mode)))
	}
}

func (v *validator) validateProfile(p *Profile) {
	// applyDefaults guarantees a non-nil Profile, but be defensive in case a
	// caller invokes Validate on a hand-built struct.
	if p == nil {
		return
	}
	switch p.Mode {
	case ProfileModeAuto, ProfileModeLinux, ProfileModeDarwin, ProfileModeDocker, ProfileModeK8s, ProfileModeNone:
		// known mode
	default:
		v.add("profile.mode", fmt.Sprintf(`unknown value %q; want one of "auto", "linux", "darwin", "docker", "k8s", or "none"`, string(p.Mode)))
	}
}

// validateMetrics enforces the RED-from-spans contract: cardinality limit
// must be sane, user-supplied dimensions must not be on the denylist that
// would tip the span_metrics connector into per-request cardinality.
//
// Denylist hits map to error code CDT0501 in the doctor catalog (M11);
// the message intentionally calls that out so search-engine-style
// debugging finds the canonical doc page.
func (v *validator) validateMetrics(m *Metrics) {
	if m == nil || m.RED == nil {
		return
	}
	red := m.RED
	if red.CardinalityLimit < 0 {
		v.add("metrics.red.cardinality_limit",
			fmt.Sprintf("must be >= 0; got %d", red.CardinalityLimit))
	}
	for i, name := range red.SpanDimensions {
		if reason, blocked := REDDimensionDenylist[name]; blocked {
			v.add(fmt.Sprintf("metrics.red.span_dimensions[%d]", i),
				fmt.Sprintf(`%q is on the cardinality denylist (CDT0501): %s. See ADR-0006.`, name, reason))
		}
	}
	for i, name := range red.ExtraResourceDimensions {
		if reason, blocked := REDDimensionDenylist[name]; blocked {
			v.add(fmt.Sprintf("metrics.red.extra_resource_dimensions[%d]", i),
				fmt.Sprintf(`%q is on the cardinality denylist (CDT0501): %s. See ADR-0006.`, name, reason))
		}
	}
}

// validateOverrides checks the structural shape of the overrides block
// without trying to validate the contents — those are upstream OTel
// Collector concerns and the collector's own resolver / config-unmarshaler
// will reject anything malformed at startup with a clearer message than
// we could produce here.
//
// What we DO catch: top-level keys outside the standard collector vocab
// (receivers / processors / exporters / connectors / extensions /
// service). Anything else is almost certainly a typo at the
// conduit.yaml level, not a deliberate collector escape hatch — call it
// out before the operator wonders why their override silently doesn't
// apply.
func (v *validator) validateOverrides(overrides map[string]any) {
	if len(overrides) == 0 {
		return
	}
	allowed := map[string]bool{
		"receivers":  true,
		"processors": true,
		"exporters":  true,
		"connectors": true,
		"extensions": true,
		"service":    true,
	}
	for key := range overrides {
		if !allowed[key] {
			v.add("overrides."+key, fmt.Sprintf(`unknown top-level key %q; expected one of receivers / processors / exporters / connectors / extensions / service. See ADR-0012 (docs/adr/adr-0012.md) for the overrides escape-hatch design.`, key))
		}
	}
}

// Compile-time assertion that *ValidationError satisfies the error interface.
var _ error = (*ValidationError)(nil)
