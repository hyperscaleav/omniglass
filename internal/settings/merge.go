package settings

// DeepMerge overlays layers left to right: a later layer wins. Nested maps merge
// recursively; any non-map value (including a slice) replaces wholesale. Inputs
// are never mutated; the result is a fresh deep copy. Merging in generic-map space
// is deliberate: key presence, not a Go zero-value, decides an override, so a key
// set to false or 0 overrides while an absent key inherits.
func DeepMerge(layers ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, layer := range layers {
		for k, v := range layer {
			if sub, ok := v.(map[string]any); ok {
				if existing, ok := out[k].(map[string]any); ok {
					out[k] = DeepMerge(existing, sub)
					continue
				}
				out[k] = DeepMerge(sub) // deep copy
				continue
			}
			out[k] = v
		}
	}
	return out
}

// ApplyMergePatch applies an RFC 7386 JSON Merge Patch to target: a nil value in
// patch deletes that key (restoring it to whatever a lower layer supplies), a map
// value recurses, and any other value replaces. Inputs are never mutated. This is
// how a PATCH write updates an override namespace: null on a key is "restore this
// one key to default".
func ApplyMergePatch(target, patch map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range target {
		out[k] = v
	}
	for k, v := range patch {
		if v == nil {
			delete(out, k)
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			if existing, ok := out[k].(map[string]any); ok {
				out[k] = ApplyMergePatch(existing, sub)
				continue
			}
			out[k] = ApplyMergePatch(map[string]any{}, sub)
			continue
		}
		out[k] = v
	}
	return out
}
