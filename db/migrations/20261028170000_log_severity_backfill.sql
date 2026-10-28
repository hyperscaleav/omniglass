-- migrate:up

-- One-time data backfill (#417): every stored log line's severity onto RFC
-- 5424's eight canonical levels, so a retention window by severity never meets
-- a stray word. Since #1145 every new log_line write goes through
-- logline.Normalize at the gateway, and node_log writes do as of this release;
-- rows written before kept the old push lane's words (warn, err, ERROR) and the
-- node's own. Each table gets the same three steps:
--   - a word the alias table knows (case and surrounding whitespace ignored,
--     every character Go's strings.TrimSpace strips: a stored 'ERROR\r', or
--     'error' and a no-break space, is error) becomes its canonical level;
--   - a word it does not know moves to the severity_raw attribute, with no
--     severity, as Normalize does, so nothing the component said is lost
--     (attributes that are not a JSON object are kept whole beside it);
--   - a blank severity becomes none.
-- The alias list is internal/logline's, held to it by
-- TestLogSeverityBackfillMatchesTheGoTable. Idempotent: a second run finds every
-- severity canonical or absent and changes nothing.
CREATE TEMPORARY TABLE log_severity_alias (word text PRIMARY KEY, level text NOT NULL) ON COMMIT DROP;
INSERT INTO log_severity_alias (word, level) VALUES
    ('0', 'emergency'),
    ('1', 'alert'),
    ('2', 'critical'),
    ('3', 'error'),
    ('4', 'warning'),
    ('5', 'notice'),
    ('6', 'info'),
    ('7', 'debug'),
    ('alert', 'alert'),
    ('crit', 'critical'),
    ('critical', 'critical'),
    ('debug', 'debug'),
    ('emerg', 'emergency'),
    ('emergency', 'emergency'),
    ('err', 'error'),
    ('error', 'error'),
    ('fatal', 'critical'),
    ('info', 'info'),
    ('information', 'info'),
    ('informational', 'info'),
    ('notice', 'notice'),
    ('panic', 'emergency'),
    ('trace', 'debug'),
    ('warn', 'warning'),
    ('warning', 'warning');

UPDATE public.log_line l SET severity = a.level
FROM log_severity_alias a
WHERE lower(btrim(l.severity, E' \t\n\r\f\013\u0085\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000')) = a.word AND l.severity IS DISTINCT FROM a.level;

UPDATE public.log_line SET
    attributes = CASE
        WHEN attributes IS NULL THEN jsonb_build_object('severity_raw', severity)
        WHEN jsonb_typeof(attributes) = 'object' THEN attributes || jsonb_build_object('severity_raw', severity)
        ELSE jsonb_build_object('attributes', attributes, 'severity_raw', severity)
    END,
    severity = NULL
WHERE severity IS NOT NULL AND btrim(severity, E' \t\n\r\f\013\u0085\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000') <> ''
  AND NOT EXISTS (SELECT 1 FROM log_severity_alias a WHERE a.word = lower(btrim(severity, E' \t\n\r\f\013\u0085\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000')));

UPDATE public.log_line SET severity = NULL WHERE severity IS NOT NULL AND btrim(severity, E' \t\n\r\f\013\u0085\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000') = '';

UPDATE public.node_log l SET severity = a.level
FROM log_severity_alias a
WHERE lower(btrim(l.severity, E' \t\n\r\f\013\u0085\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000')) = a.word AND l.severity IS DISTINCT FROM a.level;

UPDATE public.node_log SET
    attributes = CASE
        WHEN attributes IS NULL THEN jsonb_build_object('severity_raw', severity)
        WHEN jsonb_typeof(attributes) = 'object' THEN attributes || jsonb_build_object('severity_raw', severity)
        ELSE jsonb_build_object('attributes', attributes, 'severity_raw', severity)
    END,
    severity = NULL
WHERE severity IS NOT NULL AND btrim(severity, E' \t\n\r\f\013\u0085\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000') <> ''
  AND NOT EXISTS (SELECT 1 FROM log_severity_alias a WHERE a.word = lower(btrim(severity, E' \t\n\r\f\013\u0085\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000')));

UPDATE public.node_log SET severity = NULL WHERE severity IS NOT NULL AND btrim(severity, E' \t\n\r\f\013\u0085\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000') = '';

-- migrate:down

-- Not reversed. A mapped word was another spelling of the same level, and a
-- word outside the table is still on the row, as severity_raw.
