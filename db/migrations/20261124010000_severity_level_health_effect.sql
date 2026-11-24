-- migrate:up

-- A severity level declares its own effect on its component's health (#2558):
-- none (an informational alarm that does not badge the component), degraded, or
-- outage (the component stops filling its slots). Until now the effect was
-- implied by rank against a hardcoded floor. Existing rows keep that behavior
-- (outage at rank 40 and above, degraded below); the boot seed then declares the
-- official levels, which moves exactly one: info becomes none.
ALTER TABLE public.severity_level
    ADD COLUMN IF NOT EXISTS health_effect text NOT NULL DEFAULT 'degraded'
    CONSTRAINT severity_level_health_effect_check CHECK (health_effect IN ('none', 'degraded', 'outage'));

UPDATE public.severity_level SET health_effect = 'outage' WHERE rank >= 40 AND health_effect = 'degraded';

ALTER TABLE public.severity_level ALTER COLUMN health_effect DROP DEFAULT;

-- migrate:down

ALTER TABLE public.severity_level DROP COLUMN IF EXISTS health_effect;
