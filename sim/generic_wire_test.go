//go:build linux

package sim

// The generic device's wire script (#2836): an unclassified SNMP box answers
// the two objects every agent does, and nothing else. It has no HTTP face, so
// its script is empty and its transcript is the SNMP reads and walks.

const genericProduct = "generic-device"

var genericWire = wireFamily{
	Name: "generic-device", Products: []string{genericProduct}, Engine: shippedEngineOf, Source: wireYAMLSource, Frozen: wireYAMLFrozen,
	OIDs:  []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.5.0", "1.3.6.1.4.1.1.1.0"},
	Walks: []string{"1.3.6.1.2.1.1", "1.3.6.1.2.1", "1.3.6.1.4.1"},
}

func init() { wireFamilies = append(wireFamilies, genericWire) }
