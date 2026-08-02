package storage_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestRegistryAuditImages pins the fuller trail #228 asks for: a registry
// update records the row as it stood before the write beside the row after
// it, and a delete records the whole row it removed, not a bare id. Both
// images come from the same row shape (column-keyed), so the audit drawer's
// field diff lines up key for key. Exercised on a driver (its own delete
// path) and a location_type (the shared deleteTypeRow path).
func TestRegistryAuditImages(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()

	img := func(raw []byte) map[string]any {
		t.Helper()
		if len(raw) == 0 {
			return nil
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal image: %v (%s)", err, raw)
		}
		return m
	}
	find := func(resource, verb string) storage.AuditEntry {
		t.Helper()
		entries, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: resource, Verb: verb})
		if err != nil {
			t.Fatalf("list audit: %v", err)
		}
		if len(entries) == 0 {
			t.Fatalf("no %s %s audit row", resource, verb)
		}
		return entries[0]
	}

	// Driver: create, update, delete through its own gateway methods.
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "acme-x", DisplayName: "Acme X"}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	newDisplay := "Acme X2"
	if _, err := gw.UpdateDriver(ctx, "", "acme-x", storage.DriverPatch{DisplayName: &newDisplay}); err != nil {
		t.Fatalf("update driver: %v", err)
	}
	up := find("driver", "update")
	oldImg, newImg := img(up.Old), img(up.New)
	if oldImg == nil || oldImg["display_name"] != "Acme X" {
		t.Errorf("driver update old image missing or wrong: %v", oldImg)
	}
	if newImg == nil || newImg["display_name"] != "Acme X2" {
		t.Errorf("driver update new image missing or wrong: %v", newImg)
	}
	if oldImg["name"] != "acme-x" {
		t.Errorf("driver update old image carries no handle: %v", oldImg)
	}

	if err := gw.DeleteDriver(ctx, "", "acme-x"); err != nil {
		t.Fatalf("delete driver: %v", err)
	}
	del := find("driver", "delete")
	delOld := img(del.Old)
	if delOld == nil || delOld["name"] != "acme-x" || delOld["display_name"] != "Acme X2" {
		t.Errorf("driver delete must record the whole row, got %v", delOld)
	}

	// location_type: delete goes through the shared deleteTypeRow helper.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: "annex", DisplayName: "Annex", Icon: "map-pin"}); err != nil {
		t.Fatalf("create location_type: %v", err)
	}
	if err := gw.DeleteLocationType(ctx, "", "annex"); err != nil {
		t.Fatalf("delete location_type: %v", err)
	}
	ltDel := find("location_type", "delete")
	ltOld := img(ltDel.Old)
	if ltOld == nil || ltOld["name"] != "annex" || ltOld["display_name"] != "Annex" {
		t.Errorf("location_type delete must record the whole row, got %v", ltOld)
	}
}
