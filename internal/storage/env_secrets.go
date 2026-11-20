package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
)

// SecretSourceStored and SecretSourceEnv are where a secret comes from: a
// sealed row in Postgres, or a variable in the server's environment (#2223).
const (
	SecretSourceStored = "stored"
	SecretSourceEnv    = "env"
)

// envSecretIDPrefix marks the id an env secret is listed under: it has no row,
// so no uuid, and the prefix keeps the id from ever reading as one.
const envSecretIDPrefix = "env:"

// ErrSecretFromEnv is a write to a secret the environment declares (#2223):
// an update, a delete, a reveal, or a stored secret created under its name.
// The value is the server's environment, so the variable is what changes it.
var ErrSecretFromEnv = errors.New("storage: the secret is declared by the server's environment")

// SecretFromEnvError names the env secret a write was refused on and the
// variable that declares it. errors.Is matches ErrSecretFromEnv.
type SecretFromEnvError struct {
	Name, Var string
}

func (e *SecretFromEnvError) Error() string {
	return fmt.Sprintf("the secret %q is declared by the server's environment variable %s; change that variable and restart the server", e.Name, e.Var)
}

// Is makes the typed error match its sentinel.
func (e *SecretFromEnvError) Is(target error) bool { return target == ErrSecretFromEnv }

// envSecret is one env-backed platform secret installed on the gateway: the
// parsed declaration, its type's catalog row and shape, and its version.
type envSecret struct {
	secret.EnvSecret
	typ     SecretType
	shape   secret.Shape
	version string
}

// InstallEnvSecrets validates the env-backed platform secrets the server read
// at boot (secret.ParseEnv) against the seeded type catalog and installs them,
// in memory only, for every secret consumer to resolve (#2223). A type the
// catalog does not declare, fields that do not fit it, an illegal name, or a
// name a stored platform secret already holds fails the boot naming the
// variable (and the stored secret). Run after the boot seed, so the catalog is
// there to check against; nothing is sealed or written.
func (p *PG) InstallEnvSecrets(ctx context.Context, decl []secret.EnvSecret) error {
	if len(decl) == 0 {
		p.envSecrets = nil
		return nil
	}
	types, err := p.ListSecretTypes(ctx)
	if err != nil {
		return err
	}
	byName := map[string]SecretType{}
	for _, st := range InEffect(types, func(t SecretType) (string, string) { return t.Namespace, t.Name }) {
		byName[st.Name] = st
	}
	key := p.envVersionKey()
	installed := make(map[string]*envSecret, len(decl))
	names := make([]string, 0, len(decl))
	for _, d := range decl {
		if err := ValidateEntityName(d.Name); err != nil {
			return fmt.Errorf("%w: %s derives the secret name %q, which is not a legal name", secret.ErrEnvSecret, d.Var, d.Name)
		}
		st, ok := byName[d.Type]
		if !ok {
			return fmt.Errorf("%w: %s names the secret type %q, which the catalog does not declare", secret.ErrEnvSecret, d.Var, d.Type)
		}
		shape := st.Shape()
		if err := d.Check(shape); err != nil {
			return err
		}
		installed[d.Name] = &envSecret{EnvSecret: d, typ: st, shape: shape, version: d.Version(key)}
		names = append(names, d.Name)
	}
	rows, err := p.pool.Query(ctx, `select name from secret where owner_kind = 'platform' and name = any($1) order by name`, names)
	if err != nil {
		return fmt.Errorf("storage: check env secret names: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("storage: check env secret names: %w", err)
		}
		return fmt.Errorf("%w: %s declares the platform secret %q, and a stored platform secret %q already exists; delete the stored one or rename the variable", secret.ErrEnvSecret, installed[name].Var, name, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: check env secret names: %w", err)
	}
	p.envSecrets = installed
	return nil
}

// envVersionKey is the key env secret versions are MACed under: derived from
// the KEK when the provider can, so servers sharing a KEK agree; otherwise a
// key of this process's own.
func (p *PG) envVersionKey() []byte {
	if vk, ok := p.secret.(secret.VersionKeyer); ok {
		return vk.VersionKey()
	}
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return k
}

// envSecretNamed is the env secret declared under name, or nil.
func (p *PG) envSecretNamed(name string) *envSecret {
	return p.envSecrets[name]
}

// envSecretVisible reports whether a caller with this read scope and admin
// tier may see an env secret: it is a platform secret, so it rides only with
// the all scope, and an admin-sensitive one (its type's default) only with
// the admin tier, exactly as a stored platform secret is filtered.
func envSecretVisible(e *envSecret, read scope.Set, canAdmin bool) bool {
	return read.All && (!e.typ.DefaultAdminSensitive || canAdmin)
}

// envSecretByRef resolves a per-secret route's reference to an env secret the
// caller may see: its bare name, $platform/<name>, or the env:<name> id it is
// listed under. nil when the reference names none.
func (p *PG) envSecretByRef(ref string, read scope.Set, canAdmin bool) *envSecret {
	name := ref
	if owner, n, ok := strings.Cut(ref, "/"); ok {
		if owner != PlatformOwner {
			return nil
		}
		name = n
	}
	name = strings.TrimPrefix(name, envSecretIDPrefix)
	e := p.envSecretNamed(name)
	if e == nil || !envSecretVisible(e, read, canAdmin) {
		return nil
	}
	return e
}

// refuseEnvSecret is the write refusal for a reference that names an env
// secret the caller may see, nil otherwise. A stored secret takes precedence
// for a bare name it resolves (a component's secret that shares the name), so
// the refusal is consulted only once the stored lookup found nothing.
func (p *PG) refuseEnvSecret(ref string, read scope.Set, canAdmin bool) error {
	if e := p.envSecretByRef(ref, read, canAdmin); e != nil {
		return &SecretFromEnvError{Name: e.Name, Var: e.Var}
	}
	return nil
}

// asSecret projects an env secret onto the directory's Secret: its name, its
// type and the env source, never its fields.
func (e *envSecret) asSecret() Secret {
	return Secret{
		ID: envSecretIDPrefix + e.Name, Name: e.Name, SecretType: e.typ.Name, SecretTypeID: e.typ.ID,
		OwnerKind: "platform", AdminSensitive: e.typ.DefaultAdminSensitive,
		Fields: []ResolvedField{}, Source: SecretSourceEnv, SourceVar: e.Var,
	}
}

// envPlatformFields is the plaintext of an env secret of one type, by name,
// for the server's own credentials (omnibot's key, the mail relay's).
// ok is false when the environment declares no such secret of that type.
func (p *PG) envPlatformFields(name, typ string) (map[string]string, bool) {
	e := p.envSecretNamed(name)
	if e == nil || e.typ.Name != typ || e.typ.Namespace != "og" {
		return nil, false
	}
	return e.Fields, true
}

// checkEnvBind judges binding an env secret, as secretRowForAction judges a
// stored platform one: visible only with the all read scope (and the admin
// tier when its type is admin-sensitive), bindable only with the all bind
// scope. found is false when the environment declares no such secret.
func (p *PG) checkEnvBind(ref string, use SecretUse) (e *envSecret, found bool, err error) {
	if isUUID(ref) || strings.Contains(ref, "/") {
		return nil, false, nil
	}
	e = p.envSecretNamed(ref)
	if e == nil {
		return nil, false, nil
	}
	if !envSecretVisible(e, use.Read, use.CanAdmin) {
		return nil, true, ErrSecretNotFound
	}
	if !use.Bind.All {
		return nil, true, ErrSecretForbidden
	}
	return e, true, nil
}

// envSecretNames is the installed env secrets' names, sorted, so every list
// that carries them orders them the same way.
func (p *PG) envSecretNames() []string {
	names := make([]string, 0, len(p.envSecrets))
	for name := range p.envSecrets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// maskedFields is the env secret's fields as the cascade displays them: a
// secret field masked, a non-secret one as declared, in shape order.
func (e *envSecret) maskedFields() []ResolvedField {
	out := make([]ResolvedField, 0, len(e.shape.Fields))
	for _, f := range e.shape.Fields {
		v, ok := e.Fields[f.Name]
		if !ok {
			continue
		}
		rf := ResolvedField{Name: f.Name, Secret: f.Secret, Value: secret.Masked}
		if !f.Secret {
			rf.Value = v
		}
		out = append(out, rf)
	}
	return out
}
