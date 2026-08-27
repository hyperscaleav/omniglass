-- migrate:up
-- The standard's map declaration (#791, ADR-0128): one jsonb value holding the
-- normalized room layout ({aspect, positions:[{role, position, x, y}]}), null
-- when the standard declares none. Display data validated in Go (bounds,
-- duplicate positions); nothing queries into it, which is why it is one value
-- and not a table.
ALTER TABLE standard ADD COLUMN IF NOT EXISTS map jsonb;

-- migrate:down
ALTER TABLE standard DROP COLUMN IF EXISTS map;
