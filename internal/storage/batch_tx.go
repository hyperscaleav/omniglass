package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// batchTx is an ingest batch's transaction carrying what the batch resolves
// once and every lane reuses (#1274): each owner's primary key, and the
// transaction's clock. LandBatch wraps its transaction in one and hands it to
// the lock pass and every lane writer, which take a pgx.Tx and so need no
// change; ownerArcValue and dbNow recognize it and answer from it.
//
// Both memos answer what the database would: now() is the transaction's start
// time, the same for every statement in it, and an owner's primary key does
// not change within the batch's transaction. A savepoint opened inside a lane
// (tx.Begin) is a plain pgx.Tx and resolves afresh, which is only slower.
type batchTx struct {
	pgx.Tx
	arcs map[string]string
	now  time.Time
	// targeted is the batch's command-targeted property names when its caller
	// carried them (#1299), else as the batch first read them; nil reads them
	// in the transaction.
	targeted map[string]bool
	// writers is each key's resolved writer class, per lane, once the batch
	// has read it (keyWriters), so a tick of many task groups reads a key's
	// calc claims once however many groups carry it (#1582).
	writers map[writeLane]map[string]keyWriterOf
	// bindings is each node writer's component, by the name it named, once
	// resolved (nodeBinding).
	bindings map[string]string
	// The locks the batch took up front for every lane (lockGroups, #1928),
	// so a lane writer's own take of one is skipped rather than repeated:
	// ruleKeys is every sample key whose enabled event rules it read FOR
	// SHARE, rules those rules by key, marked each writer rule's calc marks
	// (by the rule a write excepts), and series each property series lock.
	// Only the batch's own transaction records them, never a savepoint, so a
	// rolled-back savepoint never leaves a lock recorded that it released.
	ruleKeys map[string]bool
	rules    map[string][]EventRule
	marked   map[string]*calcMarks
	series   map[string]bool
}

func newBatchTx(tx pgx.Tx) *batchTx {
	return &batchTx{Tx: tx, arcs: map[string]string{}, writers: map[writeLane]map[string]keyWriterOf{}, bindings: map[string]string{},
		ruleKeys: map[string]bool{}, rules: map[string][]EventRule{}, marked: map[string]*calcMarks{}, series: map[string]bool{}}
}

// savepoint is sp, a savepoint opened in b, answering from b's memos: what
// the batch resolved holds inside it, and what the savepoint resolves holds
// for the batch (a read a rolled-back savepoint made is still the
// transaction's answer).
func (b *batchTx) savepoint(sp pgx.Tx) *batchTx {
	return &batchTx{Tx: sp, arcs: b.arcs, now: b.now, targeted: b.targeted, writers: b.writers, bindings: b.bindings,
		ruleKeys: b.ruleKeys, rules: b.rules, marked: b.marked, series: b.series}
}

// heldRules is the enabled event rules reading keys, when the batch already
// read every one of them FOR SHARE.
func (b *batchTx) heldRules(keys []string) (map[string][]EventRule, bool) {
	byKey := map[string][]EventRule{}
	for _, k := range keys {
		if !b.ruleKeys[k] {
			return nil, false
		}
		if rs := b.rules[k]; len(rs) > 0 {
			byKey[k] = rs
		}
	}
	return byKey, true
}

// heldMarks reports whether the batch already marked every sample in marks
// for the writer rule exceptRule.
func (b *batchTx) heldMarks(marks *calcMarks, exceptRule string) bool {
	held := b.marked[exceptRule]
	if held == nil {
		return false
	}
	for owner, ks := range marks.keys {
		for k := range ks {
			if !held.keys[owner][k] {
				return false
			}
		}
	}
	return true
}

// heldSeries reports whether the batch already holds every property series
// lock in keys.
func (b *batchTx) heldSeries(keys []string) bool {
	for _, k := range keys {
		if !b.series[k] {
			return false
		}
	}
	return true
}

// arc resolves an owner's primary key once per batch.
func (b *batchTx) arc(ctx context.Context, p *PG, ownerKind, ownerRef string) (string, error) {
	k := ownerKind + "\x00" + ownerRef
	if v, ok := b.arcs[k]; ok {
		return v, nil
	}
	v, err := p.ownerArcValue(ctx, b.Tx, ownerKind, ownerRef)
	if err != nil {
		return "", err
	}
	b.arcs[k] = v
	return v, nil
}

// clock reads the transaction's now once per batch.
func (b *batchTx) clock(ctx context.Context) (time.Time, error) {
	if b.now.IsZero() {
		now, err := dbNow(ctx, b.Tx)
		if err != nil {
			return time.Time{}, err
		}
		b.now = now
	}
	return b.now, nil
}
