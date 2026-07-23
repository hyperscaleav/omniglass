-- migrate:up

-- The observation tables are bare-noun entities like their record-lane sibling
-- event: the noun IS the entity, and classification takes a _type / _kind suffix
-- (datapoint_type already became property). Drop the redundant _datapoint genus.
-- The table name now equals the property.kind value it stores (kind='metric' ->
-- metric), and a future UNION over the kind-tables can take the freed name
-- 'datapoint'. Dependent object names (pkey, sequence, indexes, FK and CHECK
-- constraints) keep their old _datapoint identifiers for now, exactly as the
-- #262 renames left vendor (component_make_*) and standard (system_type_*); the
-- migration collapse cleans every stale identifier in one pass.
alter table metric_datapoint rename to metric;
alter table state_datapoint rename to state;

-- migrate:down
alter table state rename to state_datapoint;
alter table metric rename to metric_datapoint;
