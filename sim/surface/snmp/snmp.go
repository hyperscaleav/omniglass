// Package snmp is the SNMP scalars every simulated agent repeats, in Go
// (#2831): the MIB-II system group's description and uptime, which every
// profile with an SNMP face maps, and the identity arc (firmware version and
// serial number at .5.1.0 and .5.2.0 under the vendor's enterprise number),
// which nine profiles repeat. Each reproduces the oid_map entries the
// profiles declare, with the types the server gives them.
package snmp

import (
	"strconv"

	"github.com/hyperscaleav/omniglass/sim/surface"
)

// The MIB-II system group's scalars.
const (
	OIDSysDescr  = "1.3.6.1.2.1.1.1.0"
	OIDSysUpTime = "1.3.6.1.2.1.1.3.0"
)

// System is the system group: what the device is and how long it has run.
type System struct {
	Descr         func() string
	UptimeSeconds func() int64
	// DescrPath and UptimePath are the state paths the two read, for the
	// console's listing of the OIDs a device serves.
	DescrPath, UptimePath string
}

// OIDs lists the system group's scalars.
func (s System) OIDs() []surface.OID {
	return []surface.OID{
		surface.Text(OIDSysDescr, s.Descr).At(s.DescrPath),
		surface.Ticks(OIDSysUpTime, func() uint32 { return uint32(s.UptimeSeconds() * 100) }).At(s.UptimePath),
	}
}

// Identity is a vendor's identity arc: the firmware version and serial
// number, under the vendor's private enterprise number. A nil accessor is a
// scalar the device does not answer (the Boreal Edge maps no firmware).
type Identity struct {
	Enterprise int
	Firmware   func() string
	Serial     func() string
	// FirmwarePath and SerialPath are the state paths the two read, for the
	// console's listing.
	FirmwarePath, SerialPath string
}

// OIDs lists the identity scalars.
func (i Identity) OIDs() []surface.OID {
	arc := "1.3.6.1.4.1." + strconv.Itoa(i.Enterprise) + ".5."
	var out []surface.OID
	if i.Firmware != nil {
		out = append(out, surface.Text(arc+"1.0", i.Firmware).At(i.FirmwarePath))
	}
	if i.Serial != nil {
		out = append(out, surface.Text(arc+"2.0", i.Serial).At(i.SerialPath))
	}
	return out
}
