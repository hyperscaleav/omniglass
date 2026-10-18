package sim

import (
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// fixedClock returns a clock func reading a mutable time, so a test can
// advance the wall clock deterministically between reads.
func fixedClock(t *time.Time) func() time.Time {
	return func() time.Time { return *t }
}

// TestComputedElapsedReadPath covers the core resolver: uptime is
// now - boot_epoch, resolved fresh on every ReadPath with no stored
// counter and no tick.
func TestComputedElapsedReadPath(t *testing.T) {
	now := time.Unix(10_000, 0)
	state := map[string]any{
		"status": map[string]any{
			"device": map[string]any{"boot_epoch": int64(9_000)},
		},
	}
	d := newTestDevice(t, "d-1", state)
	d.SetClock(fixedClock(&now))
	if err := d.AddComputedFromProfile(&profile.Computed{
		Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch",
	}); err != nil {
		t.Fatalf("AddComputedFromProfile: %v", err)
	}

	if got := d.ReadPath("status.device.uptime"); got != int64(1_000) {
		t.Errorf("uptime at t=10000 = %v (%T), want int64(1000)", got, got)
	}

	// Advancing the clock advances uptime with zero writes.
	now = time.Unix(10_500, 0)
	if got := d.ReadPath("status.device.uptime"); got != int64(1_500) {
		t.Errorf("uptime at t=10500 = %v (%T), want int64(1500)", got, got)
	}

	// A non-computed path still falls through to stored state.
	if got := d.ReadPath("status.device.boot_epoch"); got != int64(9_000) {
		t.Errorf("boot_epoch passthrough = %v (%T), want int64(9000)", got, got)
	}
}

// TestComputedElapsedClampsAndUnsetAnchor pins the two guard paths: an
// unset (0) anchor reads 0, and a clock that runs behind the anchor
// never reports negative uptime.
func TestComputedElapsedClampsAndUnsetAnchor(t *testing.T) {
	now := time.Unix(500, 0)
	state := map[string]any{
		"status": map[string]any{"device": map[string]any{"boot_epoch": int64(1_000)}},
	}
	d := newTestDevice(t, "d-1", state)
	d.SetClock(fixedClock(&now))
	d.computed = []computedEntry{{Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch"}}

	// clock (500) is behind anchor (1000): clamp to 0, never negative.
	if got := d.ReadPath("status.device.uptime"); got != int64(0) {
		t.Errorf("uptime with clock behind anchor = %v, want 0", got)
	}

	// Anchor of 0 means "unset" -> 0 elapsed regardless of clock.
	d.state["status"].(map[string]any)["device"].(map[string]any)["boot_epoch"] = int64(0)
	if got := d.ReadPath("status.device.uptime"); got != int64(0) {
		t.Errorf("uptime with unset anchor = %v, want 0", got)
	}
}

// TestComputedSeedsAnchorOnInstall verifies AddComputedFromProfile seeds
// an absent/zero anchor to "now" so a freshly-booted device reads ~0 and
// counts up.
func TestComputedSeedsAnchorOnInstall(t *testing.T) {
	now := time.Unix(42_000, 0)
	state := map[string]any{
		"status": map[string]any{"device": map[string]any{"boot_epoch": int64(0)}},
	}
	d := newTestDevice(t, "d-1", state)
	d.SetClock(fixedClock(&now))
	if err := d.AddComputedFromProfile(&profile.Computed{
		Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch",
	}); err != nil {
		t.Fatalf("AddComputedFromProfile: %v", err)
	}

	// Anchor was seeded to the install-time clock.
	if got := d.state["status"].(map[string]any)["device"].(map[string]any)["boot_epoch"]; got != int64(42_000) {
		t.Errorf("seeded boot_epoch = %v (%T), want int64(42000)", got, got)
	}
	if got := d.ReadPath("status.device.uptime"); got != int64(0) {
		t.Errorf("uptime right after seed = %v, want 0", got)
	}
	now = time.Unix(42_077, 0)
	if got := d.ReadPath("status.device.uptime"); got != int64(77) {
		t.Errorf("uptime 77s after seed = %v, want 77", got)
	}
}

// TestComputedSnapshotOverlay confirms the whole-map read surface (the
// snapshot a consumer of the diff stream reads) sees the computed value at its
// natural key, without it ever being written to stored state.
func TestComputedSnapshotOverlay(t *testing.T) {
	now := time.Unix(2_000, 0)
	state := map[string]any{
		"status": map[string]any{"device": map[string]any{"boot_epoch": int64(1_500)}},
	}
	d := newTestDevice(t, "d-1", state)
	d.SetClock(fixedClock(&now))
	d.computed = []computedEntry{{Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch"}}

	snap := d.State()
	dev := snap["status"].(map[string]any)["device"].(map[string]any)
	if dev["uptime"] != int64(500) {
		t.Errorf("snapshot uptime = %v (%T), want int64(500)", dev["uptime"], dev["uptime"])
	}
	// The overlay lives in the copy only, stored state has no uptime key.
	stored := d.state["status"].(map[string]any)["device"].(map[string]any)
	if _, ok := stored["uptime"]; ok {
		t.Errorf("computed value leaked into stored state: %v", stored["uptime"])
	}
}

// TestComputedSNMPUptime checks the SNMP read surface: sysUpTime resolves
// the computed elapsed value and reports it in hundredths of a second.
func TestComputedSNMPUptime(t *testing.T) {
	now := time.Unix(5_000, 0)
	state := map[string]any{
		"status": map[string]any{"device": map[string]any{"boot_epoch": int64(4_877)}},
	}
	d := newTestDevice(t, "d-1", state)
	d.SetClock(fixedClock(&now))
	d.computed = []computedEntry{{Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch"}}

	v, err := d.snmpValue("1.3.6.1.2.1.1.3.0", "status.device.uptime", gosnmp.TimeTicks)
	if err != nil {
		t.Fatalf("snmpValue: %v", err)
	}
	// 123 seconds elapsed -> sysUpTime in hundredths = 12300.
	if v != uint32(12_300) {
		t.Errorf("sysUpTime = %v (%T), want uint32(12300)", v, v)
	}
}

// TestComputedReadsEmitNoEvents is the flood regression: uptime is read
// many times across an advancing clock and the DiffPublisher fires zero
// times. The old uptime_counter behavior published a StateChange every
// tick; a computed field must never publish.
func TestComputedReadsEmitNoEvents(t *testing.T) {
	now := time.Unix(0, 0)
	state := map[string]any{
		"status": map[string]any{"device": map[string]any{"boot_epoch": int64(0)}},
	}
	d := newTestDevice(t, "d-1", state)
	d.SetClock(fixedClock(&now))

	var published int
	d.DiffPublisher = func(StateChange) { published++ }

	if err := d.AddComputedFromProfile(&profile.Computed{
		Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch",
	}); err != nil {
		t.Fatalf("AddComputedFromProfile: %v", err)
	}

	for i := 0; i < 100; i++ {
		now = time.Unix(int64(i), 0)
		_ = d.ReadPath("status.device.uptime")
		_ = d.State()
	}
	if published != 0 {
		t.Errorf("DiffPublisher fired %d times across 100 reads; computed reads must emit no events", published)
	}
}
