package profiles

import (
	"errors"
	"strings"
	"testing"
)

func TestAvailable_HasLinuxAndDarwin(t *testing.T) {
	got := Available()
	want := map[string]bool{"linux": true, "darwin": true}
	if len(got) < 2 {
		t.Fatalf("Available: got %v, want at least linux + darwin", got)
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
		{"linux", "kubelet", false}, // not a real signal in M3.A
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
	// linux exists; "kubelet" doesn't — yet.
	_, err := Load("linux", "kubelet")
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
