package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SoakRecordDir is where a release's soak records are committed, one file per
// build, as the build log keeps one file per entry. A release's notes link the
// record of the soak the build passed.
const SoakRecordDir = "docs/src/content/soak-records"

// SoakRecord is what is committed about one soak: the build it judged, where
// it ran, the window and the verdict with its reasons.
type SoakRecord struct {
	Build        string
	Host         string
	Since, Until time.Time
	Tolerance    time.Duration
	Verdict      SoakVerdict
}

var unsafeFileRune = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SoakRecordFile is the record's file name for a build: the build with
// anything a path could misread folded to a dash, so the name is the build.
func SoakRecordFile(build string) string {
	name := strings.Trim(unsafeFileRune.ReplaceAllString(build, "-"), "-.")
	if name == "" {
		name = "unknown-build"
	}
	return name + ".md"
}

// SoakHostFingerprint names the host a soak ran on well enough to tell two
// hosts apart: its name, platform, cores and a short digest of its machine id,
// never the id itself. A host with no machine id leaves the digest out.
func SoakHostFingerprint(hostname, goos, goarch string, cpus int, machineID string) string {
	fp := fmt.Sprintf("%s %s/%s %d cpu", hostname, goos, goarch, cpus)
	if id := strings.TrimSpace(machineID); id != "" {
		sum := sha256.Sum256([]byte(id))
		fp += " machine-id:" + hex.EncodeToString(sum[:])[:12]
	}
	return fp
}

// RenderSoakRecord is the record's file: front matter carrying the facts, and
// the verdict in words.
func RenderSoakRecord(r SoakRecord) string {
	verdict := "fail"
	if r.Verdict.Pass {
		verdict = "pass"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: \"Soak record: %s\"\nbuild: %s\nverdict: %s\nsince: %s\nuntil: %s\nhost: %s\n---\n\n",
		r.Build, r.Build, verdict, r.Since.UTC().Format(time.RFC3339), r.Until.UTC().Format(time.RFC3339), r.Host)
	fmt.Fprintf(&b, "Build %s was judged by `omniglass lab soak verdict` over %s, on %s.\n\n",
		r.Build, r.Until.Sub(r.Since).Round(time.Minute), r.Host)
	if r.Verdict.Pass {
		fmt.Fprintf(&b, "**Pass.** No platform alarm is open, none was open longer than %s, and every loss ratio on the platform health page reads 0.\n", r.Tolerance)
		return b.String()
	}
	b.WriteString("**Fail.**\n\n")
	for _, why := range r.Verdict.Reasons {
		fmt.Fprintf(&b, "- %s\n", why)
	}
	return b.String()
}
