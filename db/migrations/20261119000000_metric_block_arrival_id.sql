-- migrate:up
alter table metric_block add column if not exists arrival_id bigint[];

-- migrate:down
alter table metric_block drop column if exists arrival_id;
