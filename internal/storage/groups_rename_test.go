package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The group rename leg, which shipped unproved.
//
// Every sibling renameable entity has one of these (components_rename_test.go,
// locations_rename_test.go, systems_rename_test.go) and principal_group did not,
// because until now the only gate on a group name was the Huma pattern on the
// request body. The gateway accepted anything at all, including a uuid, for any
// caller that was not the HTTP route.
//
// The create leg is proved by the sweep in entity_key_validation_test.go. This is
// the other half: renaming is a second way to set a name, and a validator on create
// alone is a validator a rename walks around.
func TestGroupRenameObeysTheNameRule(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	g, err := gw.CreateGroup(ctx, "", storage.GroupSpec{Name: "field-techs"}, all)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	for why, bad := range map[string]string{
		"whitespace":                 "Bad Name",
		"an underscore":              "bad_name",
		"a dot":                      "bad.name",
		"the accessor sigil":         "bad$name",
		"uppercase":                  "BadName",
		"shaped exactly like a uuid": "019f8754-461f-7b82-b5f2-fc4bbe1c3765",
	} {
		t.Run(why, func(t *testing.T) {
			_, err := gw.RenameGroup(ctx, "", g.ID, bad, all)
			if !errors.Is(err, storage.ErrInvalidEntityName) && !errors.Is(err, storage.ErrEntityNameIsUUID) {
				t.Fatalf("rename to %q = %v, want a name refusal so the API maps it to 422", bad, err)
			}
		})
	}

	// The legal rename still works, so the refusals above are not passing because the
	// rename leg is simply broken.
	good := "field-engineers"
	renamed, err := gw.RenameGroup(ctx, "", g.ID, good, all)
	if err != nil {
		t.Fatalf("legal rename: %v", err)
	}
	if renamed.Name != good {
		t.Fatalf("name after rename = %q, want %q", renamed.Name, good)
	}
	if renamed.ID != g.ID {
		t.Fatalf("rename changed id: got %q, want %q (a rename moves the name, never the identity)", renamed.ID, g.ID)
	}

	// A group is addressed by id, so "reachable by the new name" is read off the
	// row rather than a lookup: the uuid still resolves and reports the new name.
	got, err := gw.GetGroup(ctx, g.ID, all)
	if err != nil || got.Name != good {
		t.Fatalf("get by uuid = %v, %v; want the row under its new name", got, err)
	}

	// Renaming onto a taken name -> ErrGroupExists (the API's 409).
	other, err := gw.CreateGroup(ctx, "", storage.GroupSpec{Name: "night-shift"}, all)
	if err != nil {
		t.Fatalf("create second group: %v", err)
	}
	if _, err := gw.RenameGroup(ctx, "", other.ID, good, all); !errors.Is(err, storage.ErrGroupExists) {
		t.Fatalf("dup rename err = %v, want ErrGroupExists", err)
	}

	// An unknown id is the ordinary not-found, so the API answers 404.
	if _, err := gw.RenameGroup(ctx, "", "00000000-0000-0000-0000-000000000000", "ghosts", all); !errors.Is(err, storage.ErrGroupNotFound) {
		t.Fatalf("rename of a missing group = %v, want ErrGroupNotFound", err)
	}

	// Group management is all-scope admin work, exactly as UpdateGroup is: a
	// narrower scope is refused before anything is read.
	if _, err := gw.RenameGroup(ctx, "", g.ID, "narrow-scope", scope.Set{}); !errors.Is(err, storage.ErrPrincipalForbidden) {
		t.Fatalf("rename without an all scope = %v, want ErrPrincipalForbidden", err)
	}

	assertOneRenameAudit(t, ctx, gw, "principal_group", g.ID)
}
