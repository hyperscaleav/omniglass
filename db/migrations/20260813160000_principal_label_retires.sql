-- migrate:up

-- #564: principal_label(uuid) retires.
--
-- It was a stored SQL function, `coalesce(human.username, service.label)`, and
-- it was wrong on both counts. It put the platform's answer to "what names this
-- principal" in the database, which is the one place this repository says logic
-- never lives, and it called the answer a LABEL when both branches return an
-- identifier: a username, or a service account's own identifying column.
--
-- The answer now lives in internal/storage/principal_ident.go and is rendered
-- from there into the statements the gateway binds, so dropping the object is
-- the schema half of the same change.
--
-- No CASCADE, deliberately. A dependent object (a view, a default, a generated
-- column) would make this fail loudly rather than be dropped silently along
-- with it; there is none today, and if one ever appears the failure is the
-- correct outcome.
DROP FUNCTION IF EXISTS principal_label(uuid);

-- migrate:down

CREATE FUNCTION principal_label(pid uuid) RETURNS text
    LANGUAGE sql STABLE
    AS $$
    select coalesce(
        (select username from human where principal_id = pid),
        (select label from service where principal_id = pid)
    );
$$;
