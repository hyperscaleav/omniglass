package borealcast

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(nil, Spec())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Rows {
		res, ok := cat.ForProduct(r.Product)
		if !ok || res.Definition.ID() != Name {
			t.Fatalf("%s resolves to %+v", r.Product, res.Definition)
		}
		if res.Identity.Model != r.Model || res.Identity.DeviceType != "media-player" {
			t.Errorf("%s: identity = %+v", r.Product, res.Identity)
		}
		f := spectest.NewFake(Spec(), r.Product)
		var doc struct {
			Device  struct{ Model, Firmware string } `json:"device"`
			Storage statusDoc                        `json:"storage"`
		}
		if err := json.Unmarshal(statusRoute(f, spec.HTTPRequest{}).Body.([]byte), &doc); err != nil || doc.Device.Model != r.Model || doc.Device.Firmware != r.Firmware {
			t.Errorf("%s: status = %+v (%v)", r.Product, doc, err)
		}
		if doc.Storage.TotalGB != int64(r.StorageGB) || doc.Storage.UsedPercent != r.UsedPercent {
			t.Errorf("%s: storage %+v, row says %d GB at %v%%", r.Product, doc.Storage, r.StorageGB, r.UsedPercent)
		}
	}
}

func tick(t *testing.T, name string) spec.Tick {
	t.Helper()
	for _, tk := range Spec().Ticks {
		if tk.Name == name {
			return tk
		}
	}
	t.Fatalf("no tick %s", name)
	return spec.Tick{}
}

func apply(f *spectest.Fake, tk spec.Tick, c spec.TickContext) {
	for _, ch := range tk.Run(f, c) {
		_ = f.Write(ch.Path, ch.Value)
	}
}

// The playlist moves one item a tick while playing and holds when paused.
func TestPlaylistStepsWhilePlayingAndHoldsPaused(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-cast-m1")
	adv := tick(t, "playlist_advance")
	var seen []string
	for i := 0; i < 5; i++ {
		apply(f, adv, spec.TickContext{})
		seen = append(seen, spec.String(f, pathContent))
	}
	want := behave.DefaultSign.Playlist
	for i, w := range []string{want[1], want[2], want[3], want[0], want[1]} {
		if seen[i] != w {
			t.Fatalf("playlist over five ticks = %q", seen)
		}
	}
	_ = f.Write(pathState, "paused")
	apply(f, adv, spec.TickContext{})
	if got := spec.String(f, pathContent); got != want[1] {
		t.Errorf("a tick while paused moved the item to %q", got)
	}
}

// The sign follows the building's day: it stops outside the hours, starts the
// playlist over at opening, holds an operator's pause until the next edge, and
// plays on where there are no hours to read.
func TestSignFollowsTheBuildingsDay(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-cast-m1")
	day := tick(t, "building_day")
	_ = f.Write(pathIndex, int64(2))
	at := func(open, known bool) spec.TickContext {
		return spec.TickContext{Now: f.Clock, Hours: behave.HoursReading{Known: known, Open: open}}
	}
	apply(f, day, at(false, true))
	if f.Read(pathState) != "stopped" {
		t.Fatalf("after closing: %v", f.Read(pathState))
	}
	apply(f, day, at(true, true))
	if f.Read(pathState) != "playing" || spec.Int(f, pathIndex) != 0 || f.Read(pathContent) != "Welcome loop" {
		t.Fatalf("on opening: %v item %v %v", f.Read(pathState), f.Read(pathIndex), f.Read(pathContent))
	}
	_ = f.Write(pathState, "paused")
	apply(f, day, at(true, true))
	if f.Read(pathState) != "paused" {
		t.Fatalf("a pause during open hours became %v", f.Read(pathState))
	}
	g := spectest.NewFake(Spec(), "boreal-cast-m1")
	apply(g, day, at(false, false))
	if g.Read(pathState) != "playing" {
		t.Fatalf("with no hours to read the sign is %v", g.Read(pathState))
	}
}

func TestStorageStaysWithinItsBand(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-cast-m1")
	drift := tick(t, "storage_drift")
	rng := behave.NewSource("storage")
	for i := 0; i < 500; i++ {
		apply(f, drift, spec.TickContext{Rand: rng, Now: time.Now()})
		if u := spec.Float(f, pathUsed); u < 20 || u > 90 {
			t.Fatalf("storage reached %v%%", u)
		}
	}
}

// The API demands its key, and setting playback answers the playback document.
func TestPlaybackControlAnswersThePlaybackDocument(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-cast-m1")
	for _, r := range Spec().HTTP {
		if r.Match == "POST /api/playback" {
			res := r.Handle(f, spec.HTTPRequest{Body: map[string]any{"state": "paused"}})
			if got := string(res.Body.([]byte)); got != "{\n  \"state\": \"paused\",\n  \"index\": 0,\n  \"content\": \"Welcome loop\"\n}\n" {
				t.Fatalf("answer = %q", got)
			}
		}
	}
	var key string
	for _, i := range Spec().Interfaces {
		if i.Protocol == "http" {
			key = i.Auth.APIKey
		}
	}
	if key != "boreal-cast-key" {
		t.Errorf("the API key is %q", key)
	}
}
