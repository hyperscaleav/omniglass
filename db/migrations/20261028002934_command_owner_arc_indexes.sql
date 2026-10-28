-- migrate:up
-- #1297: the command table's system and location owner indexes.
--
-- An owner's properties read lists every key a command targeted on it and
-- reads told from the instances its commands named (seriesKeysSQL,
-- latestIntendedSQL). Both filter command by owner_kind and the owner's arc
-- column. The only owner indexes on command lead with component_id
-- (command_component_ts_idx, command_issued_component_idx), so on a system or
-- a location each of those reads was a sequential scan of a table that is
-- never pruned, once per read and once more per declared key: measured at
-- 400,000 component commands, 518 ms for one system's key discovery under a
-- generic plan, 48 ms per declared key's told. These answer both with a
-- probe. TestPropertiesReadReachesAnIndexOnEveryArc explains the statements
-- and fails if either stops reaching an index.
--
-- Partial on the arc column so a component's command (every command issued
-- today) pays nothing for them. Deliberately NOT concurrently, for the reasons
-- 20260813100000_property_system_owner_index.sql gives (dbmate's transaction,
-- and no INVALID index left behind on a failed build).
CREATE INDEX IF NOT EXISTS command_system_type_idx
    ON public.command USING btree (system_id, command_type_id)
    WHERE (system_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS command_location_type_idx
    ON public.command USING btree (location_id, command_type_id)
    WHERE (location_id IS NOT NULL);

-- migrate:down
DROP INDEX IF EXISTS public.command_location_type_idx;
DROP INDEX IF EXISTS public.command_system_type_idx;
