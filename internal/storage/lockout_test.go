package storage

import (
	"testing"
	"time"
)

func TestNextLockout(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	// Below the threshold: just increment, no lock.
	for count := 0; count < loginFailureThreshold-1; count++ {
		d := nextLockout(count, now)
		if d.lockedUntil != nil {
			t.Errorf("count=%d: locked early (until %v)", count, d.lockedUntil)
		}
		if d.count != count+1 {
			t.Errorf("count=%d: next count = %d, want %d", count, d.count, count+1)
		}
	}

	// The threshold-th failure trips the lock: counter resets, expiry is now+window.
	d := nextLockout(loginFailureThreshold-1, now)
	if d.lockedUntil == nil {
		t.Fatalf("count=%d: expected a lock, got none", loginFailureThreshold-1)
	}
	if !d.lockedUntil.Equal(now.Add(loginLockoutWindow)) {
		t.Errorf("lockedUntil = %v, want %v", d.lockedUntil, now.Add(loginLockoutWindow))
	}
	if d.count != 0 {
		t.Errorf("counter after lock = %d, want 0", d.count)
	}
}

func TestIsLocked(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	if isLocked(nil, now) {
		t.Error("nil expiry should not be locked")
	}
	if isLocked(&past, now) {
		t.Error("a past expiry should not be locked")
	}
	if !isLocked(&future, now) {
		t.Error("a future expiry should be locked")
	}
}
