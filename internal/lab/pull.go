package lab

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// pullBackoff is how long a rate limited pull waits before each retry: a registry
// that counts anonymous pulls per address (Docker Hub's 429, #2020) forgives
// within seconds, and a nightly run fans out a hundred labs at once. It is a
// variable so a test does not wait it out.
var pullBackoff = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}

// ErrRegistryLimit marks a pull a registry refused for its rate limit even
// after the retries: an infrastructure failure, not a fault in the lab or the
// demo that asked for it.
var ErrRegistryLimit = errors.New("registry rate limit")

// statusCode429 is the status code written as a status, never bare digits: a
// digest, a port or a byte count can contain 429 without being a rate limit.
var statusCode429 = regexp.MustCompile(`\bstatus(?: code)?[: ]+429\b`)

// registryLimited is whether a pull error is the registry's rate limit: HTTP
// 429, which the daemon relays as "429 Too Many Requests" or "toomanyrequests",
// or as a status code of 429.
func registryLimited(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "429 too many requests") || strings.Contains(m, "toomanyrequests") ||
		strings.Contains(m, "too many requests") || statusCode429.MatchString(m)
}

// pullImage pulls ref, retrying a rate limited pull with backoff. Once the
// retries run out it names the failure as a registry limit (errors.Is
// ErrRegistryLimit) so a caller can report it as infrastructure. Any other
// error returns at once.
func pullImage(ctx context.Context, ref string, pull func(context.Context, string) error, say func(string)) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = pull(ctx, ref); err == nil || !registryLimited(err) {
			return err
		}
		if attempt >= len(pullBackoff) {
			return fmt.Errorf("%w: %s is rate limited by its registry (HTTP 429) after %d retries: %w", ErrRegistryLimit, ref, len(pullBackoff), err)
		}
		wait := pullBackoff[attempt]
		say(fmt.Sprintf("%s was rate limited by its registry; retrying in %s (%d of %d)", ref, wait, attempt+1, len(pullBackoff)))
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
