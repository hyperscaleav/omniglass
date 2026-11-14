package storage

import "github.com/hyperscaleav/omniglass/internal/updatemask"

// maskedPtr is what a masked PATCH write stores for one field (ADR-0091, #1189):
// nil when the write does not name the field (leave the column alone), v when the
// caller supplied a value, and a pointer to the zero value when the write names
// the field but the body carries none. The last case is the capability the
// implied mask cannot express: under it "clear" and "leave alone" are the same
// request. A NOT NULL text column reads the zero value as its empty string, and a
// target or outlet arm reads it as "clear", so one rule serves every column.
//
// Under the implied mask (an absent update_mask) the write set is exactly the
// populated fields, so this returns v unchanged and a patch with no mask behaves
// byte for byte as it did before the mask existed.
func maskedPtr[T any](w updatemask.Fields, name string, v *T) *T {
	if !w.Has(name) {
		return nil
	}
	if v == nil {
		var zero T
		return &zero
	}
	return v
}

// populatedIf records name in the implied mask when the patch carries a value
// for it, the one rule every Populated method repeats.
func populatedIf(f updatemask.Fields, name string, on bool) {
	if on {
		f[name] = true
	}
}
