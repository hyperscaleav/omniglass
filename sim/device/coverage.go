package device

// Coverage accounts for the seeded products on the network that no family
// serves yet, and for the ones with nothing on the network to simulate. The
// catalog's alignment test (sim) holds the seed's products to it, and the dev
// fleet's coverage test (internal/devseed) holds every component the lab
// places to it, so what is left to simulate is always current.
type Coverage struct {
	// Unsimulated are products on the network (they have a driver) that no
	// family serves yet, each with the issue that adds one.
	Unsimulated map[string]string
	// Passive are products with no network interface (so no driver), with
	// nothing to collect from, each with how the fleet watches them instead.
	Passive map[string]string
}

// Accounted is the shipped catalog's coverage accounting. A new seeded product
// either gains a family or is listed here.
func Accounted() Coverage {
	return Coverage{
		Unsimulated: map[string]string{},
		Passive: map[string]string{
			"lyra-gn-12":     "an analog gooseneck microphone; the DSP input it is wired to reports its level and mute (#1367)",
			"polaris-sky-c6": "a passive loudspeaker; the amplifier channel driving it reports its load (#1367)",
		},
	}
}
