-- migrate:up

-- The metric contract tables mirror their property siblings column for column,
-- but the split migration (#587) created them without the metric_type_id index
-- the property side has carried since init (product_property_property_type_idx
-- and kin). Same shape, same naming pattern, post-rename form: btree on the
-- catalog FK, so a catalog-side lookup (which contracts declare this metric?)
-- and the ON DELETE CASCADE from metric_type stop walking the whole table.

create index if not exists product_metric_metric_type_idx
    on product_metric using btree (metric_type_id);
create index if not exists standard_metric_metric_type_idx
    on standard_metric using btree (metric_type_id);
create index if not exists location_type_metric_metric_type_idx
    on location_type_metric using btree (metric_type_id);

-- migrate:down

drop index if exists product_metric_metric_type_idx;
drop index if exists standard_metric_metric_type_idx;
drop index if exists location_type_metric_metric_type_idx;
