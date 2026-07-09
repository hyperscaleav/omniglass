package auth

import (
	"errors"
	"strings"
	"testing"
)

// TestValidatePassword covers the policy gate (issue #151): a length floor, a
// common-password denylist, and no username inside the password. Pure logic, no I/O.
func TestValidatePassword(t *testing.T) {
	// A long, uncommon password that does not contain the username is accepted.
	if err := ValidatePassword("Tr0ubadour-x9qz7w", "jordan"); err != nil {
		t.Fatalf("strong password rejected: %v", err)
	}
	// Shorter than the floor.
	if err := ValidatePassword("short-pw-1", ""); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("short password: want ErrPasswordTooShort, got %v", err)
	}
	// A denylisted common password (long enough to clear the length gate).
	if err := ValidatePassword("administrator", ""); !errors.Is(err, ErrPasswordCommon) {
		t.Fatalf("common password: want ErrPasswordCommon, got %v", err)
	}
	// The denylist match is case-insensitive.
	if err := ValidatePassword("Password1234", ""); !errors.Is(err, ErrPasswordCommon) {
		t.Fatalf("common password (mixed case): want ErrPasswordCommon, got %v", err)
	}
	// Contains the username (case-insensitive).
	if err := ValidatePassword("my-Jordan-pass-9", "jordan"); !errors.Is(err, ErrPasswordContainsIdentifier) {
		t.Fatalf("username in password: want ErrPasswordContainsIdentifier, got %v", err)
	}
	// A very short username is not checked for containment (would reject too much).
	if err := ValidatePassword("aviation-safety-plan", "av"); err != nil {
		t.Fatalf("short username should not trip containment: %v", err)
	}
	// Longer than the cap.
	if err := ValidatePassword(strings.Repeat("a", MaxPasswordLength+1), ""); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("overlong password: want ErrPasswordTooLong, got %v", err)
	}
}
