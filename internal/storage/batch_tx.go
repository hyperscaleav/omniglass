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
	// carried them (#1299); nil reads them in the transaction.
	targeted map[string]bool
}

func newBatchTx(tx pgx.Tx) *batchTx {
	return &batchTx{Tx: tx, arcs: map[string]string{}}
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
