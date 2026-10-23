-- migrate:up

-- One-time data backfill (#1018's review): the calc_rule_input backfill
-- copied each earlier {as} name into an alias as it was, but an alias is now
-- held to more rules than that name was (rules.CheckAlias): it may not be a
-- list function's name or one of the calc site's own bindings. A rule holding
-- such an alias is disabled, as one reading a key no longer registered is,
-- and reads back with a calc_error on that alias until it is renamed. The
-- list is every name the calc site reserved when this ran
-- (rules.CalcReservedNames: keywords, Expr builtins, list functions, site
-- bindings). Idempotent.
UPDATE public.calc_rule c SET enabled = false
WHERE c.enabled AND EXISTS (
    SELECT 1 FROM public.calc_rule_input i
    WHERE i.rule_id = c.id AND i.alias = ANY (ARRAY[
        'abs', 'all', 'and', 'any', 'avg', 'bitand', 'bitnand', 'bitnot', 'bitor', 'bitshl',
        'bitshr', 'bitushr', 'bitxor', 'ceil', 'component', 'concat', 'contains', 'count', 'date', 'duration',
        'else', 'endsWith', 'false', 'filter', 'find', 'findIndex', 'findLast', 'findLastIndex', 'first', 'flatten',
        'float', 'floor', 'fromBase64', 'fromJSON', 'fromPairs', 'get', 'groupBy', 'has', 'hasPrefix', 'hasSuffix',
        'if', 'in', 'indexOf', 'int', 'join', 'keys', 'last', 'lastIndexOf', 'len', 'let',
        'lower', 'majority', 'map', 'matches', 'max', 'mean', 'median', 'members', 'min', 'nil',
        'none', 'not', 'now', 'one', 'or', 'reduce', 'repeat', 'replace', 'reverse', 'round',
        'sort', 'sortBy', 'split', 'splitAfter', 'startsWith', 'string', 'sum', 'system', 'take', 'timezone',
        'toBase64', 'toJSON', 'toPairs', 'trim', 'trimPrefix', 'trimSuffix', 'true', 'type', 'uniq', 'upper',
        'value', 'values'
    ]::text[]));

-- migrate:down

-- A data backfill is not reversed: re-enabling a rule is an operator's act.
SELECT 1;
