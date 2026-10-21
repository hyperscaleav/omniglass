package devseed

// ReplicaScenario is the large scenario at a chosen size, so a database test
// can prove the replica path without paying for the full fleet.
func ReplicaScenario(p ReplicaParams) Scenario {
	return Scenario{Name: "replica", expand: func(d Doc) Doc { return Replicate(d, p) }}
}

// SelfContainedRooms exposes the template choice to the shape tests.
func SelfContainedRooms(doc Doc) []string { return selfContainedRooms(doc) }
