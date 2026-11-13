package sim

// DeviceSnapshot is one simulated device as the engine loads it: who it is,
// the loopback address it answers on, the profile that shapes it, and its
// starting state. They come from the server's seeded inventory in the sim run
// mode (#887), or from a system built to a standard in a test.
type DeviceSnapshot struct {
	DeviceID    string `json:"device_id"`
	Hostname    string `json:"hostname"`
	LoopbackIP  string `json:"loopback_ip"`
	ProfileName string `json:"profile_name"`
	// Product is the product the device is, when its profile simulates it
	// as one of several (empty: the profile's own product).
	Product string         `json:"product,omitempty"`
	Version int64          `json:"version"`
	State   map[string]any `json:"state"`
	// SiteCode, Room and Building locate the device for per-room identity
	// token expansion in a profile's defaults ({site}, {building}, {room},
	// {room-device:<profile>}). Empty where the source carries none.
	SiteCode string `json:"site_code,omitempty"`
	Room     string `json:"room,omitempty"`
	Building string `json:"building,omitempty"`
}

// mergeInto deep-merges src over dst in place: nested maps merge key by key,
// anything else in src replaces dst's value.
func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeInto(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}
