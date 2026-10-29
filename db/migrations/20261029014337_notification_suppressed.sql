-- migrate:up

-- A suppressed notification (#1318, ADR-1318): a row written but never sent,
-- with the reason in last_error, because its alarm's condition is snoozed. It
-- is terminal: the tick never drains it (the due index is queued rows only),
-- the inbox never shows it (it reads sent rows), and :retry refuses it.
ALTER TABLE public.notification DROP CONSTRAINT IF EXISTS notification_status_check;
ALTER TABLE public.notification ADD CONSTRAINT notification_status_check
    CHECK (status IN ('queued', 'sent', 'failed', 'suppressed'));

-- migrate:down

ALTER TABLE public.notification DROP CONSTRAINT IF EXISTS notification_status_check;
ALTER TABLE public.notification ADD CONSTRAINT notification_status_check
    CHECK (status IN ('queued', 'sent', 'failed'));
