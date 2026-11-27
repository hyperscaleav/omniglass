package profile

import "strings"

// ExpandIdentityTokens returns a deep copy of a device's defaults map with
// identity tokens expanded in every string value:
//
//	{site}                  -> site code (e.g. BOI)
//	{building}              -> building (e.g. 17C)
//	{room}                  -> room (e.g. 300)
//	{room-device:<profile>} -> hostname of the device with that profile name
//	                           in the same room (empty when the room has none)
//
// A device profile is shared: it fans out to every room in the fleet. Any
// identity baked into its defaults as a literal (a UCC account, a paired
// peer's hostname) would otherwise appear identically on every instance.
// Expanding these tokens per device makes the value room-specific. The
// {room-device:<profile>} form lets a device name a peer whose hostname
// suffix is system-specific (e.g. a video bar naming its paired touch
// panel, which is "...-TP1" in one system and "...-touch-panel1" in
// another).
//
// The input map is never modified, profile defaults are shared across all
// instances of the profile, so a deep copy is returned.
func ExpandIdentityTokens(defaults map[string]any, site, building, room string, peers map[string]string) map[string]any {
	if defaults == nil {
		return nil
	}
	expand := func(s string) string {
		if !strings.Contains(s, "{") {
			return s
		}
		s = strings.ReplaceAll(s, "{site}", site)
		s = strings.ReplaceAll(s, "{building}", building)
		s = strings.ReplaceAll(s, "{room}", room)
		for {
			i := strings.Index(s, "{room-device:")
			if i < 0 {
				break
			}
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				break
			}
			end += i
			s = s[:i] + peers[s[i+len("{room-device:"):end]] + s[end+1:]
		}
		return s
	}
	var clone func(v any) any
	clone = func(v any) any {
		switch val := v.(type) {
		case map[string]any:
			out := make(map[string]any, len(val))
			for k, e := range val {
				out[k] = clone(e)
			}
			return out
		case []any:
			out := make([]any, len(val))
			for i, e := range val {
				out[i] = clone(e)
			}
			return out
		case string:
			return expand(val)
		default:
			return v
		}
	}
	return clone(defaults).(map[string]any)
}
