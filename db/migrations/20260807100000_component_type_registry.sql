-- migrate:up

-- component_type is a hierarchical taxonomy above product (partially reverses
-- ADR-0047's retirement of the flat component_type registry, recorded as
-- such): a product is now classified BY a component_type (mic, camera,
-- wireless-mic under mic), not the other way around. stem/icon/abbrev are
-- nullable and inherited: a subtype leaves them null to fall back to its
-- parent's, resolved by walking parent_id in Go (no DB logic). ON DELETE
-- RESTRICT on parent_id, the lesson from #507: a registry delete must never
-- cascade through classified rows.
CREATE TABLE component_type (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    stem text,
    icon text,
    abbrev text,
    default_tags text[] NOT NULL DEFAULT '{}',
    official boolean NOT NULL DEFAULT false,
    parent_id uuid REFERENCES component_type(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT component_type_name_key UNIQUE (name)
);

-- migrate:down
DROP TABLE component_type;
