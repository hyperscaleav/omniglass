package lab

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A nightly demo run fans out about a hundred labs, each pulling postgres:18
// anonymously, so Docker Hub answers some with a 429 (#2020). The pull retries
// with backoff; once the retries run out it names a registry limit, so the
// caller can report infrastructure rather than a broken demo.

func fastBackoff(t *testing.T) {
	t.Helper()
	old := pullBackoff
	pullBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { pullBackoff = old })
}

var errTooMany = errors.New(`unknown: failed to resolve reference "docker.io/library/postgres:18": unexpected status from HEAD request to https://registry-1.docker.io/v2/library/postgres/manifests/18: 429 Too Many Requests`)

func TestPullImageRetriesARateLimitThenSucceeds(t *testing.T) {
	fastBackoff(t)
	calls := 0
	var said []string
	err := pullImage(context.Background(), "postgres:18", func(context.Context, string) error {
		calls++
		if calls < 3 {
			return errTooMany
		}
		return nil
	}, func(m string) { said = append(said, m) })
	if err != nil || calls != 3 {
		t.Fatalf("err %v after %d calls, want success on the third", err, calls)
	}
	if len(said) != 2 || !strings.Contains(said[0], "rate limited by its registry; retrying") {
		t.Fatalf("the retries were not announced: %q", said)
	}
}

func TestPullImageNamesARegistryLimitOnceRetriesRunOut(t *testing.T) {
	fastBackoff(t)
	calls := 0
	err := pullImage(context.Background(), "postgres:18", func(context.Context, string) error { calls++; return errTooMany }, func(string) {})
	if !errors.Is(err, ErrRegistryLimit) {
		t.Fatalf("err %v, want it to be a registry limit", err)
	}
	if calls != 4 {
		t.Fatalf("%d pulls, want 1 and 3 retries", calls)
	}
	if !strings.Contains(err.Error(), "postgres:18") || !strings.Contains(err.Error(), "429") {
		t.Fatalf("the error does not name the image and the limit: %v", err)
	}
}

func TestPullImageDoesNotRetryAnotherError(t *testing.T) {
	fastBackoff(t)
	calls := 0
	boom := errors.New("manifest unknown")
	err := pullImage(context.Background(), "postgres:18", func(context.Context, string) error { calls++; return boom }, func(string) {})
	if !errors.Is(err, boom) || errors.Is(err, ErrRegistryLimit) || calls != 1 {
		t.Fatalf("err %v after %d calls, want the original error at once", err, calls)
	}
}

func TestPullImageStopsWaitingWhenTheContextEnds(t *testing.T) {
	old := pullBackoff
	pullBackoff = []time.Duration{time.Hour}
	t.Cleanup(func() { pullBackoff = old })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := pullImage(ctx, "postgres:18", func(context.Context, string) error { return errTooMany }, func(string) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want the context's", err)
	}
}
