-- migrate:up

-- One-time data backfill (#1100): rows written before the one-writer ruling
-- (ADR-0161) are held to it (ADR-1100). Each row of a reserved key whose
-- writer class would refuse it today is moved, whole, into retired_sample
-- with its reason, so no read serves it and its record survives. The key
-- lists are the reserved keys as of this migration; a reserved key declared
-- later was born under the ruling and has no row from before it.
--
-- 1. A declared pin (ssh-host-key, tls-certificate) is trusted only from the
--    endpoint's audited :retry, whose row carries source 'key-pin' from now
--    on. A legacy :retry pin is recognized by the audit row its transaction
--    wrote (same endpoint, same key, same instant) and stamped; any other
--    declared pin row is retired, with the observed rows of the same key
--    that a node published under it and never before it. Its endpoint then
--    pins the key the node learned before, or trusts on first use again; a
--    retired pin's key is never taken as the pin from the observed series
--    again until a :retry confirms it.
-- 2. A node-probe row is retired when its owner is not a component, when a
--    writer other than the node wrote it (a declared, intended or calculated
--    row, a derived event, or a command's reply, which carries the instant
--    its command executed), when its instance names no endpoint of its
--    component, or when it is dated more than five minutes past the database
--    clock (the cap new rows already obey).
-- 3. A health row the rollup did not write, and a command-issued event no
--    command points at, are retired.
--
-- An event row another row references (a command's caused event, a derived
-- event's source) is left in place. Idempotent: a second run finds nothing.
DO $$
DECLARE
    pinnable text[] := ARRAY['ssh-host-key', 'tls-certificate'];
    node_probe text[] := ARRAY[
        'collection-failed', 'endpoint-authenticated', 'endpoint-reachable', 'endpoint-rejection',
        'endpoint-responsive', 'http-response-time', 'icmp-reachable', 'icmp-rtt-avg', 'session-reason',
        'session-state', 'ssh-handshake-time', 'ssh-host-key', 'tcp-connect-time', 'tcp-open',
        'telnet-response-time', 'tls-certificate', 'tls-certificate-days-left', 'tls-certificate-expiry',
        'tls-handshake-time', 'udp-response-time'];
BEGIN
    -- 1. Pins: stamp the :retry ones, then retire the rest and their echoes.
    UPDATE public.property p SET source = 'key-pin'
    FROM public.property_type t
    WHERE t.id = p.property_type_id AND t.name = ANY (pinnable)
      AND p.provenance = 'declared' AND p.source = ''
      AND EXISTS (
        SELECT 1 FROM public.audit_log a
        WHERE a.resource = 'endpoint' AND a.verb = 'retry' AND a.ts = p.ts
          AND a.new->>'ComponentID' = p.component_id::text AND a.new->>'Name' = p.instance
          AND p.value #>> '{}' IN (a.new->>'host_key', a.new->>'certificate'));

    WITH planted AS (
        SELECT p.id, p.component_id, p.instance, p.property_type_id, p.value, p.ts
        FROM public.property p JOIN public.property_type t ON t.id = p.property_type_id
        WHERE t.name = ANY (pinnable) AND p.provenance = 'declared' AND p.source <> 'key-pin'
    ), echo AS (
        -- Ordered by arrival (the identity id the server assigns), never by
        -- ts: an observation's ts is the node's clock, a declared row's the
        -- database's, and a lagging node would date an echo before the plant.
        SELECT o.id FROM public.property o
        WHERE o.provenance = 'observed'
          AND EXISTS (
            SELECT 1 FROM planted d
            WHERE d.component_id = o.component_id AND d.instance = o.instance
              AND d.property_type_id = o.property_type_id AND d.value = o.value AND o.id > d.id)
          AND NOT EXISTS (
            SELECT 1 FROM public.property h
            WHERE h.provenance = 'observed' AND h.component_id = o.component_id AND h.instance = o.instance
              AND h.property_type_id = o.property_type_id AND h.value = o.value
              AND h.id < (SELECT min(d.id) FROM planted d
                          WHERE d.component_id = o.component_id AND d.instance = o.instance
                            AND d.property_type_id = o.property_type_id AND d.value = o.value))
          AND NOT EXISTS (
            SELECT 1 FROM public.property l
            WHERE l.provenance = 'declared' AND l.source = 'key-pin' AND l.component_id = o.component_id
              AND l.instance = o.instance AND l.property_type_id = o.property_type_id AND l.value = o.value)
    ), gone AS (
        DELETE FROM public.property p
        WHERE p.id IN (SELECT id FROM planted) OR p.id IN (SELECT id FROM echo)
        RETURNING p.*
    )
    INSERT INTO public.retired_sample (lane, sample_id, sample, reason)
    SELECT 'property', g.id, to_jsonb(g),
           CASE WHEN g.provenance = 'declared' THEN 'a declared pin not set through the endpoint''s audited :retry'
                ELSE 'a key observed under a declared pin not set through :retry, and never before it' END
    FROM gone g
    ON CONFLICT (lane, sample_id) DO NOTHING;

    -- 2. Node-probe rows, on every lane. Every lookup is a join, so each
    -- lane is one pass however long its history. A command's reply is the
    -- observation its command's execution wrote: same component, target,
    -- series and instant.
    WITH replies AS (
        SELECT DISTINCT c.component_id, ct.target_property_type_id AS type_id, c.executed_at AS ts,
               coalesce(nullif(c.instance, ''), de.name) AS instance
        FROM public.command c
        JOIN public.command_type ct ON ct.id = c.command_type_id
        LEFT JOIN public.endpoint de ON de.id = c.dispatched_endpoint_id
        WHERE c.executed_at IS NOT NULL AND c.component_id IS NOT NULL AND ct.target_property_type_id IS NOT NULL
    ), judged AS (
        SELECT p.id, CASE
            WHEN p.owner_kind <> 'component' THEN 'a node-probe key on an owner no node writes for'
            WHEN p.provenance <> 'observed'
                 AND NOT (p.provenance = 'declared' AND p.source = 'key-pin' AND t.name = ANY (pinnable))
                THEN 'a node-probe key written by a writer other than the node'
            WHEN rp.component_id IS NOT NULL THEN 'a node-probe key a command''s reply wrote'
            WHEN e.id IS NULL THEN 'a node-probe key under an instance that names no endpoint of its component'
            WHEN p.ts > now() + interval '5 minutes' THEN 'a node-probe key dated past the database clock'
        END AS reason
        FROM public.property p
        JOIN public.property_type t ON t.id = p.property_type_id
        LEFT JOIN public.endpoint e ON e.component = p.component_id AND e.name = p.instance
        LEFT JOIN replies rp ON p.provenance = 'observed' AND rp.component_id = p.component_id
            AND rp.type_id = p.property_type_id AND rp.ts = p.ts AND rp.instance = p.instance
        WHERE t.name = ANY (node_probe)
    ), gone AS (
        DELETE FROM public.property p USING judged j
        WHERE j.id = p.id AND j.reason IS NOT NULL
        RETURNING p.*, j.reason AS retired_reason
    )
    INSERT INTO public.retired_sample (lane, sample_id, sample, reason)
    SELECT 'property', g.id, to_jsonb(g) - 'retired_reason', g.retired_reason FROM gone g
    ON CONFLICT (lane, sample_id) DO NOTHING;

    WITH judged AS (
        SELECT m.id, CASE
            WHEN m.owner_kind <> 'component' THEN 'a node-probe key on an owner no node writes for'
            WHEN m.provenance <> 'observed' THEN 'a node-probe key written by a writer other than the node'
            WHEN e.id IS NULL THEN 'a node-probe key under an instance that names no endpoint of its component'
            WHEN m.ts > now() + interval '5 minutes' THEN 'a node-probe key dated past the database clock'
        END AS reason
        FROM public.metric m
        JOIN public.metric_type t ON t.id = m.metric_type_id
        LEFT JOIN public.endpoint e ON e.component = m.component_id AND e.name = m.instance
        WHERE t.name = ANY (node_probe)
    ), gone AS (
        DELETE FROM public.metric m USING judged j
        WHERE j.id = m.id AND j.reason IS NOT NULL
        RETURNING m.*, j.reason AS retired_reason
    )
    INSERT INTO public.retired_sample (lane, sample_id, sample, reason)
    SELECT 'metric', g.id, to_jsonb(g) - 'retired_reason', g.retired_reason FROM gone g
    ON CONFLICT (lane, sample_id) DO NOTHING;

    -- 2 and 3 on the event lane: node-probe events, and command-issued events
    -- no command points at. A row another row references stays.
    WITH caused AS (
        SELECT DISTINCT caused_event_id AS id FROM public.command WHERE caused_event_id IS NOT NULL
    ), sourced AS (
        SELECT DISTINCT source_event_id AS id FROM public.event WHERE source_event_id IS NOT NULL
    ), judged AS (
        SELECT ev.id, CASE
            WHEN t.name = 'command-issued' THEN
                CASE WHEN cz.id IS NULL THEN 'a command lifecycle event no command issued' END
            WHEN ev.owner_kind <> 'component' THEN 'a node-probe key on an owner no node writes for'
            WHEN ev.provenance <> 'observed' OR ev.origin <> 'caught' OR ev.derived_by_rule_id IS NOT NULL
                 OR ev.source_rule IS NOT NULL
                THEN 'a node-probe key written by a writer other than the node'
            WHEN e.id IS NULL THEN 'a node-probe key under an instance that names no endpoint of its component'
            WHEN ev.ts > now() + interval '5 minutes' THEN 'a node-probe key dated past the database clock'
        END AS reason
        FROM public.event ev
        JOIN public.event_type t ON t.id = ev.event_type_id
        LEFT JOIN caused cz ON cz.id = ev.id
        LEFT JOIN sourced sz ON sz.id = ev.id
        LEFT JOIN public.endpoint e ON e.component = ev.component_id AND e.name = ev.instance
        WHERE (t.name = ANY (node_probe) OR t.name = 'command-issued')
          AND sz.id IS NULL
          AND (t.name = 'command-issued' OR cz.id IS NULL)
    ), gone AS (
        DELETE FROM public.event ev USING judged j
        WHERE j.id = ev.id AND j.reason IS NOT NULL
        RETURNING ev.*, j.reason AS retired_reason
    )
    INSERT INTO public.retired_sample (lane, sample_id, sample, reason)
    SELECT 'event', g.id, to_jsonb(g) - 'retired_reason', g.retired_reason FROM gone g
    ON CONFLICT (lane, sample_id) DO NOTHING;

    -- 3. Health rows the rollup did not write.
    WITH gone AS (
        DELETE FROM public.property p USING public.property_type t
        WHERE t.id = p.property_type_id AND t.name = 'health'
          AND NOT (p.provenance = 'calculated' AND p.source_rule = 'health-rollup')
        RETURNING p.*
    )
    INSERT INTO public.retired_sample (lane, sample_id, sample, reason)
    SELECT 'property', g.id, to_jsonb(g), 'a platform key the health rollup did not write' FROM gone g
    ON CONFLICT (lane, sample_id) DO NOTHING;
END $$;

-- migrate:down

-- The retired rows go back where they were, ids and all; the pin stamps
-- stay (a declared pin's source reads as nothing to the code before them).
INSERT INTO public.property OVERRIDING SYSTEM VALUE
SELECT (jsonb_populate_record(NULL::public.property, r.sample)).*
FROM public.retired_sample r WHERE r.lane = 'property'
ON CONFLICT (id) DO NOTHING;
INSERT INTO public.metric OVERRIDING SYSTEM VALUE
SELECT (jsonb_populate_record(NULL::public.metric, r.sample)).*
FROM public.retired_sample r WHERE r.lane = 'metric'
ON CONFLICT (id) DO NOTHING;
INSERT INTO public.event OVERRIDING SYSTEM VALUE
SELECT (jsonb_populate_record(NULL::public.event, r.sample)).*
FROM public.retired_sample r WHERE r.lane = 'event'
ON CONFLICT (id) DO NOTHING;
DELETE FROM public.retired_sample;
