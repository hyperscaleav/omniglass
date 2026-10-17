package sim

import "github.com/hyperscaleav/omniglass/internal/sim/fleet"

// DeviceSnapshot is one simulated device as the engine loads it: who it is,
// the loopback address it answers on, the profile that shapes it, and its
// starting state. They come from a compiled fleet (internal/sim/fleet) or, in the
// sim run mode, from the server's seeded inventory (#887).
type DeviceSnapshot struct {
	DeviceID    string         `json:"device_id"`
	Hostname    string         `json:"hostname"`
	LoopbackIP  string         `json:"loopback_ip"`
	ProfileName string         `json:"profile_name"`
	Version     int64          `json:"version"`
	State       map[string]any `json:"state"`
	// SiteCode, Room and Building locate the device for per-room identity
	// token expansion in a profile's defaults ({site}, {building}, {room},
	// {room-device:<profile>}). Empty where the source carries none.
	SiteCode string `json:"site_code,omitempty"`
	Room     string `json:"room,omitempty"`
	Building string `json:"building,omitempty"`
}

// Snapshots turns a compiled fleet into the engine's device list: the
// standalone path, with no server between the catalog and the listeners.
// DeviceID is the compiler's stable slug, so a recompile of the same
// manifest addresses the same devices; each device starts from its own
// copy of its profile defaults.
func Snapshots(est *fleet.CompiledFleet) []DeviceSnapshot {
	out := make([]DeviceSnapshot, 0, len(est.Devices))
	for _, d := range est.Devices {
		out = append(out, DeviceSnapshot{
			DeviceID:    d.HostID,
			Hostname:    d.Hostname,
			LoopbackIP:  d.LoopbackIP.String(),
			ProfileName: d.ProfileName,
			Version:     1,
			State:       deepCopy(d.Defaults),
			SiteCode:    d.SiteCode,
			Room:        d.Room,
			Building:    d.Building,
		})
	}
	return out
}
