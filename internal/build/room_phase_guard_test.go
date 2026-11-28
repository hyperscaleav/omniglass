package build

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestNoFamilyCarriesARoomTimingNumber guards the room phase (#2797). A
// room's wake lead, cooldown tail, recording tail and sharing stretch are
// declared once per standard in the manifest's calendar.meetings knobs and read
// through the room phase (TickContext.Room and its fields), so no simulated
// device family may work one out again: it may not name the retired roomInUse or
// meetingSharing, nor reach for the knobs themselves (WakeLead, CooldownTail,
// RecordingTail), which the calendar resolves into the phase. Comments are not
// code and are not read.
func TestNoFamilyCarriesARoomTimingNumber(t *testing.T) {
	root := filepath.Join("..", "..", "sim", "device")
	found, err := roomTimingFindings(root)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(found) > 0 {
		t.Fatalf("a family works out room timing itself:\n  %s", strings.Join(found, "\n  "))
	}
}

var retiredRoomTiming = regexp.MustCompile(`\b(roomInUse|meetingSharing|WakeLead|CooldownTail|RecordingTail)\b`)

// roomTimingFindings lists every non-test Go line under root that names a retired
// timing func or a calendar timing knob.
func roomTimingFindings(root string) ([]string, error) {
	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if m := retiredRoomTiming.FindString(line); m != "" {
				found = append(found, rel+":"+strconv.Itoa(i+1)+": names "+m+"; read the room phase")
			}
		}
		return nil
	})
	sort.Strings(found)
	return found, err
}

// The guard catches what it names, and passes a family that reads the phase.
func TestRoomTimingGuardRecognizesTheOldShapes(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("clean.go", "package a\n// the wake lead is the calendar's (WakeLead)\nfunc f(c TickContext) bool { return c.Room.InUse }\n")
	write("lead.go", "package a\nfunc f(k Knobs) int { return k.WakeLead }\n")
	write("retired.go", "package a\nvar _ = roomInUse\n")
	write("lead_test.go", "package a\nvar _ = WakeLead\n")
	got, err := roomTimingFindings(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "lead.go:2") || !strings.HasPrefix(got[1], "retired.go:2") {
		t.Fatalf("findings = %v, want lead.go and retired.go only", got)
	}
}
