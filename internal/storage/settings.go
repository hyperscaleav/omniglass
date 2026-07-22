package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/jackc/pgx/v5"
)

// SettingOverride is one override row: the values and locked key-paths an operator
// set at a cascade level for a namespace. Slice-0 uses scope "platform" only. The
// "default" level is the type's own declaration, off the cascade axis, and is never
// a row here.
type SettingOverride struct {
	Scope     string
	Namespace string
	Doc       map[string]any
	Locks     []string
}

// GetSettingOverrides returns every platform override row at a scope. Unscoped:
// platform settings describe the platform, not the estate, so no ABAC scope
// applies; the route gates on settings:read.
func (p *PG) GetSettingOverrides(ctx context.Context, scope string) ([]SettingOverride, error) {
	rows, err := p.pool.Query(ctx, `
		select namespace, doc, locks
		from setting_override
		where scope = $1 and principal_id is null
		order by namespace`, scope)
	if err != nil {
		return nil, fmt.Errorf("storage: list setting overrides: %w", err)
	}
	defer rows.Close()
	var out []SettingOverride
	for rows.Next() {
		o := SettingOverride{Scope: scope}
		var docRaw, locksRaw []byte
		if err := rows.Scan(&o.Namespace, &docRaw, &locksRaw); err != nil {
			return nil, fmt.Errorf("storage: scan setting override: %w", err)
		}
		if err := json.Unmarshal(docRaw, &o.Doc); err != nil {
			return nil, fmt.Errorf("storage: unmarshal override doc: %w", err)
		}
		if err := json.Unmarshal(locksRaw, &o.Locks); err != nil {
			return nil, fmt.Errorf("storage: unmarshal override locks: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// UpsertSettingOverride inserts or replaces the (scope, namespace) platform row and
// audits it. principal_id is NULL (platform-wide, no principal). The ON CONFLICT
// target is the identity constraint, which treats the NULL principal as one value.
func (p *PG) UpsertSettingOverride(ctx context.Context, actorID, scope, namespace string, doc map[string]any, locks []string) (*SettingOverride, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin upsert setting override: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	o, err := upsertOverrideTx(ctx, tx, actorID, scope, namespace, doc, locks)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit upsert setting override: %w", err)
	}
	return o, nil
}

// MergePatchSettingOverride applies an RFC 7386 JSON Merge Patch to the (scope,
// namespace) platform override as a single atomic read-modify-write. A
// transaction-scoped advisory lock keyed on (scope, namespace) serializes concurrent
// patches to the same namespace so no update is lost. A plain SELECT FOR UPDATE would
// not suffice: on the first patch the row does not exist yet, so there is nothing to
// lock, and racing inserts would clobber each other through ON CONFLICT. The advisory
// lock covers the not-yet-existent row too. It releases at commit or rollback.
func (p *PG) MergePatchSettingOverride(ctx context.Context, actorID, scope, namespace string, patch map[string]any) (*SettingOverride, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin merge-patch setting override: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`select pg_advisory_xact_lock(hashtext($1), hashtext($2))`, scope, namespace); err != nil {
		return nil, fmt.Errorf("storage: lock setting override: %w", err)
	}

	var docRaw []byte
	err = tx.QueryRow(ctx,
		`select doc from setting_override where scope = $1 and principal_id is null and namespace = $2`,
		scope, namespace).Scan(&docRaw)
	current := map[string]any{}
	switch {
	case err == nil:
		if err := json.Unmarshal(docRaw, &current); err != nil {
			return nil, fmt.Errorf("storage: unmarshal override doc: %w", err)
		}
	case errors.Is(err, pgx.ErrNoRows):
		// no override yet: patch merges onto an empty doc
	default:
		return nil, fmt.Errorf("storage: read setting override: %w", err)
	}

	merged := settings.ApplyMergePatch(current, patch)
	o, err := upsertOverrideTx(ctx, tx, actorID, scope, namespace, merged, nil)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit merge-patch setting override: %w", err)
	}
	return o, nil
}

// upsertOverrideTx inserts or replaces the (scope, namespace) platform row and audits
// it, inside the caller's transaction. principal_id is NULL (platform-wide, no
// principal); the ON CONFLICT target is the identity constraint, which treats the
// NULL principal as one value.
func upsertOverrideTx(ctx context.Context, tx pgx.Tx, actorID, scope, namespace string, doc map[string]any, locks []string) (*SettingOverride, error) {
	if doc == nil {
		doc = map[string]any{}
	}
	if locks == nil {
		locks = []string{}
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("storage: marshal override doc: %w", err)
	}
	locksJSON, err := json.Marshal(locks)
	if err != nil {
		return nil, fmt.Errorf("storage: marshal override locks: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into setting_override (scope, principal_id, namespace, doc, locks, updated_by)
		values ($1, null, $2, $3, $4, $5)
		on conflict on constraint setting_override_identity
		do update set doc = excluded.doc, locks = excluded.locks,
		              updated_at = now(), updated_by = excluded.updated_by`,
		scope, namespace, docJSON, locksJSON, nullize(actorID)); err != nil {
		return nil, fmt.Errorf("storage: upsert setting override: %w", err)
	}
	o := SettingOverride{Scope: scope, Namespace: namespace, Doc: doc, Locks: locks}
	if err := writeAuditRes(ctx, tx, actorID, "update", "settings", namespace, nil, o); err != nil {
		return nil, err
	}
	return &o, nil
}

// DeleteSettingOverride drops one namespace's platform row (restore to defaults) and
// audits it. A missing row is not an error: restore is idempotent.
func (p *PG) DeleteSettingOverride(ctx context.Context, actorID, scope, namespace string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete setting override: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`delete from setting_override where scope = $1 and principal_id is null and namespace = $2`,
		scope, namespace); err != nil {
		return fmt.Errorf("storage: delete setting override: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "settings", namespace, nil, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteAllSettingOverrides removes every platform override (a factory reset) and
// audits it once with an empty resource id.
func (p *PG) DeleteAllSettingOverrides(ctx context.Context, actorID, scope string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin reset setting overrides: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`delete from setting_override where scope = $1 and principal_id is null`, scope); err != nil {
		return fmt.Errorf("storage: reset setting overrides: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "settings", "", nil, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
