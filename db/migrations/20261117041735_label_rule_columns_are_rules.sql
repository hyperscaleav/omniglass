-- migrate:up
-- #2065 (ADR-2033): a template is the catalog entry an operator clones once;
-- the string a label renders from is a rule. The global tier's two columns
-- spelled the old word, so default_template (the rule this release ships, the
-- boot seed's) becomes default_rule and template (the operator's override)
-- becomes rule. Every row keeps its value; only the names move.
--
-- Guarded on the old column so a database already renamed is left as it is.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'label_rule'
               AND column_name = 'default_template') THEN
    ALTER TABLE public.label_rule RENAME COLUMN default_template TO default_rule;
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'label_rule'
               AND column_name = 'template') THEN
    ALTER TABLE public.label_rule RENAME COLUMN template TO rule;
  END IF;
END $$;

-- migrate:down
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'label_rule'
               AND column_name = 'rule') THEN
    ALTER TABLE public.label_rule RENAME COLUMN rule TO template;
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'label_rule'
               AND column_name = 'default_rule') THEN
    ALTER TABLE public.label_rule RENAME COLUMN default_rule TO default_template;
  END IF;
END $$;
