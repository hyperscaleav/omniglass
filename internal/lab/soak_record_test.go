package lab

import (
	"strings"
	"testing"
	"time"
)

func TestSoakRecordFileNamesTheBuild(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"v1.4.0":           "v1.4.0.md",
		"v1.4.0-rc.1":      "v1.4.0-rc.1.md",
		"dev-4d5c1ab":      "dev-4d5c1ab.md",
		"feature/x y":      "feature-x-y.md",
		"../../etc/passwd": "etc-passwd.md",
		".hidden":          "hidden.md",
		"":                 "unknown-build.md",
		"///":              "unknown-build.md",
	}
	for build, want := range tests {
		if got := SoakRecordFile(build); got != want {
			t.Errorf("SoakRecordFile(%q) = %q, want %q", build, got, want)
		}
	}
}

func TestSoakHostFingerprint(t *testing.T) {
	t.Parallel()
	a := SoakHostFingerprint("soak-1", "linux", "amd64", 16, "0123456789abcdef0123456789abcdef")
	if want := "soak-1 linux/amd64 16 cpu machine-id:"; !strings.HasPrefix(a, want) {
		t.Errorf("fingerprint = %q, want it to start %q", a, want)
	}
	if strings.Contains(a, "0123456789abcdef0123456789abcdef") {
		t.Errorf("fingerprint %q carries the raw machine id", a)
	}
	if b := SoakHostFingerprint("soak-1", "linux", "amd64", 16, "ffffffffffffffffffffffffffffffff"); a == b {
		t.Error("two machines read one fingerprint")
	}
	if got := SoakHostFingerprint("soak-1", "linux", "amd64", 16, ""); strings.Contains(got, "machine-id") {
		t.Errorf("a host with no machine id read %q, want none named", got)
	}
}

func TestRenderSoakRecord(t *testing.T) {
	t.Parallel()
	rec := SoakRecord{
		Build: "v1.4.0", Host: "soak-1 linux/amd64 16 cpu",
		Since: soakStart, Until: soakStart.Add(SoakMinWindow + 3*time.Hour),
		Tolerance: SoakAlarmTolerance,
		Verdict:   SoakVerdict{Pass: true},
	}
	pass := RenderSoakRecord(rec)
	for _, want := range []string{
		"---\n", `title: "Soak record: v1.4.0"`, "build: v1.4.0", "verdict: pass",
		"since: 2026-11-20T08:00:00Z", "until: 2026-11-27T11:00:00Z", "host: soak-1 linux/amd64 16 cpu",
		"30m0s", "No platform alarm",
	} {
		if !strings.Contains(pass, want) {
			t.Errorf("a passing record lacks %q:\n%s", want, pass)
		}
	}
	if strings.Contains(pass, "\u2014") {
		t.Error("a record carries an em dash")
	}

	rec.Verdict = SoakVerdict{Reasons: []string{"loss 0.01 at outpost hop", "an alarm is still open"}}
	fail := RenderSoakRecord(rec)
	for _, want := range []string{"verdict: fail", "- loss 0.01 at outpost hop", "- an alarm is still open"} {
		if !strings.Contains(fail, want) {
			t.Errorf("a failing record lacks %q:\n%s", want, fail)
		}
	}
}
