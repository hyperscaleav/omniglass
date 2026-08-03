package views

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// The change detector behind the :watch seam, v1 of the live contract: the
// stream carries only "changed" notifications and the client re-runs the view,
// so ViewResult stays the only data shape. The loop is pure (the ticker and
// the query are injected), which is what lets the #430 bus feeds replace it
// later under the same client contract.

// ResultHash returns a stable content hash of a result's rows. Two runs over
// identical data hash identically and any cell delta moves the hash, which is
// the whole signal the detector needs.
//
// It cannot fail, deliberately: the hash is a change signal, not a
// serialization, and a JSON encoder would error on values Postgres can legally
// hold (a NaN or an infinity in a double column, a year outside 0..9999 in a
// timestamptz). An error there would kill the stream on every re-run, so cells
// are folded in through a total encoding instead. Times normalize to UTC
// nanoseconds so an equal instant in a different location hashes equally, and
// the field and row separators are bytes no rendered value contains.
func ResultHash(rows [][]any) string {
	h := sha256.New()
	for _, row := range rows {
		for _, cell := range row {
			switch v := cell.(type) {
			case nil:
				h.Write([]byte{0})
			case time.Time:
				fmt.Fprintf(h, "%d", v.UTC().UnixNano())
			default:
				fmt.Fprintf(h, "%v", v)
			}
			h.Write([]byte{0x1f})
		}
		h.Write([]byte{0x1e})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Watch is the notify-then-refetch detector loop. The first run notifies
// immediately (the connect baseline, so a client that missed changes while
// disconnected always refetches once on connect or reconnect); every tick
// re-runs and notifies only when the hash moved. Returns nil when ctx ends
// (the disconnect path) and the first run or notify error otherwise, so a
// broken stream fails loudly rather than stalling silent.
func Watch(ctx context.Context, tick <-chan time.Time, run func(context.Context) (string, error), notify func(hash string) error) error {
	last, err := run(ctx)
	if err != nil {
		return err
	}
	if err := notify(last); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			h, err := run(ctx)
			if err != nil {
				return err
			}
			if h == last {
				continue
			}
			last = h
			if err := notify(h); err != nil {
				return err
			}
		}
	}
}
