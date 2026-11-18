// Package fleet holds the two helpers the simulator and the scale lab share
// when they name and identify simulated things: Enrich, which gives a device
// its per-instance identity, and NameGenerator, which labels a system. The
// fleet itself is not defined here: a simulated system is built to a standard
// from the platform's template catalog (internal/lab/scenario resolves one).
package fleet

import (
	"fmt"
	"hash/fnv"
	"net"
	"strings"
)

// Enrich writes a device's per-instance identity into its state in place,
// under a blank-leaf rule (a field is written only where the profile declares it blank): the hostname, a serial and
// MAC derived from hostID, and the address. The simulator calls it for a
// device the server assigns, so a device's serial is the same function of
// its id however it was listed.
func Enrich(state map[string]any, hostname, hostID string, ip net.IP) {
	setBlankLeaf(state, "configuration.network.hostname", hostname)
	setBlankLeaf(state, "configuration.device.serial", generateSerial(hostID))
	if ip != nil {
		setBlankLeaf(state, "status.network.ipv4.address", ip.String())
	}
	setBlankLeaf(state, "status.network.ipv4.mac", generateMAC(hostID))
}

// setBlankLeaf walks the dotted path through nested map[string]any and
// sets the leaf to value, but only when the leaf key already exists and
// currently holds an empty string. Missing intermediate keys, non-map
// intermediates, and non-blank or non-string leaves are all left
// untouched, so the profile schema stays the single source of which
// fields a device actually has.
func setBlankLeaf(m map[string]any, dotted, value string) {
	keys := strings.Split(dotted, ".")
	cur := m
	for i, k := range keys {
		if i == len(keys)-1 {
			if existing, ok := cur[k].(string); ok && existing == "" {
				cur[k] = value
			}
			return
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
}

// generateSerial produces a stable, realistic-looking serial number from
// the device's HostID. 10 uppercase hex digits behind an "SN-" prefix.
func generateSerial(hostID string) string {
	h := fnv.New64a()
	h.Write([]byte("serial/" + hostID))
	return fmt.Sprintf("SN-%010X", h.Sum64()&0xFFFFFFFFFF)
}

// generateMAC produces a stable locally-administered unicast MAC from the
// device's HostID. First octet 0x02 sets the locally-administered bit and
// clears the multicast bit; the remaining five octets come from the hash.
// MACs are cosmetic here (everything talks to localhost), so collisions
// across the fleet are harmless.
func generateMAC(hostID string) string {
	h := fnv.New64a()
	h.Write([]byte("mac/" + hostID))
	sum := h.Sum64()
	return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x",
		byte(sum>>32), byte(sum>>24), byte(sum>>16), byte(sum>>8), byte(sum))
}
