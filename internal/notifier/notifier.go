// Package notifier is the server tick that delivers notifications (#618,
// ADR-0618): every TickEvery it drains the due rows the alarm transitions
// queued, one row per transaction, each sent through the egress primitive
// while its row lock is held, so replicas drain one queue (FOR UPDATE SKIP
// LOCKED) and a restart resumes where the last pass stopped. A webhook row
// goes through the egress HTTP client; an email row (#1317) through the mail
// relay, over the egress primitive's same dial. It is the shape
// ADR-0148 chose for calc rules, not the CDC-to-JetStream sender the
// alarms-and-actions page designs: the notification must commit with the
// alarm, and the CDC publisher (#430) is unbuilt; when it lands the drain can
// move behind it without changing the row. The same tick ends expired alarm
// snoozes (#1318, ADR-1318) until the time primitive (#419) lands.
package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/mail"
	"github.com/hyperscaleav/omniglass/internal/notify"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The tick: how often it drains, the most rows one pass sends (each its own
// transaction), and the deadline on a pass. A backlog larger than a pass
// drains over successive ticks, oldest due first.
const (
	TickEvery   = 2 * time.Second
	PassBatch   = 100
	PassTimeout = 60 * time.Second
)

// Store is the slice of the Storage Gateway the tick uses.
type Store interface {
	DeliverNext(ctx context.Context, send storage.SendFunc) (bool, error)
	EndExpiredSnoozes(ctx context.Context, max int) (int, error)
	EgressPolicy(ctx context.Context) (egress.Policy, error)
}

// Notifier drains the notification queue.
type Notifier struct {
	store Store
	opts  egress.Options
	now   func() time.Time
}

// New returns a notifier over store; opts tunes its outbound client (a
// test's certificate roots).
func New(store Store, opts egress.Options) *Notifier {
	return &Notifier{store: store, opts: opts, now: time.Now}
}

// Pass ends the snoozes past their deadline, then delivers up to max due
// notifications and reports how many it took (sent or not). The sweep runs
// here, on the drain's tick, until the time primitive (#419) exists
// (ADR-1318), and first, so a condition announced again at the end of its
// snooze is delivered in the same pass. The egress policy is read once per
// pass; an allow list that does not parse fails every send of the pass with
// the reason.
func (n *Notifier) Pass(ctx context.Context, max int) (int, error) {
	// A sweep that fails is logged and the drain still runs: a snooze left
	// past its deadline only waits for the next tick, while a stalled drain
	// would hold back every other notification.
	if _, err := n.store.EndExpiredSnoozes(ctx, max); err != nil && ctx.Err() == nil {
		slog.Warn("the end-of-snooze sweep failed", "facility", "notify", "error", err.Error())
	}
	pol, perr := n.store.EgressPolicy(ctx)
	client := egress.New(pol, n.opts)
	mailer := mail.New(pol, n.opts)
	send := func(ctx context.Context, s storage.NotificationSend) storage.SendResult {
		if perr != nil {
			return storage.SendResult{Reason: strings.TrimPrefix(perr.Error(), "egress: ")}
		}
		if s.Channel == storage.DeliveryEmail {
			return SendEmail(ctx, mailer, s.ID, s.Subject, s.Text, s.Email)
		}
		req, err := notify.Build(s.Target.URL, notify.Envelope{
			ID: s.ID, Type: s.Type, Transition: s.Transition, Subject: s.Subject, Text: s.Text,
			AlarmID: s.AlarmID, Facts: s.Facts,
		}, notify.Credential{HMACKey: s.Target.HMACKey, BearerToken: s.Target.BearerToken}, n.now())
		if err != nil {
			return storage.SendResult{Reason: err.Error()}
		}
		res := client.Post(ctx, req)
		return storage.SendResult{OK: res.OK, Status: res.Status, Reason: res.Reason}
	}
	taken := 0
	for taken < max && admits(ctx, n.now()) {
		// The row's send and its record run detached from the pass's
		// deadline and from shutdown, bounded by the egress timeout instead:
		// a cancel landing mid-send would roll the record back after the
		// receiver had the message, and the next pass would send it again.
		// The pass stops taking rows instead (admits).
		row, done := context.WithTimeout(context.WithoutCancel(ctx), RowBound)
		ok, err := n.store.DeliverNext(row, send)
		done()
		if err != nil {
			return taken, err
		}
		if !ok {
			break
		}
		taken++
	}
	return taken, nil
}

// SendEmail builds one email row's message and hands it to the relay: the
// same row builds the same bytes, so a retry resends what the first attempt
// sent. A message that cannot be built (an address that does not parse) is
// the send's reason.
func SendEmail(ctx context.Context, mailer *mail.Client, id, subject, text string, e *storage.EmailSend) storage.SendResult {
	if e == nil {
		return storage.SendResult{Reason: "the email has no send"}
	}
	from, err := mail.CheckAddress(e.Relay.From)
	if err != nil {
		return storage.SendResult{Reason: "mail_from is not an email address"}
	}
	raw, err := mail.Build(mail.Message{ID: id, From: e.Relay.From, To: e.To, Subject: subject, Text: text, Date: e.Date})
	if err != nil {
		return storage.SendResult{Reason: err.Error()}
	}
	res := mailer.Send(ctx, mail.Relay{Addr: e.Relay.Addr, Security: e.Relay.Security, Username: e.Relay.Username, Password: e.Relay.Password},
		from, e.To, raw)
	return storage.SendResult{OK: res.OK, Status: res.Status, Reason: res.Reason}
}

// RowBound bounds one row's take, send and record: the egress client's own
// bound on a send, with room for the row's statements.
const RowBound = egress.TotalTimeout + 10*time.Second

// admits reports whether a pass may take another row: it has not been
// cancelled, and a row taken now would finish before its deadline.
func admits(ctx context.Context, now time.Time) bool {
	if ctx.Err() != nil {
		return false
	}
	if dl, ok := ctx.Deadline(); ok && dl.Sub(now) < RowBound {
		return false
	}
	return true
}

// Run drains on every tick until ctx ends. A panicking pass is recovered and
// logged, so one bad pass never ends the tick.
func (n *Notifier) Run(ctx context.Context) {
	t := time.NewTicker(TickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		n.pass(ctx)
	}
}

func (n *Notifier) pass(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("notification tick panicked", "facility", "notify", "panic", fmt.Sprint(r))
		}
	}()
	p, done := context.WithTimeout(ctx, PassTimeout)
	defer done()
	taken, err := n.Pass(p, PassBatch)
	if err != nil && ctx.Err() == nil {
		slog.Warn("notification tick failed", "facility", "notify", "error", err.Error())
	}
	if taken > 0 {
		slog.Info("notification tick", "facility", "notify", "attempted", taken)
	}
}
