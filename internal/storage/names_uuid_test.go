package storage_test

import (
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A reference in a path or a join field accepts either a uuid or a name, and
// resolution tries the uuid first. That is only unambiguous if a name can never
// BE a uuid: otherwise `/components/019f8754-461f-7b82-b5f2-fc4bbe1c3765` means
// two different things depending on which entity happens to exist, and which one
// an operator gets would depend on data rather than intent.
//
// The slug rule alone does not prevent it. A uuid is lowercase hex and hyphens,
// so it satisfies `^[a-z0-9][a-z0-9-]*$` exactly.
func TestNamesCannotBeUUIDShaped(t *testing.T) {
	uuids := []string{
		"019f8754-461f-7b82-b5f2-fc4bbe1c3765", // a real one from the dev estate
		"00000000-0000-0000-0000-000000000000", // the nil uuid
		"019F8754-461F-7B82-B5F2-FC4BBE1C3765", // uppercase, already rejected by the slug rule
	}
	for _, u := range uuids {
		if err := storage.ValidateEntityName(u); !errors.Is(err, storage.ErrInvalidName) && !errors.Is(err, storage.ErrNameIsUUID) {
			t.Errorf("ValidateEntityName(%q) = %v, want a refusal: a name that is also a uuid "+
				"makes a reference ambiguous", u, err)
		}
	}
}

// The rule has to be narrow. Hyphenated hex-looking names are ordinary in an AV
// estate and must keep working; only the exact uuid shape is refused.
func TestHexLookingNamesAreStillFine(t *testing.T) {
	fine := []string{
		"boardroom-a",
		"019f8754",                              // hex, but not a uuid
		"019f8754-461f-7b82-b5f2",               // uuid-ish prefix, too short
		"019f8754-461f-7b82-b5f2-fc4bbe1c3765a", // one character too long
		"ab-cd-ef",
		"rack-1-dsp",
	}
	for _, n := range fine {
		if err := storage.ValidateEntityName(n); err != nil {
			t.Errorf("ValidateEntityName(%q) = %v, want nil: only the exact uuid shape is refused", n, err)
		}
	}
}

// The refusal has to be tellable apart from a plain slug violation, because a
// uuid satisfies the slug rule and the generic message would describe exactly
// what the operator typed.
func TestUUIDRefusalIsDistinguishable(t *testing.T) {
	if err := storage.ValidateEntityName("019f8754-461f-7b82-b5f2-fc4bbe1c3765"); !errors.Is(err, storage.ErrNameIsUUID) {
		t.Errorf("uuid-shaped name = %v, want ErrNameIsUUID", err)
	}
	// A genuine slug violation stays the generic error.
	if err := storage.ValidateEntityName("Not A Slug"); !errors.Is(err, storage.ErrInvalidName) {
		t.Errorf("bad slug = %v, want ErrInvalidName", err)
	}
}
