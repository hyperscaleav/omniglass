-- migrate:up
-- #417: the log tables' retention indexes. The retention pass prunes a log
-- line past its severity band's window, a band's lines at a time
-- (logline.RetentionBands): by severity and time for a line that has one, and by
-- time alone for a line with no severity, which is kept with info. Each prune
-- searches exactly the rows it may delete through one of these, never the table.
-- TestPruneLogsReachesItsIndex explains the statements and fails if one stops
-- reaching its index.
--
-- event.source_log_line_id gets the index its foreign key's on delete set null
-- needs: the action runs once per deleted line, as an update of event by that
-- column, and without an index each one scanned the whole event table, which is
-- never pruned (measured on PG18: 27.7s for a 2,000-line chunk over 200k events,
-- 8.8ms with it). Partial on a link that exists, since most events have none.
--
-- On log_line the (severity, ts) index replaces the severity-only one it covers:
-- a read filtering by severity (severity = any(...)) still leads with severity.
-- node_log had no severity index. The (ts) indexes are partial on a missing
-- severity, so a rated line pays nothing for them. Deliberately NOT
-- concurrently, for the reasons 20260813100000_property_system_owner_index.sql
-- gives (dbmate's transaction, and no INVALID index left behind on a failed
-- build).
CREATE INDEX IF NOT EXISTS event_source_log_line_idx
    ON public.event USING btree (source_log_line_id) WHERE (source_log_line_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS log_line_severity_ts_idx
    ON public.log_line USING btree (severity, ts) WHERE (severity IS NOT NULL);
DROP INDEX IF EXISTS public.log_line_severity_idx;
CREATE INDEX IF NOT EXISTS log_line_unrated_ts_idx
    ON public.log_line USING btree (ts) WHERE (severity IS NULL);
CREATE INDEX IF NOT EXISTS node_log_severity_ts_idx
    ON public.node_log USING btree (severity, ts) WHERE (severity IS NOT NULL);
CREATE INDEX IF NOT EXISTS node_log_unrated_ts_idx
    ON public.node_log USING btree (ts) WHERE (severity IS NULL);

-- migrate:down
DROP INDEX IF EXISTS public.event_source_log_line_idx;
DROP INDEX IF EXISTS public.node_log_unrated_ts_idx;
DROP INDEX IF EXISTS public.node_log_severity_ts_idx;
DROP INDEX IF EXISTS public.log_line_unrated_ts_idx;
CREATE INDEX IF NOT EXISTS log_line_severity_idx
    ON public.log_line USING btree (severity) WHERE (severity IS NOT NULL);
DROP INDEX IF EXISTS public.log_line_severity_ts_idx;
