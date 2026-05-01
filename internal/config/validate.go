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

	case OutputModeGateway:
		if o.Honeycomb != nil {
			v.add("output.honeycomb", `must be omitted when output.mode is "gateway"`)
		}
		if o.Gateway == nil {
			v.add("output.gateway", `required when output.mode is "gateway"`)
			return
		}
		if strings.TrimSpace(o.Gateway.Endpoint) == "" {
			v.add("output.gateway.endpoint", "required; non-empty OTLP/gRPC URL")
		}

	case "":
		v.add("output.mode", `required; one of "honeycomb" or "gateway"`)

	default:
		v.add("output.mode", fmt.Sprintf(`unknown value %q; want one of "honeycomb" or "gateway"`, string(o.Mode)))
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

// Compile-time assertion that *ValidationError satisfies the error interface.
var _ error = (*ValidationError)(nil)
