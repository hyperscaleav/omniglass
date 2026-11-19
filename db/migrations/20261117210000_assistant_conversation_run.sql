-- migrate:up
-- A server run's claim on its conversation (#2121). run_id names the run that
-- holds the conversation and run_until when its lease lapses: a run claims the
-- conversation before any tool runs, naming the last message it read, and its
-- append or its end frees it, so two runs (two resumes of one waiting call
-- included) never act on one conversation at once. A lapsed lease is free
-- again, so a run that died does not hold the conversation for good.
ALTER TABLE assistant_conversation ADD COLUMN IF NOT EXISTS run_id text;
ALTER TABLE assistant_conversation ADD COLUMN IF NOT EXISTS run_until timestamptz;

-- migrate:down
ALTER TABLE assistant_conversation DROP COLUMN IF EXISTS run_until;
ALTER TABLE assistant_conversation DROP COLUMN IF EXISTS run_id;
