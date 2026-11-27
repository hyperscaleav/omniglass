package behave

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"sync"
)

// Source is the one way a primitive draws randomness: a per-device seeded
// source the caller passes in. A primitive never reaches a global generator,
// so the same seed and the same calls give the same run.
type Source interface {
	// Uniform is a value uniformly distributed in [lo, hi].
	Uniform(lo, hi float64) float64
	// IntN is a uniform integer in [lo, hi].
	IntN(lo, hi int64) int64
	// Chance is true with probability p.
	Chance(p float64) bool
}

// DeviceSource is the Source of one device, seeded from its id the way the
// engine seeds the source its profile behaviors draw from, so a device
// replayed through Go draws the stream it drew through its profile. Safe for
// concurrent callers.
type DeviceSource struct {
	mu  sync.Mutex
	rng *rand.Rand
}

// NewSource is the source of the device with the id.
func NewSource(deviceID string) *DeviceSource {
	sum := sha256.Sum256([]byte(deviceID))
	seed := int64(binary.BigEndian.Uint64(sum[:8]))
	return &DeviceSource{rng: rand.New(rand.NewSource(seed))}
}

// Uniform implements Source.
func (d *DeviceSource) Uniform(lo, hi float64) float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if hi < lo {
		lo, hi = hi, lo
	}
	return lo + d.rng.Float64()*(hi-lo)
}

// IntN implements Source.
func (d *DeviceSource) IntN(lo, hi int64) int64 {
	if hi < lo {
		lo, hi = hi, lo
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return lo + d.rng.Int63n(hi-lo+1)
}

// Chance implements Source.
func (d *DeviceSource) Chance(p float64) bool { return d.Uniform(0, 1) < p }

// Step is v moved by a uniform draw within plus or minus step.
func Step(rng Source, step, v float64) float64 { return v + rng.Uniform(-step, step) }
