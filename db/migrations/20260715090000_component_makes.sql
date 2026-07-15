-- migrate:up
CREATE TABLE IF NOT EXISTS component_make (
    id            text PRIMARY KEY,
    display_name  text NOT NULL,
    icon          text NOT NULL DEFAULT '',
    support_phone text NOT NULL DEFAULT '',
    website       text NOT NULL DEFAULT '',
    official      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- migrate:down
DROP TABLE IF EXISTS component_make;
