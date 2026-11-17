package assistant

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hyperscaleav/omniglass/internal/redact"
)

// Credential is a secret the person hands omnibot (#2112): what it is called,
// its type, and where it lives. Its value is never part of it.
type Credential struct {
	Name string
	Type string
	// OwnerKind is platform, location or component; Owner names the location
	// or component and is empty for platform.
	OwnerKind string
	Owner     string
}

// CredentialField is one value a credential's type asks the person for.
// Secret is whether it is sealed (read without echo) or plain.
type CredentialField struct {
	Name   string
	Secret bool
}

// CredentialFields is what the person types for a credential of typ: the
// type's operator fields, in order. A field its own machinery fills is left
// out.
func (s *Session) CredentialFields(ctx context.Context, typ string) ([]CredentialField, error) {
	var list struct {
		SecretTypes []struct {
			Name   string `json:"name"`
			Fields []struct {
				Name   string `json:"name"`
				Secret bool   `json:"secret"`
				Origin string `json:"origin"`
			} `json:"fields"`
		} `json:"secret_types"`
	}
	if err := s.request(ctx, http.MethodGet, "/secret-types", nil, http.StatusOK, &list); err != nil {
		return nil, fmt.Errorf("omnibot: read the secret types: %w", err)
	}
	for _, st := range list.SecretTypes {
		if st.Name != typ {
			continue
		}
		var out []CredentialField
		for _, f := range st.Fields {
			if f.Origin == "operator" {
				out = append(out, CredentialField{Name: f.Name, Secret: f.Secret})
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("omnibot: no secret type %q", typ)
}

// HandCredential creates the credential with the person's own token, never
// omnibot's, and answers the note that tells the model about it: its
// reference, not its value. No value appears in an error it returns.
func (s *Session) HandCredential(ctx context.Context, c Credential, fields map[string]string) (string, error) {
	body := map[string]any{"name": c.Name, "secret_type": c.Type, "owner_kind": c.OwnerKind, "fields": fields}
	if c.Owner != "" {
		body["owner"] = c.Owner
	}
	if err := s.request(ctx, http.MethodPost, "/secrets", body, http.StatusCreated, nil); err != nil {
		values := make([]string, 0, len(fields))
		for _, v := range fields {
			values = append(values, v)
		}
		return "", fmt.Errorf("omnibot: create the secret %s: %s", c.Name, redact.Secrets(err.Error(), values...))
	}
	return CredentialNote(c), nil
}

// CredentialNote is what the model is told about a credential the person
// handed it: its name, type and owner, that it may bind it by name, and that
// its value is out of its reach.
func CredentialNote(c Credential) string {
	where := "the platform"
	if c.OwnerKind != "platform" {
		where = fmt.Sprintf("the %s %s", c.OwnerKind, c.Owner)
	}
	return fmt.Sprintf("I have stored a credential for you: the %s secret %q, at %s. Use it by that name wherever an input takes a secret; its value is not yours to read.", c.Type, c.Name, where)
}
