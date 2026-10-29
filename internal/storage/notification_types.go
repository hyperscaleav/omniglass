package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The notification_type registry (#618, #1319): the outbound peer of
// command_type, saying WHAT a message about an alarm says. A row carries two
// templates, a one-line subject and a body text, each rendered through the
// shared template sandbox (internal/sandbox) over the alarm fact map
// (notification_facts.go). Fleet-wide reference data: no scope, read on the
// viewer floor. Official rows ship in the boot seed and are read-only.

// NotificationType is one registered message shape.
type NotificationType struct {
	ID          string
	Name        string
	Label       string
	Description string
	Subject     string
	Text        string
	Official    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NotificationTypeSpec is the create input; Name is fixed at creation and
// moves only through the registry rename.
type NotificationTypeSpec struct {
	Name        string
	Label       string
	Description string
	Subject     string
	Text        string
}

// NotificationTypePatch carries the mutable fields; nil is unchanged.
type NotificationTypePatch struct {
	Label       *string
	Description *string
	Subject     *string
	Text        *string
}

// Notification type sentinels. A row the registry rename refuses answers
// with the shared ErrTypeOfficial / ErrTypeExists / ErrTypeNotFound.
var (
	ErrNotificationTypeNotFound = errors.New("storage: notification type not found")
	ErrNotificationTypeExists   = errors.New("storage: a notification type with this name already exists")
	ErrNotificationTypeOfficial = errors.New("storage: an official notification type is read-only")
	ErrNotificationTypeInvalid  = errors.New("storage: notification type is invalid")
	// ErrNotificationTypeInUse is a delete of a type an action rule still sends.
	ErrNotificationTypeInUse = errors.New("storage: notification type is in use by an action rule")
	// ErrNotificationRender is a template that parses but fails over one
	// alarm's facts (its output past the cap): what a delivery would record.
	ErrNotificationRender = errors.New("storage: notification failed to render")
)

const notificationTypeCols = `id, name, coalesce(label, ''), description, subject, text, official, created_at, updated_at`

func scanNotificationType(row pgx.Row) (*NotificationType, error) {
	var t NotificationType
	if err := row.Scan(&t.ID, &t.Name, &t.Label, &t.Description, &t.Subject, &t.Text, &t.Official, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// validateNotificationTemplates compiles both templates so a write can refuse
// one the sandbox would refuse at render: the refusal names which template and
// what in it, and the row is never stored.
func validateNotificationTemplates(subject, text *string) error {
	if subject != nil {
		if _, err := ParseNotificationTemplate(*subject); err != nil {
			return fmt.Errorf("%w: subject: %s", ErrNotificationTypeInvalid, err)
		}
	}
	if text != nil {
		if _, err := ParseNotificationTemplate(*text); err != nil {
			return fmt.Errorf("%w: text: %s", ErrNotificationTypeInvalid, err)
		}
	}
	return nil
}

// UpsertNotificationType installs an official type in the og namespace,
// authoritative over its own row only (the boot-seed bucket, #1431): an
// operator's type of the same name is left exactly as it is. The seed's templates pass the same compile check
// the CRUD lane does, naming the row: a seed an operator would be refused
// stops the boot instead of shipping.
func (p *PG) UpsertNotificationType(ctx context.Context, t NotificationType) error {
	if err := ValidateName("notification_type", t.Name); err != nil {
		return fmt.Errorf("storage: upsert notification type %q: %w", t.Name, err)
	}
	if err := validateNotificationTemplates(&t.Subject, &t.Text); err != nil {
		return fmt.Errorf("storage: upsert notification type %q: %w", t.Name, err)
	}
	if _, err := p.pool.Exec(ctx, `
		insert into notification_type (namespace, name, label, description, subject, text, official)
		values ('og', $1, $2, $3, $4, $5, true)
		on conflict (name) do update set label = excluded.label, description = excluded.description,
			subject = excluded.subject, text = excluded.text, official = true, updated_at = now()
		where notification_type.namespace = excluded.namespace`,
		t.Name, labelOrNull(t.Label), t.Description, t.Subject, t.Text); err != nil {
		return fmt.Errorf("storage: upsert notification type %q: %w", t.Name, err)
	}
	return nil
}

// ListNotificationTypes returns every registered type, by name.
func (p *PG) ListNotificationTypes(ctx context.Context) ([]NotificationType, error) {
	rows, err := p.pool.Query(ctx, `select `+notificationTypeCols+` from notification_type order by name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list notification types: %w", err)
	}
	defer rows.Close()
	out := []NotificationType{}
	for rows.Next() {
		t, err := scanNotificationType(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan notification type: %w", err)
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetNotificationType reads one type by name or uuid.
func (p *PG) GetNotificationType(ctx context.Context, ref string) (*NotificationType, error) {
	return getNotificationType(ctx, p.pool, ref)
}

func getNotificationType(ctx context.Context, q querier, ref string) (*NotificationType, error) {
	if err := RejectAddressForm("notification_type", ref); err != nil {
		return nil, err
	}
	t, err := scanNotificationType(q.QueryRow(ctx, `select `+notificationTypeCols+` from notification_type where `+registryRefCol(ref)+` = $1`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotificationTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get notification type %q: %w", ref, err)
	}
	return t, nil
}

// CreateNotificationType adds an operator type (official=false), audited.
func (p *PG) CreateNotificationType(ctx context.Context, actorID string, spec NotificationTypeSpec) (*NotificationType, error) {
	if err := ValidateName("notification_type", spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotificationTypeInvalid, err)
	}
	if err := validateNotificationTemplates(&spec.Subject, &spec.Text); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create notification type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	t, err := scanNotificationType(tx.QueryRow(ctx, `
		insert into notification_type (name, label, description, subject, text, official)
		values ($1, $2, $3, $4, $5, false) returning `+notificationTypeCols,
		spec.Name, labelOrNull(spec.Label), spec.Description, spec.Subject, spec.Text))
	if isUniqueViolation(err) {
		return nil, ErrNotificationTypeExists
	}
	if err != nil {
		return nil, fmt.Errorf("storage: insert notification type %q: %w", spec.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "notification_type", t.ID, nil, t); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create notification type: %w", err)
	}
	return t, nil
}

// lockMutableNotificationType reads a type for change under its row lock,
// refusing an official one.
func lockMutableNotificationType(ctx context.Context, tx pgx.Tx, ref string) (*NotificationType, error) {
	if err := RejectAddressForm("notification_type", ref); err != nil {
		return nil, err
	}
	t, err := scanNotificationType(tx.QueryRow(ctx, `select `+notificationTypeCols+` from notification_type where `+registryRefCol(ref)+` = $1 for update`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotificationTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: read notification type %q: %w", ref, err)
	}
	if t.Official {
		return nil, ErrNotificationTypeOfficial
	}
	return t, nil
}

// UpdateNotificationType patches an operator type, audited.
func (p *PG) UpdateNotificationType(ctx context.Context, actorID, ref string, patch NotificationTypePatch) (*NotificationType, error) {
	if err := validateNotificationTemplates(patch.Subject, patch.Text); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update notification type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := lockMutableNotificationType(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	setLabel, labelVal := labelPatch(patch.Label)
	after, err := scanNotificationType(tx.QueryRow(ctx, `
		update notification_type set
			label = case when $2::boolean then $3 else label end,
			description = coalesce($4, description),
			subject = coalesce($5, subject),
			text = coalesce($6, text),
			updated_at = now()
		where id = $1 returning `+notificationTypeCols,
		before.ID, setLabel, labelVal, patch.Description, patch.Subject, patch.Text))
	if err != nil {
		return nil, fmt.Errorf("storage: update notification type %q: %w", ref, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "notification_type", before.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update notification type: %w", err)
	}
	return after, nil
}

// DeleteNotificationType removes an operator type no action rule sends,
// audited.
func (p *PG) DeleteNotificationType(ctx context.Context, actorID, ref string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete notification type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := lockMutableNotificationType(ctx, tx, ref)
	if err != nil {
		return err
	}
	// A personal rule (#1353) never pins a type an admin deletes: it falls
	// back to the official type for the transition, as a rule naming none
	// sends. Only a platform rule, which an admin can see and edit, holds it.
	if _, err := tx.Exec(ctx, `update action_rule set notification_type_id = null, updated_at = now()
		where notification_type_id = $1 and owner_principal_id is not null`, before.ID); err != nil {
		return fmt.Errorf("storage: release personal rules from notification type %q: %w", ref, err)
	}
	if _, err := tx.Exec(ctx, `delete from notification_type where id = $1`, before.ID); err != nil {
		if isForeignKeyViolation(err) {
			return ErrNotificationTypeInUse
		}
		return fmt.Errorf("storage: delete notification type %q: %w", ref, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "notification_type", before.ID, before, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// NotificationPreview is what a delivery of one alarm transition would carry:
// the rendered subject and text, and the facts they were rendered from.
type NotificationPreview struct {
	Subject    string
	Text       string
	Transition string
	Facts      map[string]string
}

// PreviewNotificationType renders a type's templates (or a draft's, when
// subject or text is given) against one alarm on a component the caller has
// already resolved in scope, exactly as a delivery of that transition would,
// and writes nothing. An empty transition reads the alarm's own state:
// cleared when it is cleared, raised otherwise. A draft template that does not
// parse is ErrNotificationTypeInvalid; one that fails at render returns the
// render error, which is what a delivery would record.
func (p *PG) PreviewNotificationType(ctx context.Context, ref, componentID, alarmID, transition string, subject, text *string) (*NotificationPreview, error) {
	if err := validateNotificationTemplates(subject, text); err != nil {
		return nil, err
	}
	t, err := getNotificationType(ctx, p.pool, ref)
	if err != nil {
		return nil, err
	}
	if subject != nil {
		t.Subject = *subject
	}
	if text != nil {
		t.Text = *text
	}
	if alarmID == "" {
		if alarmID, err = latestAlarmID(ctx, p.pool, componentID); err != nil {
			return nil, err
		}
	}
	facts, tr, err := p.alarmFacts(ctx, p.pool, componentID, alarmID, transition)
	if err != nil {
		return nil, err
	}
	eng, err := p.notificationEngine(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	sub, body, err := renderNotification(eng, t.Subject, t.Text, facts)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotificationRender, err)
	}
	return &NotificationPreview{Subject: sub, Text: body, Transition: tr, Facts: facts}, nil
}

// latestAlarmID is a component's most recently raised alarm, the one a
// preview names when the caller names none: ErrAlarmNotFound for a component
// that has never had one.
func latestAlarmID(ctx context.Context, q querier, componentID string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `select id::text from alarm where component_id = $1::uuid order by raised_at desc, id desc limit 1`, componentID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAlarmNotFound
	}
	if err != nil {
		return "", fmt.Errorf("storage: latest alarm on %s: %w", componentID, err)
	}
	return id, nil
}

// TemplateRefusal words a template refusal or render failure for the caller:
// the sentinel prefixes stripped, so it reads as the problem ("subject:
// function "printf" is not available to a template") rather than the path.
func TemplateRefusal(err error) string {
	s := err.Error()
	for _, p := range []string{"storage: notification type is invalid: ", "storage: notification failed to render: ", "sandbox: template does not parse: ", "sandbox: template failed to render: "} {
		s = strings.ReplaceAll(s, p, "")
	}
	return s
}
