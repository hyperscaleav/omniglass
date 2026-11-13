package labfootprint

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The worklist read (#1868) takes each node's worst worklist pull loss over
// the window, by node name, from the nodes' own platform components.
func TestWorklistQueryReadsEachNodesWorstPullLoss(t *testing.T) {
	q := WorklistQuery(at(0), at(600))
	for _, want := range []string{"'" + writerclass.WorklistPullLossRatio + "'", "max(m.value)", "n.name",
		"n.platform_component_id = s.component_id", "to_timestamp(" + epoch(at(0)) + ")", "to_timestamp(" + epoch(at(600)) + ")"} {
		if !strings.Contains(q, want) {
			t.Errorf("the worklist query has no %q:\n%s", want, q)
		}
	}
}

// The parse keeps one ratio a node; a ratio that is no number, or out of
// [0, 1], is an error.
func TestParseWorklist(t *testing.T) {
	got, err := ParseWorklist("edge-hq|1\nedge-a|0\n\nnot a line\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["edge-hq"] != 1 || got["edge-a"] != 0 {
		t.Errorf("parsed %v, want edge-hq 1 and edge-a 0", got)
	}
	for _, bad := range []string{"edge-hq|one\n", "edge-hq|1.5\n", "edge-hq|-0.1\n"} {
		if _, err := ParseWorklist(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// The worklist_loss family reads one figure a node, named for it: 0 passes,
// a node that ran a stale worklist fails and says so, and a run with no
// reading is not measured rather than clean.
func TestWorklistLossFamily(t *testing.T) {
	var fam exactFamily
	for _, f := range exactFamilies() {
		if f.Family == "worklist_loss" {
			fam = f
		}
	}
	if fam.Read == nil {
		t.Fatal("no worklist_loss exact family")
	}
	r := cleanRun()
	r.WorklistLoss = map[string]float64{"edge-hq": 1, "edge-a": 0}
	got := fam.Read(r)
	if len(got) != 2 || got[0].Name != "worklist_loss/edge-a" || got[0].Value != 0 || got[0].Note != "" ||
		got[1].Name != "worklist_loss/edge-hq" || got[1].Value != 1 || !strings.Contains(got[1].Note, "stale worklist") {
		t.Errorf("read %+v, want edge-a at 0 then edge-hq at 1 naming its stale worklist", got)
	}

	r.WorklistLoss, r.WorklistNotRead = nil, "the database did not answer"
	if got := fam.Read(r); len(got) != 1 || !got[0].Missing || !strings.Contains(got[0].Note, "the database did not answer") {
		t.Errorf("an unread run read %+v, want one missing reading saying why", got)
	}
	r.WorklistNotRead = ""
	if got := fam.Read(r); len(got) != 1 || !got[0].Missing || !strings.Contains(got[0].Note, "no node") {
		t.Errorf("a run no node reported read %+v, want one missing reading", got)
	}
}

// The printed report carries the worklist row (#1868): the worst node's share
// named, 0 % when every node pulled clean, and "not measured" with a footer
// saying why when the read failed or no node reported.
func TestScaleTableWorklistRow(t *testing.T) {
	cases := []struct {
		name string
		run  ScaleRun
		want []string
	}{
		{"clean", ScaleRun{Worklist: map[string]float64{"edge-hq": 0, "edge-a": 0}},
			[]string{"worklist pull loss           0 %   (2 nodes)\n"}},
		{"stale", ScaleRun{Worklist: map[string]float64{"edge-hq": 1, "edge-a": 0.5, "edge-b": 0}},
			[]string{"worklist pull loss           100 % on edge-hq   (2 of 3 nodes refused)\n"}},
		{"unread", ScaleRun{WorklistGap: "the database did not answer"},
			[]string{"worklist pull loss           not measured\n", "worklist pull loss not measured: the database did not answer\n"}},
		{"no node", ScaleRun{},
			[]string{"worklist pull loss           not measured\n", "worklist pull loss not measured: no node reported a worklist pull loss in the window\n"}},
	}
	for _, c := range cases {
		got := SummarizeScale(c.run).Table()
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: the table has no %q:\n%s", c.name, w, got)
			}
		}
	}
}
