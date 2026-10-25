package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// One source flooding the webhook route spends its own budget, never the
// one every sender shares (#1024): a sender at one post a second is admitted
// every time, while a stranger posts a thousand a second beside it.
func TestOneSourcesFloodNeverStarvesAnother(t *testing.T) {
	rc := newWebhookReceiver(storage.UnimplementedGateway{}, nil)
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	admitted := 0
	for sec := 0; sec < 60; sec++ {
		at := start.Add(time.Duration(sec) * time.Second)
		for i := 0; i < 1000; i++ {
			if ok, _ := rc.admit("203.0.113.9", at.Add(time.Duration(i)*time.Millisecond)); ok {
				rc.releaseSource("203.0.113.9")
			}
		}
		if ok, _ := rc.admit("198.51.100.4", at.Add(999*time.Millisecond)); ok {
			admitted++
			rc.releaseSource("198.51.100.4")
		}
	}
	if admitted != 60 {
		t.Fatalf("the quiet sender was admitted %d of 60 times beside the flood, want every time", admitted)
	}
}

// unclaimedStore is a store whose inbox holds what the table would: at most
// its bound.
type unclaimedStore struct {
	storage.UnimplementedGateway
	rows []storage.UnclaimedSource
}

func (s *unclaimedStore) RecordUnclaimedSources(_ context.Context, us []storage.UnclaimedSource) error {
	s.rows = append(us, s.rows...)
	if len(s.rows) > storage.UnclaimedBound {
		s.rows = s.rows[:storage.UnclaimedBound]
	}
	return nil
}

func (s *unclaimedStore) ListUnclaimedSources(context.Context) ([]storage.UnclaimedSource, error) {
	return s.rows, nil
}

// The receiver's memory of the inbox's sources is the table's own rows,
// bounded as the table is, however many distinct strangers post.
func TestTheInboxMirrorStaysWithinTheTablesBound(t *testing.T) {
	st := &unclaimedStore{}
	rc := newWebhookReceiver(st, nil)
	at := time.Now()
	for tick := 0; tick < 20; tick++ {
		for i := 0; i < 300; i++ {
			rc.tally.Unclaimed(fmt.Sprintf("10.%d.%d.%d", tick, i/250, i%250), []byte("x"), at)
		}
		rc.flush()
	}
	rc.mu.Lock()
	n := len(rc.unclaimed)
	rc.mu.Unlock()
	if n > storage.UnclaimedBound {
		t.Fatalf("the receiver holds %d sources, past the inbox's bound of %d", n, storage.UnclaimedBound)
	}
}

// Bodies being read hold memory, so the listener budgets them by their
// declared length (a chunked post counts as the cap): past the budget a new
// post waits, however its sender drips it, and each read's deadline follows
// its size.
func TestBodiesBeingReadAreBudgeted(t *testing.T) {
	rc := newWebhookReceiver(storage.UnimplementedGateway{}, nil)
	held := 0
	for rc.reserveReading(declaredSize("")) {
		held++
	}
	if want := webhookReadingBytes / webhookBodyLimit; held != want {
		t.Fatalf("held %d unsized bodies at once, want the budget's %d", held, want)
	}
	if rc.reserveReading(declaredSize("2048")) {
		t.Fatal("a body past the budget was admitted")
	}
	rc.releaseReading(webhookBodyLimit)
	if !rc.reserveReading(declaredSize("2048")) {
		t.Fatal("a released budget did not readmit a small body")
	}
	if got := declaredSize("999999999"); got != webhookBodyLimit {
		t.Fatalf("an over-cap length counts as %d, want the cap", got)
	}
	if d := readDeadline(2048); d > 3*time.Second {
		t.Fatalf("a 2 KiB body may take %s to arrive, want about two seconds", d)
	}
	if d := readDeadline(webhookBodyLimit); d != webhookReadDeadline {
		t.Fatalf("a 1 MiB body's deadline = %s, want the ceiling", d)
	}
}
