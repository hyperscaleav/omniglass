package storage_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// ParseAddress is pure: no database, no scope. Each case below is chosen to
// fail if the corresponding piece of grammar is reverted, per the task's own
// warning that this branch has twice shipped a critical nothing in the
// existing suite could catch: TestParseAddressRevertKills documents which of
// these are hand-verified to fail on a specific reversion, in path_test.go's
// own package comment rather than a separate file.
func TestParseAddressFullComponentPath(t *testing.T) {
	addr, err := storage.ParseAddress("boi.17c.415a.$comp.display-1")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	want := &storage.Address{
		Root: []string{"boi", "17c", "415a"},
		Kind: storage.AddressComponent,
		Tail: []string{"display-1"},
	}
	if !reflect.DeepEqual(addr, want) {
		t.Errorf("ParseAddress = %+v, want %+v", addr, want)
	}
}

func TestParseAddressSubComponentPath(t *testing.T) {
	addr, err := storage.ParseAddress("boi.17c.415a.$comp.dsp-1.dante-card-1")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	want := &storage.Address{
		Root: []string{"boi", "17c", "415a"},
		Kind: storage.AddressComponent,
		Tail: []string{"dsp-1", "dante-card-1"},
	}
	if !reflect.DeepEqual(addr, want) {
		t.Errorf("ParseAddress = %+v, want %+v", addr, want)
	}
}

func TestParseAddressSystemPath(t *testing.T) {
	addr, err := storage.ParseAddress("boi.17c.$sys.av")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	want := &storage.Address{
		Root: []string{"boi", "17c"},
		Kind: storage.AddressSystem,
		Tail: []string{"av"},
	}
	if !reflect.DeepEqual(addr, want) {
		t.Errorf("ParseAddress = %+v, want %+v", addr, want)
	}
}

// A role address switches plane twice: $sys to reach the owning system, then
// $role for its role. Not in the task's required list but the grammar
// explicitly reserves the accessor, so it is covered here.
func TestParseAddressSystemRolePath(t *testing.T) {
	addr, err := storage.ParseAddress("boi.17c.$sys.av.$role.primary-dsp")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	want := &storage.Address{
		Root: []string{"boi", "17c"},
		Kind: storage.AddressRole,
		Tail: []string{"av"},
		Role: "primary-dsp",
	}
	if !reflect.DeepEqual(addr, want) {
		t.Errorf("ParseAddress = %+v, want %+v", addr, want)
	}
}

// A root-only ref (no dot, no accessor) is NOT an address: it is a bare name,
// and #627 Task 10/11's ambiguous bare-name resolution already handles it.
// ParseAddress declining is what keeps that behavior untouched.
func TestParseAddressRootOnlyIsNotAnAddress(t *testing.T) {
	addr, err := storage.ParseAddress("boi")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	if addr != nil {
		t.Errorf("ParseAddress(%q) = %+v, want nil (a bare name, not an address)", "boi", addr)
	}
}

// There is no $loc: the location plane is the root plane (the grammar
// section of the task brief, verbatim). $loc must be refused, not silently
// treated as a location-plane switch that does nothing.
func TestParseAddressRejectsDollarLoc(t *testing.T) {
	_, err := storage.ParseAddress("boi.17c.$loc.415a")
	if err == nil {
		t.Fatal("ParseAddress accepted $loc; there is no $loc accessor")
	}
	var invalid *storage.ErrInvalidAddress
	if !errors.As(err, &invalid) {
		t.Fatalf("got %T, want *storage.ErrInvalidAddress", err)
	}
	if !strings.Contains(invalid.Msg, "$loc") {
		t.Errorf("message %q does not name the bad accessor", invalid.Msg)
	}
}

// url.PathEscape leaves "$" bare, so an unquoted CLI invocation
// (omniglass component get boi.17c.415a.$comp.display-1) lets the shell
// glob-expand or drop the unquoted $comp, and the resulting empty segment
// must name that cause, not just say "invalid".
func TestParseAddressEmptySegmentNamesTheShellCause(t *testing.T) {
	_, err := storage.ParseAddress("boi.17c.415a..display-1")
	if err == nil {
		t.Fatal("ParseAddress accepted an empty segment")
	}
	var invalid *storage.ErrInvalidAddress
	if !errors.As(err, &invalid) {
		t.Fatalf("got %T, want *storage.ErrInvalidAddress", err)
	}
	const want = "an accessor like $comp must be quoted in a shell"
	if !strings.Contains(invalid.Msg, want) {
		t.Errorf("message %q does not name the shell cause %q", invalid.Msg, want)
	}
}

// A percent-encoded slash arrives at the handler already decoded (the #627
// preflight probe: GET .../boi.17c%2Fbad reaches the handler as boi.17c/bad),
// so the segment "17c/bad" must fail the entity name rule here, before any
// query runs, rather than being handed to a WHERE clause verbatim.
func TestParseAddressRejectsASegmentFailingTheNameRule(t *testing.T) {
	_, err := storage.ParseAddress("boi.17c/bad.415a")
	if err == nil {
		t.Fatal("ParseAddress accepted a segment containing a slash")
	}
	var invalid *storage.ErrInvalidAddress
	if !errors.As(err, &invalid) {
		t.Fatalf("got %T, want *storage.ErrInvalidAddress", err)
	}
}

// A uuid is never dotted and never accessor-bearing, but is tested explicitly
// so the two dispatch paths (uuid vs address) never accidentally overlap.
func TestParseAddressUUIDPassthrough(t *testing.T) {
	const uuid = "019f8754-3a1b-7c2d-8e4f-5a6b7c8d9e0f"
	addr, err := storage.ParseAddress(uuid)
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	if addr != nil {
		t.Errorf("ParseAddress(uuid) = %+v, want nil (a uuid is not an address)", addr)
	}
}

// An address that opens directly on an accessor names an unplaced row
// (component_orphan_name_key): Root is legitimately empty.
func TestParseAddressUnplacedComponent(t *testing.T) {
	addr, err := storage.ParseAddress("$comp.display-1")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	want := &storage.Address{Kind: storage.AddressComponent, Tail: []string{"display-1"}}
	if !reflect.DeepEqual(addr, want) {
		t.Errorf("ParseAddress = %+v, want %+v", addr, want)
	}
}

func TestParseAddressAccessorWithNothingAfterIsInvalid(t *testing.T) {
	_, err := storage.ParseAddress("boi.17c.$comp")
	if err == nil {
		t.Fatal("ParseAddress accepted an address ending on a bare accessor")
	}
	var invalid *storage.ErrInvalidAddress
	if !errors.As(err, &invalid) {
		t.Fatalf("got %T, want *storage.ErrInvalidAddress", err)
	}
}

func TestParseAddressRoleOutsideSystemPathIsInvalid(t *testing.T) {
	_, err := storage.ParseAddress("boi.17c.$comp.dsp-1.$role.primary-dsp")
	if err == nil {
		t.Fatal("ParseAddress accepted $role within a component path")
	}
	var invalid *storage.ErrInvalidAddress
	if !errors.As(err, &invalid) {
		t.Fatalf("got %T, want *storage.ErrInvalidAddress", err)
	}
}

func TestParseAddressMidPathAccessorIsInvalid(t *testing.T) {
	_, err := storage.ParseAddress("boi.$comp.a.$sys.b")
	if err == nil {
		t.Fatal("ParseAddress accepted a second plane switch mid-path")
	}
}

// RejectAddressForm is the companion check for the 22 registry routes whose
// names stay a single global kebab token (node, tag, and the four lane
// types): #627's address grammar covers only component/system/location, and
// a dotted ref sent to one of these must be refused explicitly, never fall
// through to a bare 404 that never says why it missed.
func TestRejectAddressFormRefusesADottedRefForARegistryKind(t *testing.T) {
	err := storage.RejectAddressForm("node", "boi.17c")
	if err == nil {
		t.Fatal("RejectAddressForm accepted a dotted ref for a single-token registry")
	}
	var notAccepted *storage.ErrAddressNotAccepted
	if !errors.As(err, &notAccepted) {
		t.Fatalf("got %T, want *storage.ErrAddressNotAccepted", err)
	}
	if notAccepted.Kind != "node" {
		t.Errorf("Kind = %q, want %q", notAccepted.Kind, "node")
	}
	if !strings.Contains(err.Error(), "single token") {
		t.Errorf("message %q does not say a name of this kind is a single token", err.Error())
	}
}

func TestRejectAddressFormRefusesALeadingAccessor(t *testing.T) {
	if err := storage.RejectAddressForm("tag", "$comp.foo"); err == nil {
		t.Fatal("RejectAddressForm accepted a leading accessor for a single-token registry")
	}
}

func TestRejectAddressFormPassesOrdinaryNamesAndUUIDs(t *testing.T) {
	const uuid = "019f8754-3a1b-7c2d-8e4f-5a6b7c8d9e0f"
	for _, ref := range []string{"node-1", uuid, "icmp-rtt-avg"} {
		if err := storage.RejectAddressForm("node", ref); err != nil {
			t.Errorf("RejectAddressForm(%q) = %v, want nil", ref, err)
		}
	}
}
