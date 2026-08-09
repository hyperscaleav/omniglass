// Package updatemask is the write mask a PATCH carries: which fields of a
// resource the request actually writes, so a caller can CLEAR a field and not
// only set one (#666).
//
// The rules are AIP-134's, exactly:
//
//   - An ABSENT mask is the implied mask of the fields the body populated (a
//     field with a non-empty value). This is what every route in the tree did
//     before the mask existed, so an absent mask is always today's behavior.
//   - A PRESENT mask writes exactly the fields it names, populated or not, so
//     a named field carrying its zero value clears. This is the capability the
//     implied mask cannot express: the implied mask covers only populated
//     fields, so under it "clear" and "leave alone" are the same request.
//   - The special value "*" is full replacement, the equivalent of a PUT:
//     every patchable field is written, and one the body omits is cleared.
//   - A mask naming a field the resource does not patch is an Error naming the
//     field, never a silent no-op. A mask is the caller stating intent, and a
//     misspelled intent that succeeds is worse than one that fails.
//
// Top-level field names only, spelled exactly as the wire body spells them
// (#666's thin cut: no nested paths, which api.md still carries as an open
// question). Nothing here touches HTTP or storage: Resolve is a pure function
// over three lists, which is what lets the same primitive serve any PATCH body
// without modification.
package updatemask

import (
	"fmt"
	"sort"
	"strings"
)

// Star is the full-replacement mask, AIP-134's "the equivalent of PUT".
const Star = "*"

// Fields is a resolved write set: the field names one write writes. A field
// outside the set keeps whatever the stored resource already has (or, when the
// write creates the resource, its default). The zero value (a nil map) is a
// legitimate empty set, so a mask that names nothing writes nothing.
type Fields map[string]bool

// Of builds a set from names, for callers that know their write set outright
// (a create, a test, a caller with no wire mask to resolve).
func Of(names ...string) Fields {
	f := make(Fields, len(names))
	for _, n := range names {
		f[n] = true
	}
	return f
}

// Has reports whether this write writes the named field.
func (f Fields) Has(name string) bool { return f[name] }

// Names returns the set sorted, for a message or an audit line that has to
// render the set deterministically.
func (f Fields) Names() []string {
	out := make([]string, 0, len(f))
	for n, on := range f {
		if on {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Implied is the write set an ABSENT update_mask implies: exactly the fields
// the body populated, AIP-134's "an implied field mask equivalent to all fields
// that are populated". Resolve returns this for a nil mask; it is exported so a
// layer holding no wire mask at all (a caller that predates #666) reaches the
// same rule rather than reimplementing it.
//
// The result is a copy: the write set outlives the caller's populated map, and
// a consumer that narrows one must not silently narrow the other.
func Implied(populated Fields) Fields {
	out := make(Fields, len(populated))
	for n, on := range populated {
		if on {
			out[n] = true
		}
	}
	return out
}

// Error is a mask the resource cannot honor: it names a field the resource
// does not patch, or it combines "*" with named fields. Field is the offending
// path and Patchable is what the resource would have accepted, because a
// refusal an operator cannot act on is the same dead end as a silent no-op
// (the whole reason this is an error rather than a skip).
type Error struct {
	Field     string
	Reason    string
	Patchable []string
}

func (e *Error) Error() string {
	return fmt.Sprintf("update_mask names %q, which %s; this resource patches %s",
		e.Field, e.Reason, strings.Join(e.Patchable, ", "))
}

// Resolve turns the wire mask into the write set for one request.
//
// mask is the body's update_mask exactly as it arrived: NIL when the caller
// omitted the field (the implied mask, so populated wins), non-nil when the
// caller sent one, INCLUDING an empty array, which is a caller explicitly
// naming no fields and therefore writing none. Callers must not substitute an
// empty slice for an omitted field: the two are different requests.
//
// patchable is every field this resource lets a PATCH write, in the order the
// refusal should list them. populated is the implied mask: the fields the body
// carried a non-empty value for.
func Resolve(mask []string, patchable []string, populated Fields) (Fields, error) {
	if mask == nil {
		return Implied(populated), nil
	}
	known := Of(patchable...)
	out := make(Fields, len(mask))
	for _, name := range mask {
		if name == Star {
			if len(mask) != 1 {
				return nil, &Error{
					Field:     Star,
					Reason:    "means full replacement and cannot be combined with named fields",
					Patchable: patchable,
				}
			}
			return Of(patchable...), nil
		}
		if !known.Has(name) {
			return nil, &Error{
				Field:     name,
				Reason:    "is not a field this resource patches",
				Patchable: patchable,
			}
		}
		out[name] = true
	}
	return out, nil
}
