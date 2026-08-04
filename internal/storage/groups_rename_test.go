package storage_test

import (
	"context"
	"errors"
	"testing"

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
			name := bad
			_, err := gw.UpdateGroup(ctx, "", g.ID, storage.GroupPatch{Name: &name}, all)
			if !errors.Is(err, storage.ErrInvalidEntityKey) && !errors.Is(err, storage.ErrEntityKeyIsUUID) {
				t.Fatalf("rename to %q = %v, want a name refusal so the API maps it to 422", bad, err)
			}
		})
	}

	// The legal rename still works, so the refusals above are not passing because the
	// rename leg is simply broken.
	good := "field-engineers"
	renamed, err := gw.UpdateGroup(ctx, "", g.ID, storage.GroupPatch{Name: &good}, all)
	if err != nil {
		t.Fatalf("legal rename: %v", err)
	}
	if renamed.Name != good {
		t.Fatalf("name after rename = %q, want %q", renamed.Name, good)
	}
}
