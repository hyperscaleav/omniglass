-- migrate:up
DROP TABLE IF EXISTS system_role_capability;
DROP TABLE IF EXISTS component_capability;
DROP TABLE IF EXISTS product_capability;
DROP TABLE IF EXISTS alarm_capability;
DROP TABLE IF EXISTS capability;

-- migrate:down
-- irreversible retirement; recreate from init.sql:87,130,160,523,624 if ever needed
