-- migrate:up

-- The component classification catalogs (estate-model shift, PR2). vendor is the
-- component_make catalog renamed and given a kind; driver and capability are new
-- leaf catalogs. product (which references all three) lands in the next slice.

-- vendor: component_make renamed, plus a kind (what role the vendor plays).
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'component_make')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'vendor') THEN
    ALTER TABLE component_make RENAME TO vendor;
  END IF;
END $$;
ALTER TABLE vendor ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'manufacturer';
ALTER TABLE vendor DROP CONSTRAINT IF EXISTS vendor_kind_check;
ALTER TABLE vendor ADD CONSTRAINT vendor_kind_check
  CHECK (kind IN ('manufacturer', 'integrator', 'developer'));

-- driver: the implementation that gets/emits/sets a product's signals (versioned).
create table if not exists driver (
    id           text primary key,
    display_name text not null,
    version      text not null default '',
    official     boolean not null default false,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now()
);

-- capability: a flat vocabulary of what a component can do (microphone, display).
create table if not exists capability (
    id           text primary key,
    display_name text not null,
    official     boolean not null default false,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now()
);

-- migrate:down

drop table if exists capability;
drop table if exists driver;
ALTER TABLE vendor DROP CONSTRAINT IF EXISTS vendor_kind_check;
ALTER TABLE vendor DROP COLUMN IF EXISTS kind;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'vendor')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'component_make') THEN
    ALTER TABLE vendor RENAME TO component_make;
  END IF;
END $$;
