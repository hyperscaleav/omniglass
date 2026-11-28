package behave_test

import (
	"math"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
)

func TestDriftWalksOneDrawWithinItsBandAndRounds(t *testing.T) {
	band := behave.Band{Lo: 34, Hi: 54, Step: 1}
	rng := &stub{frac: 1} // the largest step up
	if got := behave.Drift(38, band, rng, behave.SigFigs(3)); got != 39 || rng.draws != 1 {
		t.Fatalf("drift = %v after %d draws, want 39 after one", got, rng.draws)
	}
	if got := behave.Drift(53.9, band, &stub{frac: 1}, behave.SigFigs(3)); got != 54 {
		t.Fatalf("drift at the top = %v, want the band's 54", got)
	}
	if got := behave.Drift(34.2, band, &stub{frac: 0}, behave.SigFigs(3)); got != 34 {
		t.Fatalf("drift at the bottom = %v, want the band's 34", got)
	}
	if got := behave.Drift(41.5, behave.Band{Lo: 20, Hi: 90, Step: 0.5}, &stub{frac: 0.7}, behave.Places(1)); got != 41.7 {
		t.Fatalf("storage drift = %v, want 41.7 (41.5 + 0.2 kept to a place)", got)
	}
}

func TestWearAddsOnlyWhileLitAndKeepsItsPlaces(t *testing.T) {
	if got := behave.Wear(12480.5, false, behave.DefaultLightSource); got != 12480.5 {
		t.Fatalf("hours in standby = %v, want them held", got)
	}
	h := 12480.5
	for i := 0; i < 3; i++ {
		h = behave.Wear(h, true, behave.DefaultLightSource)
	}
	if math.Abs(h-12480.5501) > 1e-9 {
		t.Fatalf("three lit minutes = %v, want 12480.5501", h)
	}
	h = 0
	for i := 0; i < 60; i++ {
		h = behave.Wear(h, true, behave.DefaultLightSource)
	}
	if math.Abs(h-1) > 0.005 {
		t.Fatalf("an hour of lit minutes = %v, want about an hour", h)
	}
}

func TestPlaylistAdvanceStepsWhilePlayingAndWraps(t *testing.T) {
	p := behave.DefaultSign
	s := behave.SignState{Playback: "playing", Content: "Welcome loop"}
	var seen []string
	for i := 0; i < 5; i++ {
		s = behave.PlaylistAdvance(s, p)
		seen = append(seen, s.Content)
	}
	want := []string{"Wayfinding", "Today's events", "Safety notice", "Welcome loop", "Wayfinding"}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("playlist = %q, want %q", seen, want)
		}
	}
	for _, held := range []string{"paused", "stopped"} {
		before := behave.SignState{Playback: held, Index: 2, Content: "Today's events"}
		if after := behave.PlaylistAdvance(before, p); after != before {
			t.Errorf("%s: a tick moved %+v to %+v", held, before, after)
		}
	}
	if got := behave.PlaylistAdvance(behave.SignState{Playback: "playing", Index: 9}, behave.SignParams{Playing: "playing"}); got.Index != 9 {
		t.Errorf("an empty playlist moved the index to %d", got.Index)
	}
}
