package doctor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/conduit-obs/conduit-agent/internal/config"
)

// CDT0001: config.syntax — does conduit.yaml parse + pass schema
// validation? This is a wrapper around the same Validate() the loader
// runs at startup, but framed as an isolated doctor check so operators
// debugging "the agent won't start" see the validation errors split
// out from any later (network, port, permission) findings.
const cdt0001ID = "CDT0001"

// CDT0501: config.cardinality_warnings — surfaces RED dimension
// denylist hits as warnings. The schema-time validator already rejects
// these at parse time (they're CDT0501 errors there too), so this
// check is mostly a stable doctor anchor — when it fails today, the
// config didn't load, so we read the parse error and surface the
// denylist hit there.
const cdt0501ID = "CDT0501"

// CheckConfigSyntax reports CDT0001. When ConfigErr is set the parse
// or schema validation failed; we surface the structured ValidationError
// as one Result per issue path so operators can fix them in batch.
func CheckConfigSyntax(ctx Context) []Result {
	if ctx.ConfigErr == nil {
		return []Result{{
			ID:       cdt0001ID,
			Title:    "config.syntax",
			Severity: SeverityPass,
			Message:  fmt.Sprintf("conduit.yaml at %s parses cleanly and passes schema validation.", ctx.ConfigPath),
			DocsURL:  docsAnchor(cdt0001ID, "config-syntax"),
		}}
	}

	var ve *config.ValidationError
	if errors.As(ctx.ConfigErr, &ve) && len(ve.Issues) > 0 {
		results := make([]Result, 0, len(ve.Issues))
		for _, iss := range ve.Issues {
			// Cardinality denylist hits earn the CDT0501 anchor so
			// the troubleshooting doc maps the same finding from
			// schema-time and runtime sides.
			id, slug := classifyConfigIssue(iss)
			results = append(results, Result{
				ID:       id,
				Title:    "config." + topLevelField(iss.Path),
				Severity: SeverityFail,
				Message:  fmt.Sprintf("%s: %s", iss.Path, iss.Message),
				DocsURL:  docsAnchor(id, slug),
			})
		}
		return results
	}

	// Non-validation parse failure (yaml syntax error, missing file,
	// unknown top-level field). Single CDT0001 fail with the raw
	// parser message in the body — those messages are usually
	// pretty good as-is.
	return []Result{{
		ID:       cdt0001ID,
		Title:    "config.syntax",
		Severity: SeverityFail,
		Message:  fmt.Sprintf("conduit.yaml at %s could not be loaded: %v", ctx.ConfigPath, ctx.ConfigErr),
		DocsURL:  docsAnchor(cdt0001ID, "config-syntax"),
	}}
}

// classifyConfigIssue returns the CDT code + docs slug a given issue
// path should anchor against. Most paths anchor at CDT0001 (config
// syntax / required fields are the same docs section). RED denylist
// hits anchor at CDT0501 (separate doc section because the fix is
// "use a different dimension" rather than "fix your YAML").
func classifyConfigIssue(iss config.FieldIssue) (id, slug string) {
	switch {
	case strings.HasPrefix(iss.Path, "metrics.red.") &&
		(strings.Contains(iss.Path, "span_dimensions") ||
			strings.Contains(iss.Path, "extra_resource_dimensions")) &&
		strings.Contains(iss.Message, "denylist"):
		return cdt0501ID, "config-cardinality-warnings"
	default:
		return cdt0001ID, "config-syntax"
	}
}

// topLevelField extracts the leading segment of a YAML path
// ("output.honeycomb.api_key" → "output") for use as the per-Result
// Title. Falls back to the whole path on weird shapes.
func topLevelField(path string) string {
	if i := strings.Index(path, "."); i >= 0 {
		return path[:i]
	}
	return path
}
