package auth

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The password policy (issue #151): a single pure gate every set-path calls (create
// a user, self-service change, CLI set-password, bootstrap). It enforces a length
// floor, a common-password denylist, and that the password does not contain the
// username. Deliberately no character-class composition rules (NIST 800-63B favors
// length and a blocklist over composition).
const (
	// MinPasswordLength is the floor (in characters, not bytes) for any password.
	MinPasswordLength = 12
	// MaxPasswordLength caps the input. argon2 hashes any length; this bounds abuse.
	MaxPasswordLength = 256
	// minIdentifierLen is the shortest username checked for containment; a one or
	// two character username would reject far too many otherwise fine passwords.
	minIdentifierLen = 3
)

// The policy sentinels. The API maps each to 422 with a specific message; the CLI
// surfaces them; the console mirrors the length and identifier rules inline.
var (
	ErrPasswordTooShort           = errors.New("auth: password is too short")
	ErrPasswordTooLong            = errors.New("auth: password is too long")
	ErrPasswordCommon             = errors.New("auth: password is a common password")
	ErrPasswordContainsIdentifier = errors.New("auth: password contains the username")
)

//go:embed common_passwords.txt
var commonPasswordsRaw string

// commonPasswords is the lowercased denylist, parsed once at package init.
var commonPasswords = loadCommonPasswords(commonPasswordsRaw)

func loadCommonPasswords(raw string) map[string]struct{} {
	set := make(map[string]struct{})
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[strings.ToLower(line)] = struct{}{}
	}
	return set
}

// ValidatePassword reports whether a password satisfies the policy, returning a
// typed sentinel for the first rule it fails (nil if it passes). username may be
// empty (e.g. a context with no identifier to compare against). Pure: no I/O, so it
// is trivially unit-testable and safe to call on every set-path.
func ValidatePassword(password, username string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if utf8.RuneCountInString(password) > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	lower := strings.ToLower(password)
	if u := strings.ToLower(strings.TrimSpace(username)); len(u) >= minIdentifierLen && strings.Contains(lower, u) {
		return ErrPasswordContainsIdentifier
	}
	if _, ok := commonPasswords[lower]; ok {
		return ErrPasswordCommon
	}
	return nil
}

// PasswordRequirements is a human-readable description of the policy, for the API
// docs, the CLI help, and the console hint. Kept in sync with ValidatePassword.
func PasswordRequirements() string {
	return fmt.Sprintf("at least %d characters, not a common password, and not containing the username", MinPasswordLength)
}
