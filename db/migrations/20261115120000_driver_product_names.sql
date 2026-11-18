-- migrate:up
-- One-time backfill (#1483): two shipped drivers are named for the product
-- they serve, not the family they once shared. `newtron-nvp` becomes
-- `newtron-panel-nvp` and `http-json` becomes `tessellate-census-http`.
--
-- A driver's name is the boot seed's key, and an endpoint holds its driver
-- by uuid. Renaming the row in place keeps that uuid, so every endpoint (and
-- the product) attached through it stays attached, and the next boot's seed
-- updates the row instead of inserting a second one beside it. Where the new
-- name is already taken (a database the new seed has run on) the row is left
-- alone. A fresh database has no old row, so this does nothing there.
UPDATE driver
   SET name = 'newtron-panel-nvp'
 WHERE namespace = 'og' AND name = 'newtron-nvp'
   AND NOT EXISTS (SELECT 1 FROM driver WHERE namespace = 'og' AND name = 'newtron-panel-nvp');

UPDATE driver
   SET name = 'tessellate-census-http'
 WHERE namespace = 'og' AND name = 'http-json'
   AND NOT EXISTS (SELECT 1 FROM driver WHERE namespace = 'og' AND name = 'tessellate-census-http');

-- migrate:down
-- A rename of seeded data has nothing to restore: the previous binary's seed
-- would insert its own rows under the old names.
SELECT 1;
