-- migrate:up
-- #462: platform_setting has zero readers and zero writers (the settings
-- engine stores everything in setting_override) and is a decoy for the next
-- platform-wide setting implementation. Dropped; the settings engine's
-- setting_override is the one home.
DROP TABLE IF EXISTS platform_setting;

-- migrate:down
CREATE TABLE IF NOT EXISTS platform_setting (
    key text NOT NULL,
    value jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT platform_setting_pkey PRIMARY KEY (key)
);
