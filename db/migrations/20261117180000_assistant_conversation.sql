-- migrate:up
-- Omnibot conversations (#2113). assistant_conversation is one person's named
-- conversation with omnibot: only its owner reads or writes it. pending holds
-- the write a paused run is waiting on, so the owner can continue it later in
-- their own client; it is a kept transcript, not a server-side approval. The
-- usage columns total the tokens the conversation has spent. assistant_message
-- is the transcript, in order: each message's body is what the loop needs to
-- carry the conversation on. Retention prunes a conversation by updated_at, and
-- its messages go with it by cascade.
CREATE TABLE IF NOT EXISTS assistant_conversation (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    principal_id uuid NOT NULL REFERENCES principal (id) ON DELETE CASCADE,
    name text NOT NULL,
    pending jsonb,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (principal_id, name)
);
CREATE INDEX IF NOT EXISTS assistant_conversation_updated_idx ON assistant_conversation (updated_at);

CREATE TABLE IF NOT EXISTS assistant_message (
    conversation_id uuid NOT NULL REFERENCES assistant_conversation (id) ON DELETE CASCADE,
    seq integer NOT NULL,
    role text NOT NULL,
    body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, seq)
);

-- migrate:down
DROP TABLE IF EXISTS assistant_message;
DROP TABLE IF EXISTS assistant_conversation;
