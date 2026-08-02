package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestTypeReferencesAcceptEitherForm pins the ADR-0062 write contract #468
// finishes: a location_type or secret_type reference in a write body resolves
// whether the caller sends the uuid or the kebab handle, like every other
// registry reference (the registryRefCol pattern). Before the fix the uuid
// form silently resolved to NULL and the insert failed.
func TestTypeReferencesAcceptEitherForm(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	// The uuid of the seeded campus location type and snmp-community secret type.
	var campusID, snmpID string
	for _, lt := range mustLocationTypes(t, gw) {
		if lt.Name == "campus" {
			campusID = lt.ID
		}
	}
	sts, err := gw.ListSecretTypes(ctx)
	if err != nil {
		t.Fatalf("list secret types: %v", err)
	}
	for _, st := range sts {
		if st.Name == "snmp-community" {
			snmpID = st.ID
		}
	}
	if campusID == "" || snmpID == "" {
		t.Fatal("seeded types not found")
	}

	// Location create and update, by uuid and by handle.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-uuid", LocationType: campusID}, all); err != nil {
		t.Errorf("create location by type uuid: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-handle", LocationType: "campus"}, all); err != nil {
		t.Errorf("create location by type handle: %v", err)
	}
	campus := "campus"
	campusRef := campusID
	if _, err := gw.UpdateLocation(ctx, "", "hq-handle", storage.LocationPatch{LocationType: &campusRef}, all, all); err != nil {
		t.Errorf("update location by type uuid: %v", err)
	}
	if _, err := gw.UpdateLocation(ctx, "", "hq-uuid", storage.LocationPatch{LocationType: &campus}, all, all); err != nil {
		t.Errorf("update location by type handle: %v", err)
	}

	// Secret create, by uuid and by handle.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "by-uuid", SecretType: snmpID, OwnerKind: "platform",
		Fields: map[string]string{"community": "x"},
	}, all, true); err != nil {
		t.Errorf("create secret by type uuid: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "by-handle", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "x"},
	}, all, true); err != nil {
		t.Errorf("create secret by type handle: %v", err)
	}

	// A reference that is neither a live uuid nor a live handle still fails
	// loudly, in both shapes.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "ghost", LocationType: "no-such-type"}, all); err == nil {
		t.Error("create location with unknown type handle succeeded")
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "ghost", SecretType: "00000000-0000-0000-0000-000000000000", OwnerKind: "platform",
		Fields: map[string]string{"community": "x"},
	}, all, true); !errors.Is(err, storage.ErrUnknownSecretType) && err == nil {
		t.Error("create secret with unknown type uuid succeeded")
	}
}

func mustLocationTypes(t *testing.T, gw storage.Gateway) []storage.LocationType {
	t.Helper()
	lts, err := gw.ListLocationTypes(context.Background())
	if err != nil {
		t.Fatalf("list location types: %v", err)
	}
	return lts
}
