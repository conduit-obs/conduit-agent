package doctor

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestDocsAnchorParity is the canonical "docs lag" guard for doctor
// (per [07-testing-and-conformance-plan.md] §Layer 10). It asserts:
//
//  1. Every CDT0xxx ID returned by the production catalog has a
//     matching `## CDT0xxx — <slug>` heading in the troubleshooting doc.
//
//  2. The slug in that heading matches the slug the framework
//     embeds in DocsURL (so a `Result.DocsURL` fragment always
//     resolves on the rendered Markdown / GitHub page).
//
//  3. Every "Reserved" code (CDT0301 / CDT0401 / CDT0402 / CDT0510)
//     called out in the README "Code map" tables also has a section
//     so the reserved anchors operators are linking to in advance
//     don't 404.
//
// The whole point is that adding a new check ID to the catalog
// without writing the docs section breaks CI. Removing a section
// without also removing the corresponding code likewise breaks CI.
//
// Skips on environments that don't have the docs file (e.g. when
// internal/doctor is vendored elsewhere).
func TestDocsAnchorParity(t *testing.T) {
	docPath := findDocsCDTCodes(t)
	if docPath == "" {
		t.Skip("docs/troubleshooting/cdt-codes.md not present in this checkout")
	}
	body, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	docText := string(body)

	// The expected heading shape is:
	//   ## CDT0001 — config-syntax
	// (em dash, slug derived from the title with the same rules
	// `slugFromTitle` uses).
	headingRE := regexp.MustCompile(`(?m)^##\s+(CDT\d{4})\s+—\s+([a-z0-9-]+)\s*$`)
	matches := headingRE.FindAllStringSubmatch(docText, -1)
	headings := make(map[string]string, len(matches))
	for _, m := range matches {
		headings[m[1]] = m[2]
	}

	// Cross-check 1: every framework-emitted code has a matching
	// heading + slug.
	for _, def := range DefaultChecks() {
		gotSlug, ok := headings[def.ID]
		if !ok {
			t.Errorf("%s (%s) has no `## %s — <slug>` heading in %s",
				def.ID, def.Title, def.ID, docPath)
			continue
		}
		wantSlug := slugFromTitle(def.Title)
		if gotSlug != wantSlug {
			t.Errorf("%s heading slug = %q; want %q (matches DocsURL fragment)",
				def.ID, gotSlug, wantSlug)
		}
	}

	// Cross-check 2: CDT0501 is emitted by config_checks but not
	// the catalog directly (it's a routing target for cardinality
	// denylist hits inside CheckConfigSyntax). Verify it has its
	// own section anyway so the docs URL resolves.
	if _, ok := headings[cdt0501ID]; !ok {
		t.Errorf("%s has no doc section even though CheckConfigSyntax emits it for cardinality denylist hits", cdt0501ID)
	}

	// Cross-check 3: reserved codes carved out in the doc must not
	// silently disappear. We assert the four V0 reserved codes have
	// a heading; if the doc gets reorganized in the future we want
	// the test to surface that change explicitly.
	for _, code := range []string{"CDT0301", "CDT0401", "CDT0402", "CDT0510"} {
		if _, ok := headings[code]; !ok {
			t.Errorf("%s is documented as reserved but has no heading; reserved anchors must resolve", code)
		}
	}
}

// findDocsCDTCodes resolves the path to docs/troubleshooting/cdt-codes.md
// from the test binary's runtime location. The test runs from
// internal/doctor/, so the doc lives at ../../docs/troubleshooting.
// We look upward from the source file's directory rather than
// hard-coding a relative path so the test stays robust to test-
// runner cwd quirks (Bazel, GoReleaser sandboxes, etc.).
func findDocsCDTCodes(t *testing.T) string {
	t.Helper()
	_, this, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test source location")
	}
	dir := filepath.Dir(this)
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "docs", "troubleshooting", "cdt-codes.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// TestDocsAnchorURL_ScheneMatchesDocsBase guards the URL generation
// itself. If someone changes docsBaseURL without updating the
// troubleshooting doc location, this test catches it before doctor
// ships a docs URL that 404s.
func TestDocsAnchorURL_BaseMatchesGitHubLayout(t *testing.T) {
	want := "blob/main/docs/troubleshooting/cdt-codes.md"
	if !strings.HasSuffix(docsBaseURL, want) {
		t.Errorf("docsBaseURL = %q; want it to end with %q so anchors resolve on GitHub", docsBaseURL, want)
	}
}
