package devseed

// Tier is one of the three things the lab-level check holds the fleet to
// (#1371, the live counterpart of #1266's outcome).
type Tier string

// The tiers: a component's built-in data (the node's and the platform's own
// account of it), its collected data (a class signal from the device's API
// beyond identity and uptime), and a system's value of its own.
const (
	TierBuiltIn   Tier = "built-in"
	TierCollected Tier = "collected"
	TierSystem    Tier = "system"
)

// AllSystems is the one key a system gap holds: the fixture's systems carry no
// stable key the check can name, so the gap covers them all until #1361 gives
// each a value of its own.
const AllSystems = "*"

// TierGap is an owner the lab does not satisfy on a tier yet, and the open
// issue that closes it.
type TierGap struct {
	Issue string
}

// TierGaps are the known gaps per tier, keyed by the product a component gap
// covers (every placed component of it) or AllSystems. The lab-level check
// skips a listed owner and fails a listed owner that now carries the tier, so
// an entry does not outlive the issue that closes it. The list is empty when
// #1266 is done.
var TierGaps = map[Tier]map[string]TierGap{
	TierBuiltIn: {},
	TierCollected: {
		"lyra-gn-12":     {Issue: "#1367"}, // passive gear: lands its feeder's per-channel telemetry through the fan-out
		"polaris-sky-c6": {Issue: "#1367"},
	},
	TierSystem: {
		AllSystems: {Issue: "#1361"}, // a system rolls up its members' telemetry
	},
}
