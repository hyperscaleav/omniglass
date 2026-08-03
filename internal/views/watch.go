package views

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// The change detector behind the :watch seam, v1 of the live contract: the
// stream carries only "changed" notifications and the client re-runs the view,
// so ViewResult stays the only data shape. The loop is pure (the ticker and
// the query are injected), which is what lets the #430 bus feeds replace it
// later under the same client contract.

// ResultHash returns a stable content hash of a result's rows. Two runs over
// identical data hash identically (cells are positional and JSON encoding is
// deterministic for the cell types views emit), and any cell delta moves the
// hash, which is the whole signal the detector needs.
func ResultHash(rows [][]any) (string, error) {
	b, err := json.Marshal(rows)
	if err != nil {
		return "", fmt.Errorf("views: hash rows: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
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
