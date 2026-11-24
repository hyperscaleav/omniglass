package docslint

import (
	"strings"
	"testing"
)

func labelUniverse() map[string]bool {
	set := map[string]bool{}
	collectLabels(`<button aria-label="Add device">Save changes</button>; const t = 'Alarms'`, set)
	return set
}

func TestCollectLabels(t *testing.T) {
	set := labelUniverse()
	for _, want := range []string{"add device", "save changes", "alarms"} {
		if !set[want] {
			t.Errorf("label %q not collected: %v", want, set)
		}
	}
}

func TestScanUILabelsResolves(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{"Select **Add device** to begin.", 0},
		{"Click **add device**.", 0},
		{"Open the **Alarms** tab.", 0},
		{"Press `Save changes`.", 0},
		{"Select **Ghost button**.", 1},
		{"Open the **Nothing** tab.", 1},
		{"The **Ghost button** is bold but no verb precedes it.", 0},
	}
	for _, c := range cases {
		if got := scanUILabelsIn(c.line, labelUniverse()); len(got) != c.want {
			t.Errorf("%q: got %v, want %d", c.line, got, c.want)
		}
	}
}

func TestScanUILabelsIsFenceAware(t *testing.T) {
	doc := strings.Join([]string{
		"Select **Add device**.",
		"",
		":::design[#434]",
		"Click **Future thing**.",
		":::",
		"",
		"Click **Ghost**.",
	}, "\n")
	got := scanUILabelsInDoc(doc, labelUniverse())
	if len(got) != 1 || got[0].Line != 7 {
		t.Fatalf("want one finding on line 7, got %v", got)
	}
}

// TestUILabelReferences runs the lint over the real corpus, enforced.
func TestUILabelReferences(t *testing.T) {
	findings, err := ScanUILabelReferences()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "ui-label-reference", findings, true)
}
