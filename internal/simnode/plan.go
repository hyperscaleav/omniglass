// Package simnode is the simulator run mode (omniglass sim run): it runs
// beside a node with that node's credential, pulls the devices placed on the
// node, serves the ones it can through internal/sim, and reports their hits.
package simnode

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim"
)

// Skip is a listed device the simulator does not serve, and why.
type Skip struct {
	Name   string
	Reason string
}

// plan turns the server's device list into the snapshots to serve. A device
// is served when a simulator profile serves its product (the profile of that
// name, or a family profile that lists it) and its address is a loopback IP,
// the addresses a simulator sharing the node's network can own. Snapshots
// carry no state: each device starts from its profile's defaults, with its
// product's overlay and an identity derived from its component id.
func plan(devices []collection.SimDevice, profileFor func(string) (string, bool)) ([]sim.DeviceSnapshot, []Skip) {
	var snaps []sim.DeviceSnapshot
	var skips []Skip
	for _, d := range devices {
		reason := ""
		ip := net.ParseIP(d.Address)
		profileName, served := profileFor(d.Product)
		switch {
		case !served:
			reason = fmt.Sprintf("no simulator profile for product %q", d.Product)
		case d.Address == "":
			reason = "no address: no endpoint targets a host"
		case ip == nil:
			reason = fmt.Sprintf("%q is not an IP address", d.Address)
		case !ip.IsLoopback():
			reason = fmt.Sprintf("%s is not a loopback address", d.Address)
		}
		if reason != "" {
			skips = append(skips, Skip{Name: d.Name, Reason: reason})
			continue
		}
		snaps = append(snaps, sim.DeviceSnapshot{
			DeviceID: d.ComponentID, Hostname: d.Name, LoopbackIP: ip.String(),
			ProfileName: profileName, Product: d.Product, Version: 1,
		})
	}
	return snaps, skips
}

// Linux capability bits (linux/capability.h).
const (
	capNetBindService = 10
	capNetAdmin       = 12
)

// parseCapEff reads the effective capability set from /proc/self/status.
func parseCapEff(status string) (uint64, error) {
	sc := bufio.NewScanner(strings.NewReader(status))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "CapEff:"); ok {
			return strconv.ParseUint(strings.TrimSpace(v), 16, 64)
		}
	}
	return 0, fmt.Errorf("no CapEff line")
}

// capabilitiesFrom states what the simulator can do: bind ports below 1024
// (the bind capability, or the unprivileged-port sysctl lowered to 0, as a
// container runtime does), hold NET_ADMIN, and the shift on every port.
func capabilitiesFrom(capEff uint64, unprivPortStart, portOffset int) collection.SimCapabilities {
	return collection.SimCapabilities{
		LowPorts:   capEff&(1<<capNetBindService) != 0 || unprivPortStart == 0,
		NetAdmin:   capEff&(1<<capNetAdmin) != 0,
		PortOffset: portOffset,
	}
}
