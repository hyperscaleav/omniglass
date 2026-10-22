-- migrate:up

-- One-time data backfill (#1018), for databases that ran the calc rule
-- branch before it merged: each stored {as, key} input becomes a
-- calc_rule_input row holding its key by id (a property of that name, else a
-- metric, the order the gateway resolved names in), and each named reducer
-- {reducer} becomes the expression that computes the same value over the
-- input's alias (average avg(x), min min(x), max max(x), sum sum(x), count
-- len(x), majority majority(x), any any(x), all all(x)), proven equal in
-- internal/rules. A rule reading a key no longer registered keeps its other
-- inputs and is disabled: its expression names an alias nothing binds, which
-- its calc_error shows. Idempotent: a rule's existing rows are kept, and only
-- a {reducer} reduce is rewritten.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'public' AND table_name = 'calc_rule' AND column_name = 'inputs') THEN
        INSERT INTO public.calc_rule_input (rule_id, position, alias, lane, property_type_id, metric_type_id)
        SELECT c.id, (e.ord - 1)::integer, e.x->>'as',
               CASE WHEN pt.id IS NOT NULL THEN 'property' ELSE 'metric' END,
               pt.id, CASE WHEN pt.id IS NULL THEN mt.id END
        FROM public.calc_rule c
        CROSS JOIN LATERAL jsonb_array_elements(c.inputs) WITH ORDINALITY AS e(x, ord)
        LEFT JOIN public.property_type pt ON pt.name = e.x->>'key'
        LEFT JOIN public.metric_type mt ON mt.name = e.x->>'key'
        WHERE (pt.id IS NOT NULL OR mt.id IS NOT NULL)
          AND NOT EXISTS (SELECT 1 FROM public.calc_rule_input i WHERE i.rule_id = c.id)
        ON CONFLICT DO NOTHING;

        UPDATE public.calc_rule c SET enabled = false
        WHERE c.enabled AND EXISTS (
            SELECT 1 FROM jsonb_array_elements(c.inputs) x
            WHERE NOT EXISTS (SELECT 1 FROM public.property_type pt WHERE pt.name = x->>'key')
              AND NOT EXISTS (SELECT 1 FROM public.metric_type mt WHERE mt.name = x->>'key'));
    END IF;

    UPDATE public.calc_rule c SET reduce = jsonb_build_object('expr',
        CASE c.reduce->>'reducer'
            WHEN 'average' THEN 'avg'
            WHEN 'count' THEN 'len'
            ELSE c.reduce->>'reducer'
        END || '(' || coalesce(
            (SELECT i.alias FROM public.calc_rule_input i WHERE i.rule_id = c.id ORDER BY i.position LIMIT 1), 'x') || ')')
    WHERE c.reduce ? 'reducer';
END $$;

-- migrate:down

-- A data backfill is not reversed: the reduce it wrote is an expression the
-- old shape also reads, and the input rows go with their table's down.
SELECT 1;
