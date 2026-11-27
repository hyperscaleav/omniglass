package nvp

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/surface"
)

func testRoom() (Room, *[2]string) {
	st := &[2]string{"hdmi1", "idle"}
	return Room{
		Inputs: DefaultInputs, Scenes: DefaultScenes,
		Input: Field[string]{Get: func() string { return st[0] }, Set: func(v string) { st[0] = v }},
		Scene: Field[string]{Get: func() string { return st[1] }, Set: func(v string) { st[1] = v }},
	}, st
}

func TestHandleAnswersEveryCommandItLists(t *testing.T) {
	r, _ := testRoom()
	for _, c := range r.Commands() {
		if _, ok := r.Handle(c); !ok {
			t.Errorf("lists %q but does not answer it", c)
		}
	}
}

func TestSetWritesTheFieldAndReportsTheChange(t *testing.T) {
	r, st := testRoom()
	got, ok := r.Handle("SET INPUT dp1")
	want := surface.Reply{Text: "INPUT dp1", Changes: []surface.Change{{Path: PathInput, Value: "dp1"}}}
	if !ok || got.Text != want.Text || len(got.Changes) != 1 || got.Changes[0] != want.Changes[0] || st[0] != "dp1" {
		t.Fatalf("got %+v ok=%v state=%v", got, ok, *st)
	}
	if rep, _ := r.Handle("GET INPUT"); rep.Text != "INPUT dp1" {
		t.Fatalf("GET INPUT = %q", rep.Text)
	}
}

func TestRefusesWhatItDoesNotDefine(t *testing.T) {
	r, st := testRoom()
	for _, line := range []string{"SET INPUT", "SET INPUT bogus", "SET SCENE ", "set input hdmi2", "UNSUBSCRIBE LOG", "UNSUBSCRIBE SCENE", ""} {
		if _, ok := r.Handle(line); ok {
			t.Errorf("answered %q", line)
		}
	}
	if *st != [2]string{"hdmi1", "idle"} {
		t.Fatalf("a refused line changed state: %v", *st)
	}
}

func TestSubscribeNamesItsSubjectAndToken(t *testing.T) {
	r, _ := testRoom()
	got, _ := r.Handle("SUBSCRIBE SCENE")
	if got.Action != surface.ActionSubscribe || got.Subject != PathScene || got.Token != "SCENE" || got.Text != "OK" {
		t.Fatalf("got %+v", got)
	}
	got, _ = r.Handle("UNSUBSCRIBE INPUT")
	if got.Action != surface.ActionUnsubscribe || got.Subject != PathInput || got.Token != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestSubjectsPushTheirLine(t *testing.T) {
	want := map[string]string{PathInput: "EVT INPUT hdmi2", PathLog: "LOG WARN x", PathScene: "EVT SCENE vc"}
	val := map[string]any{PathInput: "hdmi2", PathLog: "WARN x", PathScene: "vc"}
	for _, s := range Subjects() {
		if got := s.Push(val[s.Path]); got != want[s.Path] {
			t.Errorf("%s pushes %q, want %q", s.Path, got, want[s.Path])
		}
	}
}
