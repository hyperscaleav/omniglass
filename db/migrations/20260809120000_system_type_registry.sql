-- migrate:up

-- system_type is the coarse taxonomy of what KIND OF SPACE a system is (a
-- boardroom, a classroom, a video wall), exactly parallel to the way
-- component_type classifies a product: a nested registry with a universal
-- shipped seed, sitting beside `standard` rather than inside it. standard is
-- already nested (parent_standard_id) and that inheritance was considered for
-- this job and rejected: a standard fork expresses two ways to BUILD the same
-- kind of room, while this expresses what kind of room it is, and one estate
-- can hold ten signage standards and six classroom standards under a single
-- coarse type. standard stays the blueprint that declares required roles.
--
-- The identifier is deliberately reused: `system_type` was the old column name
-- for what is now system.standard_id (ADR-0048). A TABLE by that name and a
-- system.system_type_id column pointing at it are different objects from the
-- retired column, and the reuse is recorded in ADR-0096.
--
-- stem/icon/abbrev are nullable and INHERITED: a subtype leaves one null to
-- fall back to its nearest ancestor that sets it, resolved by walking parent_id
-- in Go (no DB logic). ON DELETE RESTRICT on parent_id, the lesson from #507: a
-- registry delete must never cascade through classified rows.
CREATE TABLE IF NOT EXISTS system_type (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    stem text,
    icon text,
    abbrev text,
    official boolean NOT NULL DEFAULT false,
    parent_id uuid REFERENCES system_type(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT system_type_name_key UNIQUE (name)
);

-- Nullable for now. A floor (the way #647 gave product a component_type floor)
-- waits until the shipped tree has proven out; it is deliberately not this
-- slice. ON DELETE RESTRICT for the same #507 reason as parent_id above: a
-- classifier that still classifies a system cannot be deleted out from under
-- it.
ALTER TABLE system ADD COLUMN IF NOT EXISTS system_type_id uuid REFERENCES system_type(id) ON DELETE RESTRICT;

-- migrate:down
ALTER TABLE system DROP COLUMN IF EXISTS system_type_id;
DROP TABLE IF EXISTS system_type;
