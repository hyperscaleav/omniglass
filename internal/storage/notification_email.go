package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/mail"
)

// The email channel's send (#1317, #1341): what the tick hands the mail
// sender for one due email row, read inside the row's own transaction while
// its lock is held. A fixed-address email target sends to its addresses; a
// person's email row sends to that person's directory email, resolved now,
// after the recipient is judged again (ADR-1341): a row can wait out retries
// for about 21 minutes, in which time a person can lose the alarm.

// MailRelay is the install's relay as a send needs it: its address, security
// mode and From, and the credential of its bound secret, unsealed. Never
// printed.
type MailRelay struct {
	Addr, Security, From string
	Username, Password   string
}

// String never renders the credential.
func (r MailRelay) String() string { return "mail relay " + r.Addr }

// EmailSend is one email row's send: the relay, the recipients, and the
// row's own time, which the message is dated with so every attempt carries
// the same bytes.
type EmailSend struct {
	Relay MailRelay
	To    []string
	Date  time.Time
}

// emailSend reads one email row's send, or the plain reason it is refused.
func (p *PG) emailSend(ctx context.Context, q querier, targetID, recipientID *string, componentID string, at time.Time) (*EmailSend, string, error) {
	if targetID == nil {
		return nil, "the target was deleted", nil
	}
	t, err := getNotificationTarget(ctx, q, *targetID, false)
	if errors.Is(err, ErrNotificationTargetNotFound) {
		return nil, "the target was deleted", nil
	}
	if err != nil {
		return nil, "", err
	}
	if !t.Enabled {
		return nil, "the target is disabled", nil
	}
	var to []string
	switch {
	case t.Kind == TargetEmail:
		to = t.Addresses
	case isPersonKind(t.Kind):
		if !slices.Contains(t.Channels, DeliveryEmail) {
			return nil, "the target no longer delivers by email", nil
		}
		addr, refused, err := recheckRecipient(ctx, q, t, recipientID, componentID)
		if err != nil || refused != "" {
			return nil, refused, err
		}
		to = []string{addr}
	default:
		return nil, "the target is not an email destination", nil
	}
	relay, refused, err := p.mailRelay(ctx, q)
	if err != nil || refused != "" {
		return nil, refused, err
	}
	return &EmailSend{Relay: *relay, To: to, Date: at}, "", nil
}

// personalEmailSend reads a personal rule's email row's send (#1353): its
// owner, judged again as any person's row is (still active, still reading
// the component), to their directory email. A subscription deleted since is
// not sent.
func (p *PG) personalEmailSend(ctx context.Context, q querier, ruleID, recipientID *string, componentID string, at time.Time) (*EmailSend, string, error) {
	if ruleID == nil || recipientID == nil {
		return nil, "the subscription was deleted", nil
	}
	var owner string
	err := q.QueryRow(ctx, `select coalesce(owner_principal_id::text, '') from action_rule where id = $1::uuid`, *ruleID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != *recipientID) {
		return nil, "the subscription was deleted", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("storage: personal rule %s: %w", *ruleID, err)
	}
	addr, refused, err := recheckRecipient(ctx, q, &NotificationTarget{Kind: TargetPrincipal, PrincipalID: owner}, recipientID, componentID)
	if err != nil || refused != "" {
		return nil, refused, err
	}
	relay, refused, err := p.mailRelay(ctx, q)
	if err != nil || refused != "" {
		return nil, refused, err
	}
	return &EmailSend{Relay: *relay, To: []string{addr}, Date: at}, "", nil
}

// recheckRecipient judges a person's email row again at send, as the queue
// judged them (ADR-1341), and resolves their directory email: they are still
// an active human, still a member of the group the row came through (or
// still the person the target names), and their component read scope, as it
// stands now, still admits the alarm's component.
func recheckRecipient(ctx context.Context, q querier, t *NotificationTarget, recipientID *string, componentID string) (string, string, error) {
	if recipientID == nil {
		return "", "the recipient was deleted", nil
	}
	var email string
	var active, member bool
	err := q.QueryRow(ctx, `select coalesce(h.email, ''), pr.active and pr.archived_at is null,
			$2::uuid is null or exists (select 1 from principal_group_member m where m.group_id = $2::uuid and m.principal_id = pr.id)
		from principal pr join human h on h.principal_id = pr.id where pr.id = $1::uuid`,
		*recipientID, nullIfEmpty(t.PrincipalGroupID)).Scan(&email, &active, &member)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "the recipient was deleted", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("storage: recheck recipient: %w", err)
	}
	switch {
	case !active:
		return "", "the recipient is no longer active", nil
	case !member:
		// The group is not named: a reason is read by whoever reads the
		// alarm, and the group directory is admin-tier.
		return "", "the recipient is no longer a member of the target's group", nil
	case t.Kind == TargetPrincipal && t.PrincipalID != *recipientID:
		return "", "the target no longer names the recipient", nil
	}
	readers, err := readersOf(ctx, q, componentID, []string{*recipientID})
	if err != nil {
		return "", "", err
	}
	if !readers[*recipientID] {
		return "", "the recipient no longer reads the alarm's component", nil
	}
	if strings.TrimSpace(email) == "" {
		return "", "the recipient has no directory email", nil
	}
	addr, err := mail.CheckAddress(email)
	if err != nil {
		return "", "the recipient's directory email is not an email address", nil
	}
	return addr, "", nil
}

// mailRelay reads the install's relay from the notification settings, its
// bound secret unsealed; a relay that is not configured, or whose secret is
// gone, is the plain reason a send is refused.
func (p *PG) mailRelay(ctx context.Context, q querier) (*MailRelay, string, error) {
	s, err := p.platformSettings(ctx, q)
	if err != nil {
		return nil, "", err
	}
	n := s.Notification
	if n.MailRelay == "" {
		return nil, "no mail relay is configured (the notification settings' mail_relay)", nil
	}
	if n.MailFrom == "" {
		return nil, "no From address is configured (the notification settings' mail_from)", nil
	}
	r := &MailRelay{Addr: n.MailRelay, Security: n.MailSecurity, From: n.MailFrom}
	if n.MailSecret == "" {
		return r, "", nil
	}
	id, err := platformBasicAuthSecret(ctx, q, n.MailSecret)
	if errors.Is(err, ErrSecretNotFound) {
		return nil, "the mail relay's secret is not a platform basic-auth secret", nil
	}
	if err != nil {
		return nil, "", err
	}
	fields, err := p.unsealSecretByID(ctx, q, id)
	if err != nil || fields["username"] == "" {
		return nil, "the mail relay's secret no longer unseals", nil
	}
	r.Username, r.Password = fields["username"], fields["password"]
	return r, "", nil
}

// platformBasicAuthSecret finds a platform basic-auth secret by its bare
// name.
func platformBasicAuthSecret(ctx context.Context, q querier, name string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `select s.id::text from secret s join secret_type st on st.id = s.secret_type
		where s.owner_kind = 'platform' and s.name = $1 and st.name = 'basic-auth'`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrSecretNotFound
	}
	if err != nil {
		return "", fmt.Errorf("storage: mail relay secret %q: %w", name, err)
	}
	return id, nil
}

// MailRelay reads the install's relay for a :test send, its secret unsealed;
// refused is the plain reason there is no relay to send through. The
// platform's own read: it never leaves the process.
func (p *PG) MailRelay(ctx context.Context) (*MailRelay, string, error) {
	return p.mailRelay(ctx, p.pool)
}

// checkMailSecret judges binding a secret to the mail relay (the notification
// settings' mail_secret), as binding one to a target is judged: a PLATFORM
// basic-auth secret, by bare name, that the caller may reveal (SecretUse),
// since every send hands it to whatever mail_relay names. A secret outside
// the caller's view is ErrSecretNotFound, one it reads but may not reveal
// ErrSecretForbidden, one of another kind ErrMailSecretInvalid.
func (p *PG) checkMailSecret(ctx context.Context, q querier, ref string, use SecretUse) error {
	if ref == "" {
		return nil
	}
	if isUUID(ref) || strings.Contains(ref, "/") {
		return fmt.Errorf("%w: mail_secret names a platform secret by its bare name", ErrMailSecretInvalid)
	}
	row, _, err := p.secretRowForAction(ctx, q, ref, use.Read, use.Reveal, use.CanAdmin, true)
	if err != nil {
		return err
	}
	if row.ownerKind != "platform" {
		return fmt.Errorf("%w: secret %q is not a platform secret", ErrMailSecretInvalid, ref)
	}
	if _, err := platformBasicAuthSecret(ctx, q, ref); errors.Is(err, ErrSecretNotFound) {
		return fmt.Errorf("%w: secret %q is not a basic-auth secret", ErrMailSecretInvalid, ref)
	} else if err != nil {
		return err
	}
	return nil
}

// ErrMailSecretInvalid is a mail_secret that is not a platform basic-auth
// secret.
var ErrMailSecretInvalid = errors.New("storage: the mail relay's secret is invalid")
