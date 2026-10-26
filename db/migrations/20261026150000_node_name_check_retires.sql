-- migrate:up

-- The node table carried its own name rule (#570): a CHECK admitting any name
-- without a dot, '*', '>' or whitespace. That is a fifth rule, looser than the
-- one storage.ValidateName enforces (it admitted uppercase, underscores, '$'
-- and '/'), so it never fired and only misdescribed the identifier. Its intent,
-- a name safe to embed in a NATS subject, is a property of the entity name rule
-- itself, which the gateway applies before any node row is written. No row
-- changes: every stored name already satisfies the tighter rule.
ALTER TABLE node DROP CONSTRAINT IF EXISTS node_name_subject_safe_check;

-- migrate:down

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint
                 WHERE conrelid = 'node'::regclass AND conname = 'node_name_subject_safe_check') THEN
    ALTER TABLE node ADD CONSTRAINT node_name_subject_safe_check
      CHECK (name ~ '^[^.*> \t\n\r]+$'::text);
  END IF;
END $$;
