package storage

import "time"

// Login brute-force policy: after loginFailureThreshold consecutive failed
// password attempts on a real account, lock it for loginLockoutWindow. A correct
// password during the lock is still refused until the window passes; a successful
// login (before the lock) clears the counter. Per-username only for this slice;
// per-IP throttling and a configurable threshold are deferred (see identity-access).
const (
	loginFailureThreshold = 5
	loginLockoutWindow    = 15 * time.Minute
)

// lockoutDecision is the counter state to persist after a failed login attempt.
type lockoutDecision struct {
	// count is the new consecutive-failure count to store (reset to 0 when the
	// attempt trips the lock, so the account starts fresh after the window).
	count int
	// lockedUntil, when non-nil, is the new lock expiry to store; nil leaves the
	// account unlocked and only bumps the counter.
	lockedUntil *time.Time
}

// nextLockout is the pure decision for a single failed attempt: given the current
// consecutive-failure count and the clock, it returns the counter state to persist.
// The threshold-th failure trips the lock (count resets, lockedUntil set); earlier
// failures just increment. Kept pure so the policy is unit-testable without a DB.
func nextLockout(count int, now time.Time) lockoutDecision {
	next := count + 1
	if next >= loginFailureThreshold {
		until := now.Add(loginLockoutWindow)
		return lockoutDecision{count: 0, lockedUntil: &until}
	}
	return lockoutDecision{count: next}
}

// isLocked reports whether a lock expiry is set and still in the future.
func isLocked(lockedUntil *time.Time, now time.Time) bool {
	return lockedUntil != nil && lockedUntil.After(now)
}
