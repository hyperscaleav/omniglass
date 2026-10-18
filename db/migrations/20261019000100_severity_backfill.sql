-- migrate:up

-- One-time backfill (#917): the retired three-value severities map onto the
-- ranked levels by meaning. critical, the word that made a component an
-- outage, becomes high, the outage floor of the ranked levels; info and
-- warning keep their names.
UPDATE public.alarm SET severity = 'high' WHERE severity = 'critical';

-- migrate:down

-- Back to the three values the restored CHECK accepts: high and above were
-- outage (critical), below high and above warning degraded (warning).
UPDATE public.alarm SET severity = 'critical'
    WHERE severity IN (SELECT name FROM public.severity_level WHERE rank >= 40) OR severity = 'high';
UPDATE public.alarm SET severity = 'warning'
    WHERE severity NOT IN ('info', 'warning', 'critical');
