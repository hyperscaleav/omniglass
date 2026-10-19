-- migrate:up

-- Severity becomes a registry of named, ranked levels (#917, #819, ADR-0050):
-- an operator relabels, recolors and inserts levels; rules and alarms name a
-- level; the rank compares them. The boot seed lands the official five,
-- spaced (info 10 to disaster 50). The column is "rank" because "order" is a
-- reserved word; it is the design's order. The three-value CHECK on
-- alarm.severity retires here; a following migration maps the old values and
-- the one after references the registry.
CREATE TABLE IF NOT EXISTS public.severity_level (
    id uuid DEFAULT uuidv7() PRIMARY KEY,
    name text NOT NULL CONSTRAINT severity_level_name_key UNIQUE,
    label text,
    color text NOT NULL DEFAULT '',
    rank integer NOT NULL CONSTRAINT severity_level_rank_key UNIQUE CHECK (rank > 0),
    official boolean NOT NULL DEFAULT false,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

ALTER TABLE public.alarm DROP CONSTRAINT IF EXISTS alarm_severity_check;

-- migrate:down

ALTER TABLE public.alarm DROP CONSTRAINT IF EXISTS alarm_severity_check;
ALTER TABLE public.alarm ADD CONSTRAINT alarm_severity_check
    CHECK ((severity = ANY (ARRAY['info'::text, 'warning'::text, 'critical'::text])));
DROP TABLE IF EXISTS public.severity_level;
