package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/settings"
)

// Omnibot conversations (#2113): a person's named conversations with omnibot,
// kept so a later session, or the person's own client after an unattended run
// paused, carries one on. A conversation is its owner's alone: every query
// carries the ownership predicate, and anyone else, an owner-role principal
// included, finds nothing (the non-disclosing ErrAssistantConversationNotFound,
// ADR-2113). Its pending field holds the write a paused run waits on; it is
// the transcript's last word, not a server-side approval, since nothing on the
// server ever acts on it.

// AssistantUsage is the tokens a conversation has spent.
type AssistantUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// AssistantConversation is one stored conversation, without its messages.
type AssistantConversation struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Pending   json.RawMessage `json:"pending,omitempty"`
	Usage     AssistantUsage  `json:"usage"`
	Messages  int             `json:"messages"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// AssistantMessage is one message of a conversation. Body is what the loop
// needs to carry the conversation on; storage keeps it as given.
type AssistantMessage struct {
	Seq       int             `json:"seq"`
	Role      string          `json:"role"`
	Body      json.RawMessage `json:"body"`
	CreatedAt time.Time       `json:"created_at"`
}

// AssistantAppend is one append: the messages after the one the client last
// saw (After, 0 for an empty conversation), the pending write the run paused
// on (nil clears it), and the tokens the turn spent.
type AssistantAppend struct {
	After    int
	Messages []AssistantMessage
	Pending  json.RawMessage
	Usage    AssistantUsage
}

var (
	// ErrAssistantConversationNotFound is a conversation the caller does not
	// own or that does not exist; the two read the same.
	ErrAssistantConversationNotFound = errors.New("storage: assistant conversation not found")
	// ErrAssistantConversationExists is a name the owner already uses.
	ErrAssistantConversationExists = errors.New("storage: an assistant conversation with this name already exists")
	// ErrAssistantConversationMoved is an append whose After is not the
	// conversation's last message: another client wrote first.
	ErrAssistantConversationMoved = errors.New("storage: the assistant conversation has moved on since it was read")
)

const assistantConversationCols = `c.id::text, c.name, c.pending, c.input_tokens, c.output_tokens,
	(select count(*) from assistant_message m where m.conversation_id = c.id), c.created_at, c.updated_at`

func scanAssistantConversation(row pgx.Row) (*AssistantConversation, error) {
	var c AssistantConversation
	var pending []byte
	if err := row.Scan(&c.ID, &c.Name, &pending, &c.Usage.InputTokens, &c.Usage.OutputTokens, &c.Messages, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAssistantConversationNotFound
		}
		return nil, fmt.Errorf("storage: scan assistant conversation: %w", err)
	}
	if pending != nil {
		c.Pending = pending
	}
	return &c, nil
}

func getAssistantConversation(ctx context.Context, q querier, ownerID, name string, lock bool) (*AssistantConversation, error) {
	sql := `select ` + assistantConversationCols + ` from assistant_conversation c where c.principal_id = $1::uuid and c.name = $2`
	if lock {
		sql += ` for update`
	}
	return scanAssistantConversation(q.QueryRow(ctx, sql, ownerID, name))
}

// assistantAudit is what an audit row says of a conversation: never a
// message's content nor the pending call's arguments.
type assistantAudit struct {
	Name     string `json:"name"`
	Messages int    `json:"messages"`
	Pending  bool   `json:"pending"`
}

func auditOf(c *AssistantConversation) assistantAudit {
	return assistantAudit{Name: c.Name, Messages: c.Messages, Pending: c.Pending != nil}
}

// CreateAssistantConversation starts a conversation for ownerID. An empty name
// is the server's to choose.
func (p *PG) CreateAssistantConversation(ctx context.Context, actorID, ownerID, name string) (*AssistantConversation, error) {
	if !isUUID(ownerID) {
		return nil, ErrAssistantConversationNotFound
	}
	if name != "" {
		if err := validateEntityName(name); err != nil {
			return nil, err
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create assistant conversation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `insert into assistant_conversation (principal_id, name)
		values ($1::uuid, coalesce(nullif($2, ''), 'c-' || left(replace(gen_random_uuid()::text, '-', ''), 10)))
		returning name`, ownerID, name).Scan(&name)
	if isUniqueViolation(err) {
		return nil, ErrAssistantConversationExists
	}
	if err != nil {
		return nil, fmt.Errorf("storage: insert assistant conversation: %w", err)
	}
	c, err := getAssistantConversation(ctx, tx, ownerID, name, false)
	if err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "assistant_conversation", c.ID, nil, auditOf(c)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create assistant conversation: %w", err)
	}
	return c, nil
}

// ListAssistantConversations lists the owner's conversations, the most
// recently moved first.
func (p *PG) ListAssistantConversations(ctx context.Context, ownerID string) ([]AssistantConversation, error) {
	out := []AssistantConversation{}
	if !isUUID(ownerID) {
		return out, nil
	}
	rows, err := p.pool.Query(ctx, `select `+assistantConversationCols+` from assistant_conversation c
		where c.principal_id = $1::uuid order by c.updated_at desc, c.name`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("storage: list assistant conversations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanAssistantConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetAssistantConversation reads one of the owner's conversations and its
// messages, in order.
func (p *PG) GetAssistantConversation(ctx context.Context, ownerID, name string) (*AssistantConversation, []AssistantMessage, error) {
	if !isUUID(ownerID) {
		return nil, nil, ErrAssistantConversationNotFound
	}
	c, err := getAssistantConversation(ctx, p.pool, ownerID, name, false)
	if err != nil {
		return nil, nil, err
	}
	rows, err := p.pool.Query(ctx, `select seq, role, body, created_at from assistant_message
		where conversation_id = $1::uuid order by seq`, c.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: read assistant messages: %w", err)
	}
	defer rows.Close()
	msgs := []AssistantMessage{}
	for rows.Next() {
		var m AssistantMessage
		var body []byte
		if err := rows.Scan(&m.Seq, &m.Role, &body, &m.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("storage: scan assistant message: %w", err)
		}
		m.Body = body
		msgs = append(msgs, m)
	}
	return c, msgs, rows.Err()
}

// AppendAssistantMessages adds messages to one of the owner's conversations,
// sets its pending write (nil clears it) and adds the turn's usage, audited.
// It is refused with ErrAssistantConversationMoved unless After is the
// conversation's last message.
func (p *PG) AppendAssistantMessages(ctx context.Context, actorID, ownerID, name string, in AssistantAppend) (*AssistantConversation, error) {
	if !isUUID(ownerID) {
		return nil, ErrAssistantConversationNotFound
	}
	if in.Pending != nil && !json.Valid(in.Pending) {
		return nil, fmt.Errorf("storage: assistant pending is not JSON")
	}
	for _, m := range in.Messages {
		if m.Role == "" || !json.Valid(m.Body) {
			return nil, fmt.Errorf("storage: an assistant message needs a role and a JSON body")
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin append assistant messages: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := getAssistantConversation(ctx, tx, ownerID, name, true)
	if err != nil {
		return nil, err
	}
	var last int
	if err := tx.QueryRow(ctx, `select coalesce(max(seq), 0) from assistant_message where conversation_id = $1::uuid`, before.ID).Scan(&last); err != nil {
		return nil, fmt.Errorf("storage: last assistant message: %w", err)
	}
	if last != in.After {
		return nil, ErrAssistantConversationMoved
	}
	for i, m := range in.Messages {
		if _, err := tx.Exec(ctx, `insert into assistant_message (conversation_id, seq, role, body) values ($1::uuid, $2, $3, $4)`,
			before.ID, last+i+1, m.Role, []byte(m.Body)); err != nil {
			return nil, fmt.Errorf("storage: insert assistant message: %w", err)
		}
	}
	var pending []byte
	if in.Pending != nil {
		pending = in.Pending
	}
	if _, err := tx.Exec(ctx, `update assistant_conversation set pending = $2, input_tokens = input_tokens + $3,
		output_tokens = output_tokens + $4, updated_at = now() where id = $1::uuid`,
		before.ID, pending, in.Usage.InputTokens, in.Usage.OutputTokens); err != nil {
		return nil, fmt.Errorf("storage: update assistant conversation: %w", err)
	}
	after, err := getAssistantConversation(ctx, tx, ownerID, name, false)
	if err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "append", "assistant_conversation", after.ID, auditOf(before), auditOf(after)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit append assistant messages: %w", err)
	}
	return after, nil
}

// DeleteAssistantConversation deletes one of the owner's conversations and
// its messages, audited.
func (p *PG) DeleteAssistantConversation(ctx context.Context, actorID, ownerID, name string) error {
	if !isUUID(ownerID) {
		return ErrAssistantConversationNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete assistant conversation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := getAssistantConversation(ctx, tx, ownerID, name, true)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from assistant_conversation where id = $1::uuid`, before.ID); err != nil {
		return fmt.Errorf("storage: delete assistant conversation: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "assistant_conversation", before.ID, auditOf(before), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete assistant conversation: %w", err)
	}
	return nil
}

// PruneAssistantConversations deletes up to limit conversations, whoever owns
// them, last changed before before; their messages go by cascade. Retention
// is not an operator's action, so it writes no audit row, as no other
// retention lane does.
func (p *PG) PruneAssistantConversations(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("storage: prune assistant conversations: limit %d, want at least 1", limit)
	}
	tag, err := p.pool.Exec(ctx, `delete from assistant_conversation where id = any(array(
		select id from assistant_conversation where updated_at < $1 limit $2))`, before, limit)
	if err != nil {
		return 0, fmt.Errorf("storage: prune assistant conversations: %w", err)
	}
	return tag.RowsAffected(), nil
}

// AssistantSettings is the effective assistant namespace (#2113).
func (p *PG) AssistantSettings(ctx context.Context) (settings.AssistantSettings, error) {
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return settings.AssistantSettings{}, fmt.Errorf("storage: assistant settings: %w", err)
	}
	return s.Assistant, nil
}
