-- migrate:up

-- Retire audit_id from the observation tables. It was a placeholder for "the
-- audit_log row that declared this value", but it was never wired (no code wrote
-- it), typed bigint against a uuid audit_log.id so it could never carry the FK it
-- implied, and it exists only to serve a 'declared' datapoint provenance the
-- design does not use: declared VALUES are config (property_value / variables),
-- not observations (see the glossary). source_rule_version and value_json are
-- kept: they are designed-but-unbuilt on-row lineage (the backtest version hinge
-- and ADR-0038's optional structured state value), not dead columns.

-- The lineage CHECK named audit_id in every branch, so rebuild it without that
-- column. observed / calculated / intended keep their exact shape; 'declared'
-- stays a valid enum value but, its pointer gone, simply requires the other
-- lineage columns null.
alter table metric_datapoint drop constraint metric_datapoint_lineage_check;
alter table metric_datapoint drop column audit_id;
alter table metric_datapoint add constraint metric_datapoint_lineage_check check (
       (provenance = 'observed'   and event_id is null)
    or (provenance = 'calculated' and source_rule is not null and event_id is null)
    or (provenance = 'intended'   and event_id is not null and source_rule is null)
    or (provenance = 'declared'   and source_rule is null and event_id is null)
);

alter table state_datapoint drop constraint state_datapoint_lineage_check;
alter table state_datapoint drop column audit_id;
alter table state_datapoint add constraint state_datapoint_lineage_check check (
       (provenance = 'observed'   and event_id is null)
    or (provenance = 'calculated' and source_rule is not null and event_id is null)
    or (provenance = 'intended'   and event_id is not null and source_rule is null)
    or (provenance = 'declared'   and source_rule is null and event_id is null)
);

-- migrate:down
alter table state_datapoint drop constraint state_datapoint_lineage_check;
alter table state_datapoint add column audit_id bigint;
alter table state_datapoint add constraint state_datapoint_lineage_check check (
       (provenance = 'observed'   and event_id is null and audit_id is null)
    or (provenance = 'calculated' and source_rule is not null and event_id is null and audit_id is null)
    or (provenance = 'intended'   and event_id is not null and source_rule is null and audit_id is null)
    or (provenance = 'declared'   and audit_id is not null and source_rule is null and event_id is null)
);

alter table metric_datapoint drop constraint metric_datapoint_lineage_check;
alter table metric_datapoint add column audit_id bigint;
alter table metric_datapoint add constraint metric_datapoint_lineage_check check (
       (provenance = 'observed'   and event_id is null and audit_id is null)
    or (provenance = 'calculated' and source_rule is not null and event_id is null and audit_id is null)
    or (provenance = 'intended'   and event_id is not null and source_rule is null and audit_id is null)
    or (provenance = 'declared'   and audit_id is not null and source_rule is null and event_id is null)
);
