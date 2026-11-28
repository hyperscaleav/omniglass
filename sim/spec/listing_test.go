package spec_test

import (
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// The console lists a family's line commands where the family serves them
// (#2833): a Command with a Face under that interface alone, one with none
// under every line interface as before, and a surface's commands under the
// interface its Surface names, each with the subscription verb it carries.

type verbs struct{}

func (verbs) Commands() []string { return []string{"GET X", "SUBSCRIBE X", "UNSUBSCRIBE X"} }

func (verbs) Handle(line string) (surface.Reply, bool) {
	switch line {
	case "GET X":
		return surface.Reply{Text: "X"}, true
	case "SUBSCRIBE X":
		return surface.Reply{Text: "OK", Action: surface.ActionSubscribe, Subject: "x"}, true
	case "UNSUBSCRIBE X":
		return surface.Reply{Text: "OK", Action: surface.ActionUnsubscribe, Subject: "x"}, true
	}
	return surface.Reply{}, false
}

func listingFamily() spec.Spec {
	say := func(spec.Device) string { return "" }
	return spec.Spec{
		Name:     "listing-family",
		Identity: spec.Identity{Manufacturer: "Acme", Model: "L1", DeviceType: "display"},
		Interfaces: []profile.Interface{
			{Name: "shell", Protocol: "ssh", Port: 22, Persistent: true},
			{Name: "line", Protocol: "tcp", Port: 5000, Persistent: true},
			{Name: "other", Protocol: "telnet", Port: 23, Persistent: true},
		},
		State: map[string]any{},
		Commands: []spec.Command{
			{Match: "show", Summary: "Show it", Face: "shell", Handle: say},
			{Match: "everywhere", Handle: say},
		},
		Surface: func(spec.Device) spec.Surface {
			return spec.Surface{Commanders: []surface.Commander{verbs{}}, ListedOn: "line"}
		},
	}
}

func TestAFamilyListsItsLineCommandsWhereItServesThem(t *testing.T) {
	f, err := spec.NewFamily(listingFamily())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]profile.Command{}
	for _, iface := range f.Surfaces() {
		got[iface.Name] = iface.Commands
	}
	match := func(cs []profile.Command) (out [][2]string) {
		for _, c := range cs {
			out = append(out, [2]string{c.Match, c.Action})
		}
		return out
	}
	for name, want := range map[string][][2]string{
		"shell": {{"show", ""}, {"everywhere", ""}},
		"line":  {{"everywhere", ""}, {"GET X", ""}, {"SUBSCRIBE X", "subscribe"}, {"UNSUBSCRIBE X", "unsubscribe"}},
		"other": {{"everywhere", ""}},
	} {
		if !reflect.DeepEqual(match(got[name]), want) {
			t.Errorf("%s lists %v, want %v", name, match(got[name]), want)
		}
	}
	for _, c := range got["shell"] {
		if !c.Native {
			t.Errorf("%s is listed but not marked native", c.Match)
		}
	}
	if got["shell"][0].Doc.Summary != "Show it" {
		t.Errorf("the summary was lost: %+v", got["shell"][0])
	}
}
