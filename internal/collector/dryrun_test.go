package collector

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"go.opentelemetry.io/collector/otelcol"

	"github.com/conduit-obs/conduit-agent/internal/config"
	"github.com/conduit-obs/conduit-agent/internal/expander"
)

// TestDryRun_RenderedProfilesLoad feeds the expander's rendered YAML for
// every profile mode through the REAL embedded collector's config
// resolution (otelcol.Collector.DryRun) with the real factory map from
// components.go. This is the guard between "the expander tests pass"
// and "the binary boots": a processor referenced by a pipeline but
// missing from builder-config.yaml, a typoed config key on any
// component (strict unmarshal), or an invalid component config
// (Validate()) all fail here rather than on a customer host.
//
// DryRun goes beyond schema checks: it builds every pipeline with the
// real factories, which parses transform/logs's OTTL statements and
// builds the filelog/journald stanza operator chains. The trade-off is
// that platform-gated components (journald, windowseventlog, obi,
// hostmetrics root_path) refuse creation off their platform, so each
// OS validates the profile set it can actually create — CI's matrix
// (ubuntu / macos / windows legs) composes full coverage.
func TestDryRun_RenderedProfilesLoad(t *testing.T) {
	modes := []config.ProfileMode{
		config.ProfileModeNone,
		config.ProfileModeK8sCluster,
	}
	switch runtime.GOOS {
	case "linux":
		modes = append(modes, config.ProfileModeLinux, config.ProfileModeDocker, config.ProfileModeK8s)
	case "darwin":
		modes = append(modes, config.ProfileModeDarwin)
	case "windows":
		modes = append(modes, config.ProfileModeWindows)
	}
	// One shared root_path shim for every subtest that needs one: the
	// hostmetrics receiver records root_path in PROCESS-GLOBAL state
	// (gopsutilenv) and rejects a second component configured with a
	// different value ("inconsistent root_path configuration detected
	// among components"), so per-subtest t.TempDir() values would
	// conflict across the docker and k8s runs.
	sharedRoot := t.TempDir()
	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			// Two config-validation checks depend on the surrounding
			// environment rather than the rendered YAML's shape: the
			// docker/k8s hostmetrics fragments' root_path must be an
			// existing directory (/hostfs is bind-mounted only in real
			// deployments), and k8sattributes requires K8S_NODE_NAME
			// (the chart wires it via the downward API). Shim both so
			// DryRun still validates everything else those profiles
			// render; the kind/helm and docker integration smokes
			// exercise the real environments.
			extra := ""
			switch mode {
			case config.ProfileModeDocker, config.ProfileModeK8s:
				extra = fmt.Sprintf(`
overrides:
  receivers:
    hostmetrics:
      root_path: %s
`, sharedRoot)
				t.Setenv("K8S_NODE_NAME", "dryrun-node")
			}
			yamlDoc := fmt.Sprintf(`
service_name: dryrun
deployment_environment: test
profile:
  mode: %s
output:
  mode: honeycomb
  honeycomb:
    api_key: hcaik_dryrun_test_key
%s`, mode, extra)
			cfg, err := config.Parse(strings.NewReader(yamlDoc))
			if err != nil {
				t.Fatalf("config.Parse: %v", err)
			}
			sources, err := expander.ExpandConfigs(cfg)
			if err != nil {
				t.Fatalf("expander.ExpandConfigs: %v", err)
			}
			settings := DefaultSettings(DefaultBuildInfo)
			for _, src := range sources {
				settings.ConfigProviderSettings.ResolverSettings.URIs = append(
					settings.ConfigProviderSettings.ResolverSettings.URIs, "yaml:"+src)
			}
			col, err := otelcol.NewCollector(settings)
			if err != nil {
				t.Fatalf("otelcol.NewCollector: %v", err)
			}
			if err := col.DryRun(context.Background()); err != nil {
				t.Fatalf("DryRun failed for profile.mode=%s:\n%v", mode, err)
			}
		})
	}
}
