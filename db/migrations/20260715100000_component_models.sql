-- migrate:up

-- component_model: a make + model product (e.g. Crestron DM-NVX-363). Unlike
-- component_make it is not a flat registry alone: make_id is a required FK, so
-- a model always belongs to a make, and the make-in-use delete guard (in
-- component_makes.go) refuses to delete a make while a model still points at
-- it. front_image_id/back_image_id are optional pointers at the files
-- primitive (file.id is uuid); lifecycle timestamps are all optional, set only
-- once known. No type/classification field here (deferred). Additive and
-- idempotent.
CREATE TABLE IF NOT EXISTS component_model (
    id             text PRIMARY KEY,
    display_name   text NOT NULL,
    make_id        text NOT NULL REFERENCES component_make(id),
    model_number   text NOT NULL DEFAULT '',
    family         text NOT NULL DEFAULT '',
    released_at    timestamptz,
    eos_at         timestamptz,
    eol_at         timestamptz,
    front_image_id uuid REFERENCES file(id),
    back_image_id  uuid REFERENCES file(id),
    official       boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS component_model_make_id_idx ON component_model(make_id);

-- migrate:down
DROP TABLE IF EXISTS component_model;
