-- migrate:up

-- A rule's fire event type is required (#948), now that the backfill has
-- given every existing rule one.
ALTER TABLE public.event_rule ALTER COLUMN fire_event_type_id SET NOT NULL;

-- migrate:down

ALTER TABLE public.event_rule ALTER COLUMN fire_event_type_id DROP NOT NULL;
