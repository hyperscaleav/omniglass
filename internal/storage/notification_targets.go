package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/mail"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
)

// The notification_target (#618, #1320): WHERE a message goes. Platform-owned
// (no owner arc) and admin-only: a target is an egress destination holding a
// credential reference, so every operation takes an all-scope grant of its
// notification_target permission (the directory pattern), and the resource is
// off the viewer floor. Its URL is treated as sensitive: reads mask it, and
// RevealNotificationTargetURL returns it under an audit row.

// Target kinds are a code-declared enum (like transports), published through
// the API's enum. webhook is #618's; email (a fixed set of addresses, a
// shared mailbox) is #1317's; a principal group and a principal (one person)
// are #1341's, the targets that name people rather than a destination.
const (
	ChannelWebhook       = "webhook"
	TargetEmail          = "email"
	TargetPrincipalGroup = "principal_group"
	TargetPrincipal      = "principal"
)

// ChannelKinds are the kinds a target may name.
var ChannelKinds = []string{ChannelWebhook, TargetEmail, TargetPrincipalGroup, TargetPrincipal}

// Delivery channels: the way one notification row travels. A webhook row goes
// to a target's URL; a person's row goes to their console inbox (delivered by
// its own commit) or, from #1317, to their email.
const (
	DeliveryWebhook = "webhook"
	DeliveryInbox   = "inbox"
	DeliveryEmail   = "email"
)

// PersonChannels are the channels a group or person target may deliver on.
var PersonChannels = []string{DeliveryInbox, DeliveryEmail}

// deliveryFor is the channel a destination target's row travels on.
func deliveryFor(kind string) string {
	if kind == TargetEmail {
		return DeliveryEmail
	}
	return DeliveryWebhook
}

// isPersonKind reports whether a target kind names people.
func isPersonKind(kind string) bool { return kind == TargetPrincipalGroup || kind == TargetPrincipal }

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
	// A person target's binding (#1341): the group it expands to, or the one
	// person, each by name and uuid, and the channels it delivers on.
	PrincipalGroupID string
	PrincipalGroup   string
	PrincipalID      string
	Principal        string
	Channels         []string
	// Addresses are an email target's fixed recipients (#1317).
	Addresses []string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt        time.Time
}

// NotificationTargetSpec is the create input; Secret names a platform secret
// by name or uuid, empty for none.
type NotificationTargetSpec struct {
	Name   string
	Label  string
	Kind   string
	URL    string
	Secret string
	// PrincipalGroup (a group's name or uuid) binds a principal_group target,
	// Principal (a username or uuid) a principal target; Channels are the
	// channels a person target delivers on.
	PrincipalGroup string
	Principal      string
	Channels       []string
	// Addresses are an email target's recipients.
	Addresses []string
	Enabled   *bool
}

// PeopleUse is the caller's right over the people a target would name: binding
// a group or a person is reading the principal directory (its members are who
// the target reaches), so it takes that directory's admin-tier read, as
// binding a secret takes the right to reveal it.
type PeopleUse struct {
	Groups, Principals bool
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
	Label     *string
	URL       *string
	Secret    *string
	Channels  *[]string
	Addresses *[]string
	Enabled   *bool
}

// Notification target sentinels.
var (
	ErrNotificationTargetNotFound  = errors.New("storage: notification target not found")
	ErrNotificationTargetExists    = errors.New("storage: a notification target with this name already exists")
	ErrNotificationTargetInvalid   = errors.New("storage: notification target is invalid")
	ErrNotificationTargetForbidden = errors.New("storage: notification targets take an all-scope grant")
	ErrNotificationTargetInUse     = errors.New("storage: notification target is in use by an action rule")
	// ErrNotificationTargetPeopleForbidden is binding a group or a person
	// without the principal directory's admin-tier read.
	ErrNotificationTargetPeopleForbidden = errors.New("storage: binding a group or a person takes the principal directory's read")
	// ErrBoundToTarget is deleting a group or a person a target names.
	ErrBoundToTarget = errors.New("storage: a notification target names it")
)

// TargetBindingError is a delete refused because a notification target names
// the group or person: it names the target, so the admin knows what to unbind.
type TargetBindingError struct {
	What   string // "principal group" or "principal"
	Target string
}

func (e *TargetBindingError) Error() string {
	return "storage: the notification target " + e.Target + " notifies this " + e.What
}

func (e *TargetBindingError) Unwrap() error { return ErrBoundToTarget }

// boundTarget refuses deleting a group or a principal a target binds, naming
// the first such target; col is principal_group_id or principal_id.
func boundTarget(ctx context.Context, q querier, col, id, what string) error {
	var name string
	err := q.QueryRow(ctx, `select name from notification_target where `+col+` = $1::uuid order by name limit 1`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: targets bound to %s %s: %w", what, id, err)
	}
	return &TargetBindingError{What: what, Target: name}
}

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

const notificationTargetCols = `t.id, t.name, coalesce(t.label, ''), t.kind, coalesce(t.url, ''), coalesce(t.secret_id::text, ''),
	coalesce(s.name, ''), coalesce(st.name, ''), coalesce(t.principal_group_id::text, ''), coalesce(g.name, ''),
	coalesce(t.principal_id::text, ''), coalesce(h.username, ''), t.channels, t.addresses, t.enabled, t.created_at, t.updated_at`

const notificationTargetFrom = ` from notification_target t
	left join secret s on s.id = t.secret_id
	left join secret_type st on st.id = s.secret_type
	left join principal_group g on g.id = t.principal_group_id
	left join human h on h.principal_id = t.principal_id`

func scanNotificationTarget(row pgx.Row) (*NotificationTarget, error) {
	var t NotificationTarget
	if err := row.Scan(&t.ID, &t.Name, &t.Label, &t.Kind, &t.URL, &t.SecretID, &t.Secret, &t.SecretType,
		&t.PrincipalGroupID, &t.PrincipalGroup, &t.PrincipalID, &t.Principal, &t.Channels, &t.Addresses,
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
	img := map[string]any{"id": t.ID, "name": t.Name, "label": t.Label, "kind": t.Kind, "secret": t.Secret, "enabled": t.Enabled}
	if t.URL != "" {
		img["url"] = MaskURL(t.URL)
	}
	if isPersonKind(t.Kind) {
		img["principal_group"], img["principal"], img["channels"] = t.PrincipalGroup, t.Principal, t.Channels
	}
	if t.Kind == TargetEmail {
		img["addresses"] = t.Addresses
	}
	return img
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
func (p *PG) CreateNotificationTarget(ctx context.Context, actorID string, spec NotificationTargetSpec, create scope.Set, use SecretUse, people PeopleUse) (*NotificationTarget, error) {
	if !create.All {
		return nil, ErrNotificationTargetForbidden
	}
	if err := ValidateName("notification_target", spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotificationTargetInvalid, err)
	}
	if !validChannel(spec.Kind) {
		return nil, fmt.Errorf("%w: kind %q is not a target kind (webhook, email, principal_group, principal)", ErrNotificationTargetInvalid, spec.Kind)
	}
	if err := checkTargetShape(spec, people); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create notification target: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var secretID, groupID, principalID *string
	var channels, addresses []string
	switch {
	case spec.Kind == TargetEmail:
		if addresses, err = checkAddresses(spec.Addresses); err != nil {
			return nil, err
		}
	case isPersonKind(spec.Kind):
		if channels, err = checkPersonChannels(spec.Channels); err != nil {
			return nil, err
		}
		if groupID, principalID, err = resolveTargetPeople(ctx, tx, spec); err != nil {
			return nil, err
		}
	default:
		if err := p.checkTargetURL(ctx, tx, spec.URL); err != nil {
			return nil, err
		}
		if secretID, err = p.resolveTargetSecret(ctx, tx, spec.Secret, use); err != nil {
			return nil, err
		}
	}
	if channels == nil {
		channels = []string{}
	}
	if addresses == nil {
		addresses = []string{}
	}
	enabled := true
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	var id string
	err = tx.QueryRow(ctx, `insert into notification_target (name, label, kind, url, secret_id, principal_group_id, principal_id, channels, addresses, enabled)
		values ($1, $2, $3, nullif($4, ''), $5::uuid, $6::uuid, $7::uuid, $8, $9, $10) returning id::text`,
		spec.Name, labelOrNull(spec.Label), spec.Kind, spec.URL, secretID, groupID, principalID, channels, addresses, enabled).Scan(&id)
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

// checkTargetShape refuses the fields a kind does not carry, and a group or
// person binding the caller may not make. The permission is judged before any
// name resolves, so a caller without the directory's read learns nothing about
// which groups or people exist.
func checkTargetShape(spec NotificationTargetSpec, people PeopleUse) error {
	switch spec.Kind {
	case ChannelWebhook:
		if spec.PrincipalGroup != "" || spec.Principal != "" || len(spec.Channels) > 0 || len(spec.Addresses) > 0 {
			return fmt.Errorf("%w: a webhook target names a URL, not a group, a person, channels or addresses", ErrNotificationTargetInvalid)
		}
		return nil
	case TargetEmail:
		if spec.URL != "" || spec.Secret != "" || spec.PrincipalGroup != "" || spec.Principal != "" || len(spec.Channels) > 0 {
			return fmt.Errorf("%w: an email target names its addresses alone; the relay and its credential are the install's (notification settings)", ErrNotificationTargetInvalid)
		}
		return nil
	case TargetPrincipalGroup:
		if !people.Groups {
			return ErrNotificationTargetPeopleForbidden
		}
		if spec.PrincipalGroup == "" || spec.Principal != "" {
			return fmt.Errorf("%w: a principal_group target names one group (principal_group) and no person", ErrNotificationTargetInvalid)
		}
	case TargetPrincipal:
		if !people.Principals {
			return ErrNotificationTargetPeopleForbidden
		}
		if spec.Principal == "" || spec.PrincipalGroup != "" {
			return fmt.Errorf("%w: a principal target names one person (principal) and no group", ErrNotificationTargetInvalid)
		}
	}
	if spec.URL != "" || spec.Secret != "" || len(spec.Addresses) > 0 {
		return fmt.Errorf("%w: a %s target reaches people on their channels; it has no URL, secret or addresses", ErrNotificationTargetInvalid, spec.Kind)
	}
	return nil
}

// checkAddresses returns an email target's addresses, each parsed (a bare
// address or "Name <address>") and kept as its address part, deduplicated in
// the order given; none is refused.
func checkAddresses(in []string) ([]string, error) {
	var out []string
	for _, raw := range in {
		a, err := mail.CheckAddress(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: addresses: %q is not an email address", ErrNotificationTargetInvalid, raw)
		}
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: addresses: name at least one", ErrNotificationTargetInvalid)
	}
	return out, nil
}

// checkPersonChannels returns a person target's channels deduplicated in
// their canonical order, refusing none and any it cannot deliver on.
func checkPersonChannels(chs []string) ([]string, error) {
	for _, c := range chs {
		if !slices.Contains(PersonChannels, c) {
			return nil, fmt.Errorf("%w: channels: %q is not a person channel (%s)", ErrNotificationTargetInvalid, c, strings.Join(PersonChannels, ", "))
		}
	}
	var out []string
	for _, c := range PersonChannels {
		if slices.Contains(chs, c) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: channels: name at least one (%s)", ErrNotificationTargetInvalid, strings.Join(PersonChannels, ", "))
	}
	return out, nil
}

// resolveTargetPeople binds a person target: the group by name or uuid, or the
// person by username or uuid, who must be a human (a service account or a
// node is no one to tell).
func resolveTargetPeople(ctx context.Context, q querier, spec NotificationTargetSpec) (groupID, principalID *string, err error) {
	if spec.Kind == TargetPrincipalGroup {
		id, err := groupIDByRef(ctx, q, spec.PrincipalGroup)
		if err == nil {
			err = q.QueryRow(ctx, `select id::text from principal_group where id = $1::uuid`, id).Scan(&id)
		}
		if errors.Is(err, ErrGroupNotFound) || errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, fmt.Errorf("%w: principal_group %q is not a group", ErrNotificationTargetInvalid, spec.PrincipalGroup)
		}
		if err != nil {
			return nil, nil, err
		}
		return &id, nil, nil
	}
	col := "username"
	if isUUID(spec.Principal) {
		col = "principal_id::text"
	}
	var id string
	err = q.QueryRow(ctx, `select principal_id::text from human where `+col+` = $1`, spec.Principal).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, fmt.Errorf("%w: principal %q is not a person: a principal target names a human by username or uuid", ErrNotificationTargetInvalid, spec.Principal)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("storage: resolve target principal %q: %w", spec.Principal, err)
	}
	return nil, &id, nil
}

// UpdateNotificationTarget patches a target, audited (URL masked; a URL
// change is recorded as changed without either value). Every path that
// changes where a bound credential goes takes the right to reveal it: binding
// a secret, and moving the URL of a target that keeps one. Nothing else on a
// target redirects a send (no header, method or signing field exists, and a
// target is not renamed or cloned); :test sends to the URL as it stands.
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
	var channels, addresses []string
	switch {
	case isPersonKind(before.Kind):
		if patch.URL != nil || (patch.Secret != nil && *patch.Secret != "") || patch.Addresses != nil {
			return nil, fmt.Errorf("%w: a %s target reaches people on their channels; it has no URL, secret or addresses", ErrNotificationTargetInvalid, before.Kind)
		}
		if patch.Channels != nil {
			if channels, err = checkPersonChannels(*patch.Channels); err != nil {
				return nil, err
			}
		}
	case before.Kind == TargetEmail:
		if patch.URL != nil || (patch.Secret != nil && *patch.Secret != "") || patch.Channels != nil {
			return nil, fmt.Errorf("%w: an email target names its addresses alone", ErrNotificationTargetInvalid)
		}
		if patch.Addresses != nil {
			if addresses, err = checkAddresses(*patch.Addresses); err != nil {
				return nil, err
			}
		}
	case patch.Channels != nil || patch.Addresses != nil:
		return nil, fmt.Errorf("%w: a webhook target has no channels or addresses", ErrNotificationTargetInvalid)
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
	// Moving a target that keeps its bound secret is handing that secret to
	// the new URL on the next send, so it takes the same right as binding it.
	// The caller already reads the secret's name off the target, so a secret
	// it cannot see is answered forbidden here, not hidden.
	if patch.URL != nil && *patch.URL != before.URL && !setSecret && before.SecretID != "" {
		if _, err := p.resolveTargetSecret(ctx, tx, before.SecretID, use); err != nil {
			if errors.Is(err, ErrSecretNotFound) {
				return nil, ErrSecretForbidden
			}
			return nil, err
		}
	}
	setLabel, labelVal := labelPatch(patch.Label)
	if _, err := tx.Exec(ctx, `update notification_target set
			label = case when $2::boolean then $3 else label end,
			url = coalesce($4, url),
			secret_id = case when $5::boolean then $6::uuid else secret_id end,
			enabled = coalesce($7, enabled),
			channels = coalesce($8, channels),
			addresses = coalesce($9, addresses),
			updated_at = now()
		where id = $1`, before.ID, setLabel, labelVal, patch.URL, setSecret, secretID, patch.Enabled, channels, addresses); err != nil {
		return nil, fmt.Errorf("storage: update notification target %q: %w", ref, err)
	}
	after, err := getNotificationTarget(ctx, tx, before.ID, false)
	if err != nil {
		return nil, err
	}
	img := auditTarget(after)
	if after.Kind == ChannelWebhook {
		img["url_changed"] = after.URL != before.URL
	}
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
	// Kind is webhook or email; Addresses an email target's recipients.
	Kind      string
	Addresses []string
	Enabled   bool
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
	out := &TargetSend{TargetID: t.ID, Name: t.Name, URL: t.URL, Kind: t.Kind, Addresses: t.Addresses, Enabled: t.Enabled}
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
	t, err := getNotificationTarget(ctx, p.pool, ref, false)
	if err != nil {
		return nil, err
	}
	if isPersonKind(t.Kind) {
		return nil, fmt.Errorf("%w: a test send goes to a webhook or an email target; a %s target reaches people, who are told only about alarms", ErrNotificationTargetInvalid, t.Kind)
	}
	return p.targetSend(ctx, p.pool, t.ID)
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
