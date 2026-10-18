-- migrate:up

-- What an operator has broken on a simulated device (#889): unplugged (dark
-- on every protocol) or a degraded link. One row per impaired component; a
-- healthy device has none. The simulator beside the component's node reads it
-- with its device list and applies it. The checks mirror impair.Validate.
CREATE TABLE IF NOT EXISTS public.sim_impairment (
    component_id uuid PRIMARY KEY REFERENCES public.component(id) ON DELETE CASCADE,
    unplugged boolean NOT NULL DEFAULT false,
    latency_ms integer NOT NULL DEFAULT 0 CHECK (latency_ms BETWEEN 0 AND 10000),
    jitter_ms integer NOT NULL DEFAULT 0 CHECK (jitter_ms >= 0 AND jitter_ms <= latency_ms),
    loss_pct numeric(5, 2) NOT NULL DEFAULT 0 CHECK (loss_pct BETWEEN 0 AND 100),
    rate_kbit integer NOT NULL DEFAULT 0 CHECK (rate_kbit >= 0),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

-- migrate:down

DROP TABLE IF EXISTS public.sim_impairment;
