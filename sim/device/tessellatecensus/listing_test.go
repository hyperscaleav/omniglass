package tessellatecensus

import (
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// The console lists what each band answers, as the profile did: the text
// commands under the raw-TCP control band, the binary poll as a hex command
// under its own band. Regression for #2857's live pass: the family listed none.
func TestTheConsoleListsEachBandsCommands(t *testing.T) {
	f, err := spec.NewFamily(Spec())
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string][]string{}
	summaries := map[string]string{}
	for _, iface := range f.Surfaces() {
		for _, c := range iface.Commands {
			m := c.Match
			if c.MatchHex != "" {
				m = "hex " + c.MatchHex
			}
			listed[iface.Name] = append(listed[iface.Name], m)
			if c.Doc.Summary != "" {
				summaries[m] = c.Doc.Summary
			}
		}
	}
	want := map[string][]string{
		"control":     {"COUNT?", "VACANT?", "SUBSCRIBE COUNT", "SUBSCRIBE VACANT", "SUBSCRIBE EVENT", "UNSUBSCRIBE COUNT", "UNSUBSCRIBE VACANT"},
		"control-hex": {"hex AA 01"},
	}
	if !reflect.DeepEqual(listed, want) {
		t.Errorf("listed commands = %v, want %v", listed, want)
	}
	// Each summary reads as the profile's did.
	wantSummaries := map[string]string{
		"COUNT?":    "Query live people count",
		"VACANT?":   "Query vacant flag",
		"hex AA 01": "Hex poll: people_count (request AA 01 → response AA 81 <count>)",
	}
	if !reflect.DeepEqual(summaries, wantSummaries) {
		t.Errorf("summaries = %v, want %v", summaries, wantSummaries)
	}
}
