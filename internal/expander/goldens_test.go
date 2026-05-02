package expander

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conduit-obs/conduit-agent/internal/config"
)

// updateGoldens, when set via `go test -update`, rewrites each
// expected.yaml under testdata/goldens/<case>/ with the freshly-
// rendered output. Keeps the developer workflow for adding a new
// golden case down to "drop a conduit.yaml in a new dir, run
// `make update-goldens`, commit". The implementation is
// `find ./internal/expander/testdata/goldens -name conduit.yaml |
// xargs -I{} dirname {} | xargs ls -la` style: the harness reads
// every dir under testdata/goldens/.
//
// Per [07-testing-and-conformance-plan.md] §Layer 2, the golden
// matrix is the second-line defense against silent expander
// regressions — unit tests catch broken templates, goldens catch
// "the rendering changed but the unit tests didn't notice because
// they only assert structural shape, not exact byte equality."
var updateGoldens = flag.Bool("update", false, "rewrite testdata/goldens/<case>/expected.yaml with the current renderer output")

// TestExpand_Goldens iterates every directory under testdata/
// goldens/, loads conduit.yaml as the input, expands it through
// the production Expand() pipeline, and diffs against expected.yaml.
// Diffs fail the test with a "run go test -update" hint.
//
// Cases are intentionally hand-curated for the most common shipping
// configurations rather than exhaustive — full coverage of the
// (platform × output × RED × queue) cross-product would be 96 cases
// and most of those add no signal. See the README in the goldens
// directory for the slice list and what each case proves.
func TestExpand_Goldens(t *testing.T) {
	cases, err := os.ReadDir(goldensRoot(t))
	if err != nil {
		t.Fatalf("read goldens dir: %v", err)
	}
	for _, e := range cases {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			runGoldenCase(t, name)
		})
	}
}

func runGoldenCase(t *testing.T, name string) {
	t.Helper()
	caseDir := filepath.Join(goldensRoot(t), name)
	inputPath := filepath.Join(caseDir, "conduit.yaml")
	expectedPath := filepath.Join(caseDir, "expected.yaml")

	in, err := os.Open(inputPath)
	if err != nil {
		t.Fatalf("open %s: %v", inputPath, err)
	}
	defer func() { _ = in.Close() }()
	cfg, err := config.Parse(in)
	if err != nil {
		t.Fatalf("parse %s: %v", inputPath, err)
	}
	got, err := ExpandWithWarnings(cfg, io.Discard)
	if err != nil {
		t.Fatalf("expand %s: %v", inputPath, err)
	}
	got = normalizeRenderedYAML(got)

	if *updateGoldens {
		if err := os.WriteFile(expectedPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", expectedPath, err)
		}
		t.Logf("updated %s (%d bytes)", expectedPath, len(got))
		return
	}

	wantBytes, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read %s: %v (run `go test ./internal/expander -update` to seed)", expectedPath, err)
	}
	want := normalizeRenderedYAML(string(wantBytes))
	if got != want {
		t.Errorf("rendered output for case %q differs from %s\n\nGot:\n%s\n\nWant:\n%s\n\nIf this change is intended, run:\n  go test ./internal/expander -run TestExpand_Goldens -update",
			name, expectedPath, got, want)
	}
}

// normalizeRenderedYAML strips trailing whitespace + collapses
// multiple blank lines so byte-identical comparisons aren't
// derailed by editor reflows or CRLF line endings. The semantic
// content is what we care about; whitespace at end-of-file,
// trailing-spaces on a commented line, and Windows-style \r\n
// (which can appear when the runner has core.autocrlf=true and
// .gitattributes hasn't taken effect on a stale checkout) are
// noise.
func normalizeRenderedYAML(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		out = append(out, strings.TrimRight(ln, " \t"))
	}
	joined := strings.Join(out, "\n")
	// Collapse any final run of newlines down to one trailing newline.
	for strings.HasSuffix(joined, "\n\n") {
		joined = joined[:len(joined)-1]
	}
	if !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}
	return joined
}

func goldensRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "goldens")
}
