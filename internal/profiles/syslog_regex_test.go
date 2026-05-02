package profiles

import (
	"regexp"
	"strings"
	"testing"
)

// TestLinuxSyslogRegex_MatchesBothFormats locks down the filelog/system
// regex shipped at internal/profiles/linux/logs.yaml against the two
// rsyslog file templates that modern Linux distros disagree on:
//
//   - BSD / RFC3164:        RSYSLOG_TraditionalFileFormat
//     (RHEL 9, Alpine, older Debian/Ubuntu)
//   - ISO 8601 high-res:    RSYSLOG_FileFormat
//     (Ubuntu 22.04+, Debian 12+; the new default)
//
// V0.0.1 shipped with a BSD-only regex that missed every line on Ubuntu
// 22.04+ and produced a self-amplifying error spam loop (parser fails →
// stderr error → systemd → journald → rsyslog → /var/log/syslog → parser
// fails → ...). This test pins the alternation in place so a future
// "simplify the regex" patch can't silently regress us back to that
// state.
func TestLinuxSyslogRegex_MatchesBothFormats(t *testing.T) {
	body, err := Load("linux", SignalSystemLogs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	re := mustExtractFilelogRegex(t, body)
	compiled, err := regexp.Compile(re)
	if err != nil {
		t.Fatalf("compile %q: %v", re, err)
	}

	cases := []struct {
		name     string
		line     string
		wantProc string
		wantPID  string
		wantMsg  string
	}{
		{
			name:     "BSD format with single-digit day (double-space)",
			line:     "May  2 14:24:53 host01 sshd[1234]: Accepted publickey for andy",
			wantProc: "sshd",
			wantPID:  "1234",
			wantMsg:  "Accepted publickey for andy",
		},
		{
			name:     "BSD format with two-digit day",
			line:     "Dec 25 09:00:00 host01 cron[42]: starting daily backup",
			wantProc: "cron",
			wantPID:  "42",
			wantMsg:  "starting daily backup",
		},
		{
			name:     "BSD format without pid",
			line:     "Jan  1 00:00:00 host01 kernel: rolling over",
			wantProc: "kernel",
			wantPID:  "",
			wantMsg:  "rolling over",
		},
		{
			name:     "ISO 8601 with fractional seconds and offset (Ubuntu 22.04+ default)",
			line:     "2026-05-02T14:24:53.001012-04:00 lima-conduit-smoke conduit[3598]: starting",
			wantProc: "conduit",
			wantPID:  "3598",
			wantMsg:  "starting",
		},
		{
			name:     "ISO 8601 with Z suffix",
			line:     "2026-05-02T14:24:53Z host01 systemd[1]: started",
			wantProc: "systemd",
			wantPID:  "1",
			wantMsg:  "started",
		},
		{
			name:     "ISO 8601 without colon in offset",
			line:     "2026-05-02T14:24:53.001-0400 host01 cron[42]: tick",
			wantProc: "cron",
			wantPID:  "42",
			wantMsg:  "tick",
		},
		{
			name:     "ISO 8601 with space separator (older rsyslog templates)",
			line:     "2026-05-02 14:24:53 host01 dhclient: bound to 10.0.0.1",
			wantProc: "dhclient",
			wantPID:  "",
			wantMsg:  "bound to 10.0.0.1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := captureNamed(compiled, tc.line)
			if m == nil {
				t.Fatalf("regex did not match line:\n  %s", tc.line)
			}
			if got := m["process"]; got != tc.wantProc {
				t.Errorf("process: got %q, want %q", got, tc.wantProc)
			}
			if got := m["pid"]; got != tc.wantPID {
				t.Errorf("pid: got %q, want %q", got, tc.wantPID)
			}
			if got := m["message"]; got != tc.wantMsg {
				t.Errorf("message: got %q, want %q", got, tc.wantMsg)
			}
		})
	}
}

// TestLinuxSyslogRegex_DoesNotMatch covers the lines that on_error: send
// is supposed to forward unparsed: kernel ringbuffer continuations,
// multi-line stack-trace tails, and lines from formats we don't
// support. The regex must NOT match these, otherwise we'd silently
// chop off the leading bytes and ship a corrupted message. Keeping the
// non-match contract explicit catches an "I made the regex more
// permissive" change that ate well-formed lines.
func TestLinuxSyslogRegex_DoesNotMatch(t *testing.T) {
	body, err := Load("linux", SignalSystemLogs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	compiled := regexp.MustCompile(mustExtractFilelogRegex(t, body))

	notSyslog := []string{
		"",
		"\tat com.example.Foo.bar(Foo.java:42)",
		"=== systemd-resolved start ===",
		"Caused by: java.lang.NullPointerException",
		// RFC5424 (priority + version) — lands in journald, not in
		// the file we tail, but worth making sure we don't
		// accidentally chew it up:
		"<165>1 2026-05-02T14:24:53.001Z host app - - - msg",
	}
	for _, line := range notSyslog {
		if compiled.MatchString(line) {
			t.Errorf("regex unexpectedly matched non-syslog line:\n  %q", line)
		}
	}
}

// TestDarwinSyslogRegex_MatchesBothFormats does the same for the macOS
// fragment, which has its own format quirk (install.log writes
// "YYYY-MM-DD HH:MM:SS-OF" without the colon-grouped offset rsyslog uses).
// Pinning this here means a copy-paste from the linux fragment that drops
// macOS install.log support fails loudly.
func TestDarwinSyslogRegex_MatchesBothFormats(t *testing.T) {
	body, err := Load("darwin", SignalSystemLogs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	compiled := regexp.MustCompile(mustExtractFilelogRegex(t, body))

	cases := []struct {
		name string
		line string
	}{
		{"system.log BSD", "May  1 13:59:13 andy-mac kernel: foo"},
		{"install.log ISO+offset(no colon)", "2026-05-01 13:42:38-04 andy-mac softwareupdated[123]: starting check"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !compiled.MatchString(tc.line) {
				t.Errorf("regex did not match line:\n  %s", tc.line)
			}
		})
	}
}

// captureNamed returns a map of named-group → captured-text for a single
// match, or nil if the line doesn't match. Pulled out so the test
// assertions can read like a table instead of a stream of FindSubmatch
// index arithmetic.
func captureNamed(re *regexp.Regexp, line string) map[string]string {
	m := re.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(re.SubexpNames()))
	for i, name := range re.SubexpNames() {
		if name == "" {
			continue
		}
		out[name] = m[i]
	}
	return out
}

// mustExtractFilelogRegex pulls the regex string out of a profile YAML
// fragment without depending on a YAML decoder — the fragment shape is
// stable ("    - type: regex_parser\n      regex: '<pattern>'") and
// pinning the regex string this way means the test fails the moment
// someone edits the fragment, which is exactly when we want it to.
func mustExtractFilelogRegex(t *testing.T, body string) string {
	t.Helper()
	const marker = "regex: '"
	idx := strings.Index(body, marker)
	if idx == -1 {
		t.Fatalf("filelog/system regex not found in fragment:\n%s", body)
	}
	rest := body[idx+len(marker):]
	end := strings.Index(rest, "'")
	if end == -1 {
		t.Fatalf("filelog/system regex not terminated in fragment:\n%s", body)
	}
	return rest[:end]
}
