// Package profiles owns the embedded YAML fragments that turn the V0
// OTel-only base pipeline into a useful out-of-the-box agent on each
// supported platform.
//
// A "profile" is the set of fragments that ship for one runtime.GOOS:
// Linux, Darwin (macOS) today; Windows lands at M6, Kubernetes / Docker
// land alongside their respective milestones. Each profile has up to one
// fragment per concern (host metrics, system logs, ...). Fragments are
// chunks of upstream OTel Collector YAML rooted at the receiver level —
// no top-level "receivers:" key — so the expander can splice them into
// the rendered config under a single receivers: block.
//
// The loader is deliberately small: it answers "give me the fragment for
// hostmetrics on linux" and "what GOOS values do I have fragments for".
// Profile-mode resolution (auto -> runtime.GOOS, etc.) lives in the
// expander where it has access to the validated AgentConfig.
package profiles

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"sort"
	"strings"
)

// Signal names every fragment Conduit knows how to load. Adding a new
// fragment kind means adding a constant here, an authoring contract in
// this package's comments, and the matching .yaml file under each
// platform directory that supports it.
type Signal string

const (
	// SignalHostMetrics is the hostmetrics receiver fragment for the platform.
	SignalHostMetrics Signal = "hostmetrics"
	// SignalSystemLogs is the system-log receiver fragment (filelog and,
	// where applicable, journald) for the platform. On k8s this fragment
	// holds filelog/k8s rather than journald.
	SignalSystemLogs Signal = "logs"
	// SignalKubelet is the kubeletstatsreceiver fragment. Only the k8s
	// platform ships it today; host platforms have no analogue.
	SignalKubelet Signal = "kubelet"
)

//go:embed all:linux all:darwin all:docker all:k8s
var fragmentsFS embed.FS

// Available reports the platform names for which Conduit ships at least one
// fragment. Used by the expander to decide whether profile.mode=auto can
// resolve cleanly on a given runtime.GOOS.
func Available() []string {
	entries, err := fs.ReadDir(fragmentsFS, ".")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Has reports whether the platform/signal combination has a fragment.
// Callers should use this before Load when they want to silently skip a
// missing pairing rather than treat absence as an error.
func Has(platform string, signal Signal) bool {
	_, err := fragmentsFS.ReadFile(fragmentPath(platform, signal))
	return err == nil
}

// Load returns the YAML body for the given platform / signal pairing. The
// returned text is the raw fragment with comments preserved; the expander
// is responsible for indenting it to fit under receivers: when splicing.
//
// errors:
//   - ErrUnknownPlatform if Conduit ships no fragments for that GOOS.
//   - ErrUnknownSignal if the platform exists but lacks that signal
//     fragment (e.g. macOS has no journald).
func Load(platform string, signal Signal) (string, error) {
	if platform == "" {
		return "", fmt.Errorf("profiles: platform is empty")
	}
	if !platformExists(platform) {
		return "", fmt.Errorf("profiles: %w: %q (available: %s)", ErrUnknownPlatform, platform, strings.Join(Available(), ", "))
	}
	body, err := fragmentsFS.ReadFile(fragmentPath(platform, signal))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("profiles: %w: %s/%s.yaml", ErrUnknownSignal, platform, signal)
		}
		return "", fmt.Errorf("profiles: read %s/%s.yaml: %w", platform, signal, err)
	}
	return string(body), nil
}

// DetectPlatform returns runtime.GOOS if Conduit ships fragments for it,
// or "" if it does not. The expander turns "" into a graceful fall-back
// to ProfileModeNone with a stderr warning.
func DetectPlatform() string {
	goos := runtime.GOOS
	if platformExists(goos) {
		return goos
	}
	return ""
}

func fragmentPath(platform string, signal Signal) string {
	return path.Join(platform, string(signal)+".yaml")
}

func platformExists(platform string) bool {
	info, err := fs.Stat(fragmentsFS, platform)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// ErrUnknownPlatform indicates Conduit has no fragment directory for the
// requested platform (typically because we haven't shipped that profile yet).
var ErrUnknownPlatform = errors.New("unknown platform")

// ErrUnknownSignal indicates the platform exists but does not ship a
// fragment for that signal (e.g. macOS has no journald).
var ErrUnknownSignal = errors.New("unknown signal")
