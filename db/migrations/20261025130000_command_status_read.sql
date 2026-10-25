-- migrate:up

-- A command's status is readable from issue to settlement (#1155). The row
-- already records the wire (dispatched_at, executed_at, exec_error) and the
-- verdict (status, settled_at); what it did not keep is the evidence and the
-- why, so a read could say THAT a command failed but not what the component
-- said or what was observed.
--
--   reply           the component's answer to the command, as its execution
--                   report carried it, masked of credentials before it is
--                   stored and capped; null until a report lands.
--   reason          why the command reached its terminal status, in operator
--                   words (the reply that confirmed it, the value observed
--                   instead, the node that never took it); null while issued.
--   settled_by      what decided it: issue (nothing to settle), reply (the
--                   execution report), observation (a sample of the target),
--                   or window (the settle window closed on no evidence).
--   observed_value  the sample a settle-check judged it on, and its time,
--   observed_at     when an observation decided it.
--   settled_event_id the lifecycle event its terminal status wrote on its
--                   owner's timeline (command-settled, command-failed or
--                   command-timed-out), beside caused_event_id's
--                   command-issued.
ALTER TABLE command ADD COLUMN IF NOT EXISTS reply text;
ALTER TABLE command ADD COLUMN IF NOT EXISTS reason text;
ALTER TABLE command ADD COLUMN IF NOT EXISTS settled_by text;
ALTER TABLE command ADD COLUMN IF NOT EXISTS observed_value jsonb;
ALTER TABLE command ADD COLUMN IF NOT EXISTS observed_at timestamptz;
ALTER TABLE command ADD COLUMN IF NOT EXISTS settled_event_id bigint;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'command_settled_event_id_fkey') THEN
    ALTER TABLE command ADD CONSTRAINT command_settled_event_id_fkey
      FOREIGN KEY (settled_event_id) REFERENCES event(id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'command_settled_by_check') THEN
    ALTER TABLE command ADD CONSTRAINT command_settled_by_check
      CHECK (settled_by IS NULL OR settled_by = ANY (ARRAY['issue'::text, 'reply'::text, 'observation'::text, 'window'::text]));
  END IF;
END $$;

-- A component's command history reads newest first, filtered by time.
CREATE INDEX IF NOT EXISTS command_component_ts_idx ON public.command (component_id, ts DESC);

-- migrate:down

DROP INDEX IF EXISTS public.command_component_ts_idx;
ALTER TABLE command DROP CONSTRAINT IF EXISTS command_settled_by_check;
ALTER TABLE command DROP CONSTRAINT IF EXISTS command_settled_event_id_fkey;
ALTER TABLE command DROP COLUMN IF EXISTS settled_event_id;
ALTER TABLE command DROP COLUMN IF EXISTS observed_at;
ALTER TABLE command DROP COLUMN IF EXISTS observed_value;
ALTER TABLE command DROP COLUMN IF EXISTS settled_by;
ALTER TABLE command DROP COLUMN IF EXISTS reason;
ALTER TABLE command DROP COLUMN IF EXISTS reply;
