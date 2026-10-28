-- migrate:up

-- The boot seed no longer plants standards (ADR-0827): an operator forks the
-- ones they want from the template catalog. The seeded mark existed only so a
-- rename could refuse a row the next boot would plant again by name; nothing
-- plants one now, so a standard a fleet already holds from the old seed is
-- an ordinary operator row, renameable and deletable, and the mark goes.
ALTER TABLE standard DROP COLUMN IF EXISTS seeded;

-- migrate:down

ALTER TABLE standard ADD COLUMN IF NOT EXISTS seeded boolean NOT NULL DEFAULT false;
