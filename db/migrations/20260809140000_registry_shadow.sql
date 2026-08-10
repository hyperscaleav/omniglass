-- migrate:up

-- registry_shadow is the operator's private version of a shipped registry row
-- (#655, ADR-0095). A shipped row seeds official=true and the platform owns it;
-- an operator edit does not write that row, it writes a shadow here, and reads
-- resolve the shadow over the official row. "Restore defaults" is deleting the
-- shadow, so a release can improve a shipped row without stomping anyone.
--
-- Keyed on (registry, row_id), where row_id is the OFFICIAL row's uuid. That
-- keeps one uuid per logical row: `component_type_name_key` stays a global
-- unique, every inbound foreign key (product.component_type_id,
-- role_component_type.component_type_id, component_type.parent_id, all ON
-- DELETE RESTRICT) keeps pointing at a row that exists, and every existing
-- lookup through registryRefCol still matches exactly one row. A fork changes
-- what a read RESOLVES to, never what anything ADDRESSES, so the namespace
-- cannot leak into a URL.
--
-- One generic table rather than a shadow table per registry: twelve more
-- registries carry `official` and adopt this later, and the per-type rule
-- columns of #657 add columns to them. A typed shadow table would double the
-- DDL cost of every future column on every adopter; this costs none. The
-- polymorphic (registry, row_id) reference is the shape audit_log already uses
-- for (resource, resource_id).
--
-- `image` is the operator's whole-row copy of the target's MUTABLE columns
-- only, taken at fork time (ADR-0095 decision 2). Resolution overlays the keys
-- the image carries, so a column added to a registry after a fork resolves to
-- the official value rather than to nothing. Structural columns (the uuid, the
-- name, the official flag, the parent link) are never captured: a shadow
-- restates a row's facts, it never moves or renames it.
CREATE TABLE IF NOT EXISTS registry_shadow (
    registry text NOT NULL,
    row_id uuid NOT NULL,
    image jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT registry_shadow_pkey PRIMARY KEY (registry, row_id)
);

-- migrate:down
DROP TABLE IF EXISTS registry_shadow;
