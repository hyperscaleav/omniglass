package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
)

// The notification_target (#618, #1320): WHERE a message goes. Platform-owned
// (no owner arc) and admin-only: a target is an egress destination holding a
// credential reference, so every operation takes an all-scope grant of its
// notification_target permission (the directory pattern), and the resource is
// off the viewer floor. Its URL is treated as sensitive: reads mask it, and
// RevealNotificationTargetURL returns it under an audit row.

// Channel kinds are a code-declared enum (like transports), published through
// the API's enum. webhook is the first and only kind of #618.
const ChannelWebhook = "webhook"

// ChannelKinds are the channel kinds a target may name.
var ChannelKinds = []string{ChannelWebhook}

// The secret types a target may bind: an hmac key it signs each body with,
// or a bearer token it presents.
const (
	SecretTypeHMACKey     = "hmac-key"
	SecretTypeBearerToken = "bearer-token"
)

// NotificationTarget is one destination. URL is the full destination and
// never leaves the API unmasked except through the audited reveal.
type NotificationTarget struct {
	ID         string
	Name       string
	Label      string
	Kind       string
	URL        string
	SecretID   string
	Secret     string
	SecretType string
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NotificationTargetSpec is the create input; Secret names a platform secret
// by name or uuid, empty for none.
type NotificationTargetSpec struct {
	Name    string
	Label   string
	Kind    string
	URL     string
	Secret  string
	Enabled *bool
}

// SecretUse is the caller's right over a secret it binds to a target: its
// secret read and reveal scopes and whether it holds the admin tier. Binding
// a secret hands it to whatever the target's URL names on every send, so it
// takes exactly what revealing the secret takes; a target holder without it
// would be a confused deputy, sending a credential it cannot read to a URL it
// chose.
type SecretUse struct {
	Read, Reveal scope.Set
	CanAdmin     bool
}

// NotificationTargetPatch carries the mutable fields; nil is unchanged, and
// an empty Secret unbinds it.
type NotificationTargetPatch struct {
	Label   *string
	URL     *string
	Secret  *string
	Enabled *bool
}

// Notification target sentinels.
var (
	ErrNotificationTargetNotFound  = errors.New("storage: notification target not found")
	ErrNotificationTargetExists    = errors.New("storage: a notification target with this name already exists")
	ErrNotificationTargetInvalid   = errors.New("storage: notification target is invalid")
	ErrNotificationTargetForbidden = errors.New("storage: notification targets take an all-scope grant")
	ErrNotificationTargetInUse     = errors.New("storage: notification target is in use by an action rule")
)

// MaskURL renders a destination for a read: its scheme and host, the path
// and query elided, since a chat incoming-webhook URL carries its credential
// there.
func MaskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(unreadable)"
	}
	out := u.Scheme + "://" + u.Host
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		out += "/…"
	}
	return out
}

const notificationTargetCols = `t.id, t.name, coalesce(t.label, ''), t.kind, t.url, coalesce(t.secret_id::text, ''),
	coalesce(s.name, ''), coalesce(st.name, ''), t.enabled, t.created_at, t.updated_at`

const notificationTargetFrom = ` from notification_target t
	left join secret s on s.id = t.secret_id
	left join secret_type st on st.id = s.secret_type`

func scanNotificationTarget(row pgx.Row) (*NotificationTarget, error) {
	var t NotificationTarget
	if err := row.Scan(&t.ID, &t.Name, &t.Label, &t.Kind, &t.URL, &t.SecretID, &t.Secret, &t.SecretType,
		&t.Enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// auditTarget is a target's audit image: the URL masked, so the audit log
// (read by admins, kept forever) never holds the credential a URL can be.
func auditTarget(t *NotificationTarget) map[string]any {
	if t == nil {
		return nil
	}
	return map[string]any{"id": t.ID, "name": t.Name, "label": t.Label, "kind": t.Kind, "url": MaskURL(t.URL),
		"secret": t.Secret, "enabled": t.Enabled}
}

// EgressPolicy reads the install's egress allow list (notification.egress_allow)
// and parses it; an entry that is not a CIDR, an address or a host name is an
// error naming it.
func (p *PG) EgressPolicy(ctx context.Context) (egress.Policy, error) {
	return p.egressPolicy(ctx, p.pool)
}

func (p *PG) egressPolicy(ctx context.Context, q querier) (egress.Policy, error) {
	s, err := p.platformSettings(ctx, q)
	if err != nil {
		return egress.Policy{}, err
	}
	return egress.ParsePolicy(s.Notification.EgressAllow)
}

func validChannel(kind string) bool {
	for _, k := range ChannelKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// checkTargetURL judges a destination at write time under the policy in
// force, so a URL the send would refuse (http to an unnamed host, embedded
// credentials) is refused when typed. The send judges it again, since the
// policy can change in between.
func (p *PG) checkTargetURL(ctx context.Context, q querier, raw string) error {
	pol, err := p.egressPolicy(ctx, q)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotificationTargetInvalid, err)
	}
	if _, err := pol.CheckURL(raw); err != nil {
		return fmt.Errorf("%w: url: %s", ErrNotificationTargetInvalid, err)
	}
	return nil
}

// resolveTargetSecret binds a target's credential: a PLATFORM secret (targets
// are platform-owned, so a secret owned by one component or place is not
// theirs to sign with), by name or uuid, of a type a send can use, that the
// caller may reveal (SecretUse). The reveal check is the reveal route's own
// (secretRowForAction): a secret outside the caller's read scope, or an
// admin-sensitive one without the admin tier, is the non-disclosing
// ErrSecretNotFound, and one it can read but not reveal is ErrSecretForbidden.
// Empty is no secret.
func (p *PG) resolveTargetSecret(ctx context.Context, q querier, ref string, use SecretUse) (*string, error) {
	if ref == "" {
		return nil, nil
	}
	// A bare name or a uuid, resolved through the caller's view either way.
	if !isUUID(ref) && strings.Contains(ref, "/") {
		return nil, fmt.Errorf("%w: secret %q: a target binds a platform secret by its bare name or uuid", ErrNotificationTargetInvalid, ref)
	}
	row, _, err := p.secretRowForAction(ctx, q, ref, use.Read, use.Reveal, use.CanAdmin, true)
	if err != nil {
		return nil, err
	}
	id, ownerKind := row.id, row.ownerKind
	var typ string
	if err := q.QueryRow(ctx, `select st.name from secret s join secret_type st on st.id = s.secret_type where s.id = $1::uuid`, id).Scan(&typ); err != nil {
		return nil, fmt.Errorf("storage: resolve target secret %q: %w", ref, err)
	}
	if ownerKind != "platform" {
		return nil, fmt.Errorf("%w: secret %q is not a platform secret: a target is platform-owned and signs with a platform credential", ErrNotificationTargetInvalid, ref)
	}
	if typ != SecretTypeHMACKey && typ != SecretTypeBearerToken {
		return nil, fmt.Errorf("%w: secret %q is a %s; a target binds an %s or a %s secret", ErrNotificationTargetInvalid, ref, typ, SecretTypeHMACKey, SecretTypeBearerToken)
	}
	return &id, nil
}

func getNotificationTarget(ctx context.Context, q querier, ref string, forUpdate bool) (*NotificationTarget, error) {
	if err := RejectAddressForm("notification_target", ref); err != nil {
		return nil, err
	}
	lock := ""
	if forUpdate {
		lock = " for update of t"
	}
	t, err := scanNotificationTarget(q.QueryRow(ctx, `select `+notificationTargetCols+notificationTargetFrom+` where t.`+registryRefCol(ref)+` = $1`+lock, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotificationTargetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get notification target %q: %w", ref, err)
	}
	return t, nil
}

// ListNotificationTargets lists every target, by name; an all-scope read.
func (p *PG) ListNotificationTargets(ctx context.Context, read scope.Set) ([]NotificationTarget, error) {
	if !read.All {
		return nil, ErrNotificationTargetForbidden
	}
	rows, err := p.pool.Query(ctx, `select `+notificationTargetCols+notificationTargetFrom+` order by t.name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list notification targets: %w", err)
	}
	defer rows.Close()
	out := []NotificationTarget{}
	for rows.Next() {
		t, err := scanNotificationTarget(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan notification target: %w", err)
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetNotificationTarget reads one target by name or uuid; an all-scope read.
func (p *PG) GetNotificationTarget(ctx context.Context, ref string, read scope.Set) (*NotificationTarget, error) {
	if !read.All {
		return nil, ErrNotificationTargetForbidden
	}
	return getNotificationTarget(ctx, p.pool, ref, false)
}

// CreateNotificationTarget adds a target, audited with its URL masked.
func (p *PG) CreateNotificationTarget(ctx context.Context, actorID string, spec NotificationTargetSpec, create scope.Set, use SecretUse) (*NotificationTarget, error) {
	if !create.All {
		return nil, ErrNotificationTargetForbidden
	}
	if err := ValidateName("notification_target", spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotificationTargetInvalid, err)
	}
	if !validChannel(spec.Kind) {
		return nil, fmt.Errorf("%w: kind %q is not a channel kind (webhook)", ErrNotificationTargetInvalid, spec.Kind)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create notification target: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := p.checkTargetURL(ctx, tx, spec.URL); err != nil {
		return nil, err
	}
	secretID, err := p.resolveTargetSecret(ctx, tx, spec.Secret, use)
	if err != nil {
		return nil, err
	}
	enabled := true
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	var id string
	err = tx.QueryRow(ctx, `insert into notification_target (name, label, kind, url, secret_id, enabled)
		values ($1, $2, $3, $4, $5::uuid, $6) returning id::text`,
		spec.Name, labelOrNull(spec.Label), spec.Kind, spec.URL, secretID, enabled).Scan(&id)
	if isUniqueViolation(err) {
		return nil, ErrNotificationTargetExists
	}
	if err != nil {
		return nil, fmt.Errorf("storage: insert notification target %q: %w", spec.Name, err)
	}
	t, err := getNotificationTarget(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "notification_target", t.ID, nil, auditTarget(t)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create notification target: %w", err)
	}
	return t, nil
}

// UpdateNotificationTarget patches a target, audited (URL masked; a URL
// change is recorded as changed without either value).
func (p *PG) UpdateNotificationTarget(ctx context.Context, actorID, ref string, patch NotificationTargetPatch, action scope.Set, use SecretUse) (*NotificationTarget, error) {
	if !action.All {
		return nil, ErrNotificationTargetForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update notification target: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := getNotificationTarget(ctx, tx, ref, true)
	if err != nil {
		return nil, err
	}
	if patch.URL != nil {
		if err := p.checkTargetURL(ctx, tx, *patch.URL); err != nil {
			return nil, err
		}
	}
	setSecret := patch.Secret != nil
	var secretID *string
	if setSecret {
		if secretID, err = p.resolveTargetSecret(ctx, tx, *patch.Secret, use); err != nil {
			return nil, err
		}
	}
	setLabel, labelVal := labelPatch(patch.Label)
	if _, err := tx.Exec(ctx, `update notification_target set
			label = case when $2::boolean then $3 else label end,
			url = coalesce($4, url),
			secret_id = case when $5::boolean then $6::uuid else secret_id end,
			enabled = coalesce($7, enabled),
			updated_at = now()
		where id = $1`, before.ID, setLabel, labelVal, patch.URL, setSecret, secretID, patch.Enabled); err != nil {
		return nil, fmt.Errorf("storage: update notification target %q: %w", ref, err)
	}
	after, err := getNotificationTarget(ctx, tx, before.ID, false)
	if err != nil {
		return nil, err
	}
	img := auditTarget(after)
	img["url_changed"] = after.URL != before.URL
	if err := writeAuditRes(ctx, tx, actorID, "update", "notification_target", before.ID, auditTarget(before), img); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update notification target: %w", err)
	}
	return after, nil
}

// DeleteNotificationTarget removes a target no action rule sends to, audited.
func (p *PG) DeleteNotificationTarget(ctx context.Context, actorID, ref string, action scope.Set) error {
	if !action.All {
		return ErrNotificationTargetForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete notification target: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := getNotificationTarget(ctx, tx, ref, true)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from notification_target where id = $1`, before.ID); err != nil {
		if isForeignKeyViolation(err) {
			return ErrNotificationTargetInUse
		}
		return fmt.Errorf("storage: delete notification target %q: %w", ref, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "notification_target", before.ID, auditTarget(before), nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RevealNotificationTargetURL returns a target's full URL, writing a reveal
// audit row in the same transaction (the URL itself is not in the row).
func (p *PG) RevealNotificationTargetURL(ctx context.Context, actorID, ref string, action scope.Set) (string, error) {
	if !action.All {
		return "", ErrNotificationTargetForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("storage: begin reveal notification target: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	t, err := getNotificationTarget(ctx, tx, ref, false)
	if err != nil {
		return "", err
	}
	if err := writeAuditRes(ctx, tx, actorID, "reveal", "notification_target", t.ID, nil, map[string]any{"name": t.Name, "field": "url"}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("storage: commit reveal notification target: %w", err)
	}
	return t.URL, nil
}

// TargetSend is what a send to one target needs: its URL, whether it is
// enabled, and its credential unsealed. It is the platform's own read, never
// a caller's, and never leaves the process.
type TargetSend struct {
	TargetID, Name, URL string
	Enabled             bool
	HMACKey             string
	BearerToken         string
	// CredentialMissing is a bound secret that no longer unseals.
	CredentialMissing bool
}

// String never renders the URL or a credential.
func (t TargetSend) String() string { return "notification target " + t.Name }

func (p *PG) targetSend(ctx context.Context, q querier, ref string) (*TargetSend, error) {
	t, err := getNotificationTarget(ctx, q, ref, false)
	if err != nil {
		return nil, err
	}
	out := &TargetSend{TargetID: t.ID, Name: t.Name, URL: t.URL, Enabled: t.Enabled}
	if t.SecretID == "" {
		return out, nil
	}
	fields, err := p.unsealSecretByID(ctx, q, t.SecretID)
	if err != nil {
		out.CredentialMissing = true
		return out, nil
	}
	switch t.SecretType {
	case SecretTypeHMACKey:
		out.HMACKey = fields["key"]
	case SecretTypeBearerToken:
		out.BearerToken = fields["token"]
	}
	if out.HMACKey == "" && out.BearerToken == "" {
		out.CredentialMissing = true
	}
	return out, nil
}

// unsealSecretByID unseals a secret's fields by uuid for the platform's own
// use (a notification's signature), with no caller scope: the binding was
// the admin's audited act.
func (p *PG) unsealSecretByID(ctx context.Context, q querier, id string) (map[string]string, error) {
	var row secretRow
	var typ string
	var comp, sys, loc *string
	var value []byte
	if err := q.QueryRow(ctx, `select s.id::text, s.name, coalesce(s.label, ''), st.name, s.owner_kind, s.component_id, s.system_id, s.location_id, s.admin_sensitive, s.value
		from secret s join secret_type st on st.id = s.secret_type where s.id = $1::uuid`, id).
		Scan(&row.id, &row.name, &row.label, &typ, &row.ownerKind, &comp, &sys, &loc, &row.adminSensitive, &value); err != nil {
		return nil, fmt.Errorf("storage: read secret %s: %w", id, err)
	}
	row.ownerID = firstNonNil(comp, sys, loc)
	if err := json.Unmarshal(value, &row.value); err != nil {
		return nil, fmt.Errorf("storage: decode secret value: %w", err)
	}
	st, err := p.GetSecretType(ctx, typ)
	if err != nil {
		return nil, err
	}
	var shape secret.Shape = st.Shape()
	fields, _, err := p.unsealSecretFields(ctx, row, shape)
	return fields, err
}

// TargetForTest reads a target's send for the :test verb, all-scope gated.
func (p *PG) TargetForTest(ctx context.Context, ref string, action scope.Set) (*TargetSend, error) {
	if !action.All {
		return nil, ErrNotificationTargetForbidden
	}
	return p.targetSend(ctx, p.pool, ref)
}

// AuditTargetTest records one :test send and its result (never the URL, the
// envelope or a credential).
func (p *PG) AuditTargetTest(ctx context.Context, actorID, targetID string, status int, ok bool, reason string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin audit target test: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := writeAuditRes(ctx, tx, actorID, "test", "notification_target", targetID, nil,
		map[string]any{"ok": ok, "status": status, "reason": reason}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
