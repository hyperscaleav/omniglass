package devseed

// The dev lab's known telemetry gaps (#1371), each with the issue that closes
// it. The lab's telemetry check (make test-lab) expects the owners they cover
// to fall short, and fails an entry as stale once every owner it covers meets
// its tier. Unsimulated and passive products are gaps too, read from the
// simulator catalog's coverage.yaml rather than listed here. The lists are
// empty when #1266 is done.

// CollectedGaps are products whose components carry no live collected value
// beyond identity and uptime, keyed by product.
var CollectedGaps = map[string]string{
	// Collected only through snmp-generic: model-number and uptime.
	"boreal-edge-55": "#1372",
	"boreal-edge-65": "#1372",
	"boreal-edge-75": "#1372",
	"boreal-edge-86": "#1372",
	"boreal-edge-98": "#1372",
	"polaris-dsp-16": "#1372",
	// The power conditioner, on no catalog product, until it is a networked
	// rack power product.
	"generic-device": "#1368",
}

// SystemGaps are systems with no live value of their own, keyed by fixture
// system key: a system rolls up its members' values from its standard's rules
// (#1361). The rest already carry one (a fanned-out call state, a pushed
// booking).
var SystemGaps = map[string]string{
	"boardroom-b": "#1361",
	"lab-pod-a":   "#1361", "lab-pod-b": "#1361",
	"bay-1-av": "#1361",
	"wl3-s1":   "#1361", "wl3-s2": "#1361", "wl3-s3": "#1361", "wl3-s4": "#1361", "wl3-s5": "#1361", "wl3-s6": "#1361",
	"wl4-s1": "#1361", "wl4-s2": "#1361", "wl4-s3": "#1361", "wl4-s4": "#1361", "wl4-s5": "#1361",
	"hl2-s1": "#1361", "hl2-s2": "#1361", "hl2-s3": "#1361", "hl2-s4": "#1361", "hl2-s5": "#1361", "hl2-s6": "#1361",
	"pava-s1": "#1361", "pava-s2": "#1361", "pava-s3": "#1361", "pava-s4": "#1361", "pava-s5": "#1361",
	"pavb-s1": "#1361", "pavb-s2": "#1361", "pavb-s3": "#1361", "pavb-s4": "#1361", "pavb-s5": "#1361",
	"ann-s1": "#1361", "ann-s2": "#1361", "ann-s3": "#1361",
	"dep-s1": "#1361", "dep-s2": "#1361", "dep-s3": "#1361",
}
