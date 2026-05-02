package profiles

import (
	"errors"
	"strings"
	"testing"
)

func TestAvailable_HasShippedPlatforms(t *testing.T) {
	got := Available()
	want := map[string]bool{"linux": true, "darwin": true, "k8s": true}
	if len(got) < len(want) {
		t.Fatalf("Available: got %v, want at least %v", got, keys(want))
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("Available: unexpected platform %q (this is fine if you just added it; update the test)", p)
		}
	}
	for w := range want {
		found := false
		for _, p := range got {
			if p == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Available: missing %q; got %v", w, got)
		}
	}
}

func TestHas(t *testing.T) {
	cases := []struct {
		platform string
		signal   Signal
		want     bool
	}{
		{"linux", SignalHostMetrics, true},
		{"linux", SignalSystemLogs, true},
		{"darwin", SignalHostMetrics, true},
		{"darwin", SignalSystemLogs, true},
		{"k8s", SignalHostMetrics, true},
		{"k8s", SignalKubelet, true},
		{"k8s", SignalSystemLogs, true},
		// kubelet is k8s-only; host platforms have no analogue.
		{"linux", SignalKubelet, false},
		{"darwin", SignalKubelet, false},
		{"plan9", SignalHostMetrics, false},
	}
	for _, tc := range cases {
		t.Run(tc.platform+"/"+string(tc.signal), func(t *testing.T) {
			if got := Has(tc.platform, tc.signal); got != tc.want {
				t.Errorf("Has(%q, %q): got %v, want %v", tc.platform, tc.signal, got, tc.want)
			}
		})
	}
}

func TestLoad_ContentSanity(t *testing.T) {
	cases := []struct {
		platform string
		signal   Signal
		mustHave []string
	}{
		{"linux", SignalHostMetrics, []string{"hostmetrics:", "scrapers:", "processes:"}},
		{"linux", SignalSystemLogs, []string{"filelog/system:", "journald:"}},
		{"darwin", SignalHostMetrics, []string{"hostmetrics:", "scrapers:"}},
		{"darwin", SignalSystemLogs, []string{"filelog/system:", "/var/log/system.log"}},
		{"k8s", SignalHostMetrics, []string{"hostmetrics:", "root_path: /hostfs", "scrapers:", "system.cpu.utilization"}},
		{"k8s", SignalKubelet, []string{"kubeletstats:", "auth_type: serviceAccount", "K8S_NODE_NAME"}},
		{"k8s", SignalSystemLogs, []string{"filelog/k8s:", "/var/log/pods", "type: container"}},
	}
	for _, tc := range cases {
		t.Run(tc.platform+"/"+string(tc.signal), func(t *testing.T) {
			body, err := Load(tc.platform, tc.signal)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			for _, s := range tc.mustHave {
				if !strings.Contains(body, s) {
					t.Errorf("Load(%q, %q): missing %q in body:\n%s", tc.platform, tc.signal, s, body)
				}
			}
		})
	}
}

// keys returns the keys of a string-keyed map, sorted, so test failure
// messages are stable and don't depend on Go's randomized map iteration.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestLoad_DarwinHasNoJournald(t *testing.T) {
	body, err := Load("darwin", SignalSystemLogs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Contains(body, "journald:") {
		t.Errorf("darwin/logs.yaml should not include journald (macOS has none); body:\n%s", body)
	}
}

func TestLoad_UnknownPlatform(t *testing.T) {
	_, err := Load("plan9", SignalHostMetrics)
	if err == nil {
		t.Fatal("Load: want error for unknown platform")
	}
	if !errors.Is(err, ErrUnknownPlatform) {
		t.Errorf("Load: want ErrUnknownPlatform, got %v", err)
	}
}

func TestLoad_UnknownSignal(t *testing.T) {
	// linux is a real platform but has no kubeletstats fragment — that
	// signal is k8s-only.
	_, err := Load("linux", SignalKubelet)
	if err == nil {
		t.Fatal("Load: want error for unknown signal")
	}
	if !errors.Is(err, ErrUnknownSignal) {
		t.Errorf("Load: want ErrUnknownSignal, got %v", err)
	}
}

func TestDetectPlatform_RuntimeGOOS(t *testing.T) {
	got := DetectPlatform()
	switch got {
	case "linux", "darwin":
		// expected on the platforms Conduit currently supports
	case "":
		// allowed: the test is running on a GOOS we don't have a fragment for
	default:
		t.Errorf("DetectPlatform: got %q; expected linux, darwin, or empty", got)
	}
}
