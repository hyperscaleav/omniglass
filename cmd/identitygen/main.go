// Command identitygen renders the identity-shape reference (docs/src/generated/
// identity.json) from the declaration in internal/storage, the identity arm of the
// generation pipeline. Every table's shape and every exception's reason live in one
// place; the guard in internal/storage checks the declaration against the live
// schema, and the docs render this rather than restating it, so the two cannot drift.
// It also writes the console's copy of the single sign-on refusal codes
// (web/src/lib/sso_errors.gen.ts) from internal/sso, so the console's messages
// are keyed by exactly the codes the callback redirects with (#2362).
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/sso"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

const (
	dst       = "docs/src/generated/identity.json"
	ssoErrDst = "web/src/lib/sso_errors.gen.ts"
)

func main() {
	facts, err := storage.IdentityShapesJSON()
	if err != nil {
		log.Fatalf("identitygen: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Fatalf("identitygen: mkdir: %v", err)
	}
	if err := os.WriteFile(dst, append(facts, '\n'), 0o644); err != nil {
		log.Fatalf("identitygen: write %s: %v", dst, err)
	}
	log.Printf("wrote identity-shape reference to %s", dst)
	if err := os.WriteFile(ssoErrDst, sso.ErrorCodesTS(), 0o644); err != nil {
		log.Fatalf("identitygen: write %s: %v", ssoErrDst, err)
	}
	log.Printf("wrote the single sign-on refusal codes to %s", ssoErrDst)
}
