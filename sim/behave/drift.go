package behave

// Drift is a reading that wanders inside a band: the previous value moved by a
// uniform draw within plus or minus the band's step (one draw), held within the
// band, and rounded as the device reports it. Round is Places or SigFigs.
func Drift(prev float64, b Band, rng Source, round func(float64) float64) float64 {
	return round(Clamp(b.Lo, b.Hi, Step(rng, b.Step, prev)))
}

// Places rounds to n decimal places (see Round).
func Places(n int) func(float64) float64 { return func(v float64) float64 { return Round(n, v) } }

// SigFigs rounds to n significant figures (see SigFig).
func SigFigs(n int) func(float64) float64 { return func(v float64) float64 { return SigFig(n, v) } }

// WearParams are a worn part's rules: what a tick adds while the device is
// lit, and the decimal places the reading is kept to.
type WearParams struct {
	PerTick float64
	Places  int
}

// DefaultLightSource is a projector's laser source on a one-minute tick: a
// minute's share of an hour, kept to the ten-thousandth so an hour of ticks
// adds an hour.
var DefaultLightSource = WearParams{PerTick: 0.0166667, Places: 4}

// Wear is a part's hours after a tick: a lit device adds PerTick, and a device
// in standby adds nothing.
func Wear(prev float64, lit bool, p WearParams) float64 {
	if !lit {
		return prev
	}
	return Round(p.Places, prev+p.PerTick)
}

// PlaylistAdvance is a player's next item: while it plays it steps to the next
// item, wrapping at the end; paused or stopped, it holds. The content follows
// the index.
func PlaylistAdvance(prev SignState, p SignParams) SignState {
	next := prev
	if prev.Playback == p.Playing && len(p.Playlist) > 0 {
		next.Index = (prev.Index + 1) % len(p.Playlist)
	}
	if len(p.Playlist) > 0 {
		next.Content = p.Playlist[min(max(next.Index, 0), len(p.Playlist)-1)]
	}
	return next
}
