// Package impair breaks simulated devices on purpose: unplugged (dark on
// every protocol), slow, lossy or rate-limited, applied in the network the
// simulator shares with its node, so a collector sees exactly what a failing
// device would show. It also makes ping truthful: every loopback address
// answers ICMP, so the addresses that are not devices are told to stay
// silent. Applying needs NET_ADMIN.
package impair

import (
	"fmt"
	"math"
	"net/netip"
	"slices"
)

// Impairment is what is wrong with one simulated device. The zero value is a
// healthy device.
type Impairment struct {
	Unplugged bool    `json:"unplugged"`
	LatencyMs int     `json:"latency_ms"`
	JitterMs  int     `json:"jitter_ms"`
	LossPct   float64 `json:"loss_pct"`
	RateKbit  int     `json:"rate_kbit"`
}

// maxLatencyMs bounds a requested delay: ten seconds is already far past
// any collector's timeout.
const maxLatencyMs = 10_000

// maxRateKbit bounds a rate limit at 10 Gbit/s, past any link a simulated
// device stands for (and inside the column that stores it).
const maxRateKbit = 10_000_000

// Validate refuses an impairment the network cannot apply.
func (i Impairment) Validate() error {
	switch {
	case i.LatencyMs < 0 || i.LatencyMs > maxLatencyMs:
		return fmt.Errorf("latency_ms %d is outside 0 to %d", i.LatencyMs, maxLatencyMs)
	case i.JitterMs < 0 || i.JitterMs > i.LatencyMs:
		return fmt.Errorf("jitter_ms %d must be between 0 and the latency (%d)", i.JitterMs, i.LatencyMs)
	case i.LossPct < 0 || i.LossPct > 100:
		return fmt.Errorf("loss_pct %g is outside 0 to 100", i.LossPct)
	case i.RateKbit < 0 || i.RateKbit > maxRateKbit:
		return fmt.Errorf("rate_kbit %d is outside 0 to %d", i.RateKbit, maxRateKbit)
	}
	return nil
}

// Normalized holds loss to hundredths of a percent, the precision it is
// stored at, so what is set is what reads back.
func (i Impairment) Normalized() Impairment {
	i.LossPct = math.Round(i.LossPct*100) / 100
	return i
}

// Shaped reports whether the impairment degrades the link (delay, jitter,
// loss or a rate limit) rather than cutting it.
func (i Impairment) Shaped() bool {
	return i.LatencyMs > 0 || i.JitterMs > 0 || i.LossPct > 0 || i.RateKbit > 0
}

// Device is one served device and what is wrong with it.
type Device struct {
	Addr       netip.Addr
	Impairment Impairment
}

// Shape is one device whose link is degraded.
type Shape struct {
	Addr netip.Addr
	Impairment
}

// Plan is the network state that realizes a fleet's impairments: the
// devices (so ping answers only for them), the unplugged ones (dark), and
// the shaped ones.
type Plan struct {
	Devices   []netip.Addr
	Unplugged []netip.Addr
	Shaped    []Shape
}

// PlanFor turns the served devices into a plan, sorted so equal fleets plan
// equally. An unplugged device is not shaped: it is dark either way.
func PlanFor(devs []Device) Plan {
	var p Plan
	for _, d := range devs {
		p.Devices = append(p.Devices, d.Addr)
		switch {
		case d.Impairment.Unplugged:
			p.Unplugged = append(p.Unplugged, d.Addr)
		case d.Impairment.Shaped():
			p.Shaped = append(p.Shaped, Shape{Addr: d.Addr, Impairment: d.Impairment})
		}
	}
	slices.SortFunc(p.Devices, func(a, b netip.Addr) int { return a.Compare(b) })
	slices.SortFunc(p.Unplugged, func(a, b netip.Addr) int { return a.Compare(b) })
	slices.SortFunc(p.Shaped, func(a, b Shape) int { return a.Addr.Compare(b.Addr) })
	return p
}

// Equal reports whether two plans realize the same network state.
func (p Plan) Equal(q Plan) bool {
	return slices.Equal(p.Devices, q.Devices) && slices.Equal(p.Unplugged, q.Unplugged) && slices.Equal(p.Shaped, q.Shaped)
}
