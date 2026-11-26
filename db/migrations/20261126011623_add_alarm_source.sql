-- migrate:up
-- An alarm says who landed it when that was not the platform itself (#2727): a
-- throwaway lab's seed lands past alarms, raised and cleared at the time, with
-- source 'devseed', as it labels the health edges it lands (property.source).
-- An alarm the platform raises keeps the empty default.
ALTER TABLE alarm ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT '';

-- migrate:down
ALTER TABLE alarm DROP COLUMN IF EXISTS source;
