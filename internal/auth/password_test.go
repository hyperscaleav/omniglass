package auth

import "testing"

func TestHashVerifyPassword(t *testing.T) {
	enc, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ok, err := VerifyPassword("correct horse battery staple", enc)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("expected the right password to verify")
	}

	bad, err := VerifyPassword("wrong password", enc)
	if err != nil {
		t.Fatalf("verify wrong: %v", err)
	}
	if bad {
		t.Fatal("expected a wrong password to fail")
	}
}

func TestHashUsesADistinctSaltEachTime(t *testing.T) {
	a, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("expected a random salt to make two hashes of the same password differ")
	}
	// both must still verify
	for _, enc := range []string{a, b} {
		ok, err := VerifyPassword("same", enc)
		if err != nil || !ok {
			t.Fatalf("expected both encodings to verify, ok=%v err=%v", ok, err)
		}
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	for _, bad := range []string{
		"",
		"not-a-phc-string",
		"$argon2id$v=19$m=1$onlyfour$parts",
		"$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA", // wrong algorithm
	} {
		if _, err := VerifyPassword("x", bad); err == nil {
			t.Errorf("expected an error for malformed hash %q", bad)
		}
	}
}
