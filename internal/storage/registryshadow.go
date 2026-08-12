package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrTypeNotForked is a restore-to-defaults on a registry row that carries no
// operator fork. Distinct from ErrTypeNotFound (the row exists) and from
// ErrTypeOfficial (nothing is being refused): there is simply nothing to undo,
// and the console says so rather than reporting a silent success.
var ErrTypeNotForked = errors.New("storage: registry row has no operator fork to restore")

// The registry-fork primitive (#655, ADR-0095).
//
// A shipped registry row is official and the platform owns it. An operator's
// edit does not write that row; it writes a SHADOW keyed on the official row's
// uuid, and every read resolves the shadow over the official row. Restoring
// defaults is deleting the shadow. The official row is never updated and never
// deleted by an operator action, so a release can improve a shipped row
// without stomping anyone.
//
// Three facts hold the shape together, and an adopting registry must keep all
// three:
//
//  1. The shadow shares the official row's uuid, so there is exactly one
//     address per logical row. Foreign keys, the tree walks, the audit trail
//     and the URL all keep naming the row they already named; the namespace is
//     not an addressing concept and never reaches a caller.
//  2. The shadow carries MUTABLE columns only. Structure (the uuid, the name,
//     the official flag, the parent link) is official space: a fork restates a
//     row's facts, it never moves or renames it.
//  3. A fork captures the whole mutable row, not just the edited column, so a
//     later release's change to an unedited column does not reach a forked row.
//     Resolution overlays the keys the image carries, which is what makes a
//     column added after a fork resolve to its official value instead of to
//     nothing.
//
// component_type is the first adopter and location_type the second (#703).
// The other eleven registries carrying `official` adopt later; they need no
// schema of their own, only a mutable-column projection like
// componentTypeShadowImage and a resolve like applyComponentTypeImage.
//
// Fact 2 reads "structure" per registry rather than per column name.
// location_type is FLAT, so it has no parent link to hold out of the image, and
// its allowed_parent_types is not that link: it constrains which types a
// location of this type may sit under, which is a fact the row states about
// itself and one an operator legitimately reshapes. Holding it official would
// leave a fork unable to say the thing the registry exists to say.

// shadowRowID parses a registry row's own uuid, which is the shadow's key. It
// exists because not every adopting registry holds that uuid as a uuid.UUID:
// location_type carries it as the string its wire body and its audit rows use,
// and a MustParse in a gateway path would turn a value the database minted into
// a panic. The failure it names cannot happen through the gateway; a caller
// that fabricated an id gets an error instead of a crash.
func shadowRowID(id string) (uuid.UUID, error) {
	rowID, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("storage: %q is not a registry row uuid: %w", id, err)
	}
	return rowID, nil
}

// loadShadowImage reads a registry row's shadow image, returning nil when the
// row is not forked. nil is the unforked answer everywhere in this file, and
// the overlay treats it as a no-op.
func loadShadowImage(ctx context.Context, q querier, registry string, rowID uuid.UUID) ([]byte, error) {
	var image []byte
	err := q.QueryRow(ctx, `select image from registry_shadow where registry = $1 and row_id = $2`, registry, rowID).Scan(&image)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: load %s shadow %q: %w", registry, rowID, err)
	}
	return image, nil
}

// putShadowImage installs or replaces a registry row's shadow. A second edit
// of an already-forked row replaces the image rather than layering on it,
// which is what keeps the image a whole-row copy.
func putShadowImage(ctx context.Context, tx pgx.Tx, registry string, rowID uuid.UUID, image []byte) error {
	if _, err := tx.Exec(ctx, `
		insert into registry_shadow (registry, row_id, image)
		values ($1, $2, $3::jsonb)
		on conflict (registry, row_id) do update
			set image      = excluded.image,
			    updated_at = now()`, registry, rowID, image); err != nil {
		return fmt.Errorf("storage: write %s shadow %q: %w", registry, rowID, err)
	}
	return nil
}

// dropShadowImage removes a registry row's shadow, reporting whether there was
// one. That report is the restore path's ErrTypeNotForked.
func dropShadowImage(ctx context.Context, tx pgx.Tx, registry string, rowID uuid.UUID) (bool, error) {
	tag, err := tx.Exec(ctx, `delete from registry_shadow where registry = $1 and row_id = $2`, registry, rowID)
	if err != nil {
		return false, fmt.Errorf("storage: drop %s shadow %q: %w", registry, rowID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// shadowFields decodes a shadow image into its present keys. A registry's
// overlay works key by key because "the image does not carry this column" and
// "the image carries this column as null" are different answers: on a registry
// with inherit-from-parent columns (component_type's stem, icon and abbrev)
// null is a value, so collapsing the two would make a forked row silently
// re-adopt an official value the operator cleared.
func shadowFields(image []byte) (map[string]json.RawMessage, error) {
	if len(image) == 0 {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(image, &fields); err != nil {
		return nil, fmt.Errorf("storage: decode shadow image: %w", err)
	}
	return fields, nil
}

// shadowValue reads one key of a shadow image, reporting whether the image
// carries it at all so the caller can leave the official value in place when it
// does not.
//
// It decodes into a FRESH T rather than into the official row's field, which
// matters more than it looks: encoding/json unmarshals into an existing non-nil
// pointer by writing through it, so decoding a shadow's stem straight into the
// official row's *string would rewrite the string the official row itself
// points at. Every read shares that row, so the overlay would leak across
// callers. Decoding fresh and assigning keeps the official values immutable,
// which is the whole premise of the fork.
func shadowValue[T any](fields map[string]json.RawMessage, key string) (T, bool, error) {
	var v T
	raw, ok := fields[key]
	if !ok {
		return v, false, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, false, fmt.Errorf("storage: decode shadow field %q: %w", key, err)
	}
	return v, true, nil
}
