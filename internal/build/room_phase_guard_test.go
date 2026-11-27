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

// TestNoProfileCarriesARoomTimingNumber guards the room phase (#2797). A
// room's wake lead, cooldown tail and sharing stretch are declared once per
// standard in the manifest's calendar.meetings knobs and read through the
// room phase (roomPhase and its fields), so no simulated device profile may
// work one out again: it may not call the retired roomInUse or meetingSharing
// funcs, and no template action may do arithmetic on a meeting's start, end or
// slot (a wake lead is `sub .start now`, a tail `add .end 300`, a recording
// window `sub now .end`). Comments are not templates and are not read.
func TestNoProfileCarriesARoomTimingNumber(t *testing.T) {
	root := filepath.Join("..", "..", "sim", "profiles")
	retired := regexp.MustCompile(`\b(roomInUse|meetingSharing)\b`)
	action := regexp.MustCompile(`\{\{.*?\}\}`)
	meetingTime := regexp.MustCompile(`\.(start|end|slot_start|slot_end)\b`)
	arith := regexp.MustCompile(`\b(add|sub|mul|div)\b`)
	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if retired.MatchString(line) {
				found = append(found, rel+":"+strconv.Itoa(i+1)+": calls a retired timing func; read roomPhase")
			}
			for _, a := range action.FindAllString(line, -1) {
				if meetingTime.MatchString(a) && (arith.MatchString(a) || strings.Contains(a, "now")) {
					found = append(found, rel+":"+strconv.Itoa(i+1)+": does arithmetic on a meeting's time ("+strings.TrimSpace(a)+"); a wake, tail or sharing number belongs in the standard's calendar.meetings knobs, read through roomPhase")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Fatalf("a profile works out room timing itself:\n  %s", strings.Join(found, "\n  "))
	}
}

// The guard catches what it names: the shapes the profiles carried before the
// room phase.
func TestRoomTimingGuardRecognizesTheOldShapes(t *testing.T) {
	meetingTime := regexp.MustCompile(`\.(start|end|slot_start|slot_end)\b`)
	arith := regexp.MustCompile(`\b(add|sub|mul|div)\b`)
	for _, a := range []string{`{{ add .end 300 }}`, `{{ le (sub .start now) 120 }}`, `{{ le (sub now .end) 600 }}`} {
		if !(meetingTime.MatchString(a) && (arith.MatchString(a) || strings.Contains(a, "now"))) {
			t.Errorf("the guard misses %s", a)
		}
	}
	if (meetingTime.MatchString(`{{ printf "m%v" .start }}`) && arith.MatchString(`{{ printf "m%v" .start }}`)) {
		t.Error("the guard flags a meeting key")
	}
}
