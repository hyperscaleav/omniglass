package secret

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// EnvPrefix is the prefix of a variable that declares an env-backed platform
// secret (#2223): OMNIGLASS_SECRET__<NAME>='{"type":"api-key","fields":{...}}'.
// The doubled underscore matches the settings layer's OMNIGLASS_SETTING__.
const EnvPrefix = "OMNIGLASS_SECRET__"

// ErrEnvSecret is every boot failure ParseEnv and EnvSecret.Check return. The
// message names the variable and never quotes its value: the value is the
// secret.
var ErrEnvSecret = errors.New("env secret")

// EnvSecret is one platform secret declared by the process environment: its
// cascade name, the variable that declared it, its secret type's name and its
// plaintext fields. It lives in memory only; nothing of it is sealed or
// written to the database.
type EnvSecret struct {
	Name   string
	Var    string
	Type   string
	Fields map[string]string
}

// envSecretName is the rule a derived name must meet, the same lowercase,
// digits and hyphens a stored secret's name takes.
var envSecretName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// EnvSecretName derives a secret's cascade name from its variable's tail:
// lowercased, each underscore a hyphen (BEDROCK_KEY is bedrock-key).
func EnvSecretName(tail string) string {
	return strings.ReplaceAll(strings.ToLower(tail), "_", "-")
}

// EnvVar is the variable that declares the env secret name, the inverse of
// EnvSecretName.
func EnvVar(name string) string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// ParseEnv reads the env-backed platform secrets out of a process environment
// (KEY=VALUE entries, as os.Environ returns; other variables are ignored).
// Each value must be a JSON object {"type": "<secret type>", "fields":
// {"<field>": "<string>", ...}}. A variable whose name derives no legal secret
// name, two variables deriving one name, or a value that does not parse fails
// with the variable's name and never its value. The fields are checked
// against their type's shape later, by Check, once the type catalog is known.
// The result is ordered by name.
func ParseEnv(env []string) ([]EnvSecret, error) {
	var out []EnvSecret
	byName := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, EnvPrefix) {
			continue
		}
		name := EnvSecretName(strings.TrimPrefix(key, EnvPrefix))
		if !envSecretName.MatchString(name) {
			return nil, fmt.Errorf("%w: %s does not name a secret (OMNIGLASS_SECRET__<NAME>, letters, digits and single underscores)", ErrEnvSecret, key)
		}
		if other, dup := byName[name]; dup {
			return nil, fmt.Errorf("%w: %s and %s both declare the secret %q", ErrEnvSecret, other, key, name)
		}
		var doc struct {
			Type   string            `json:"type"`
			Fields map[string]string `json:"fields"`
		}
		dec := json.NewDecoder(bytes.NewReader([]byte(value)))
		dec.DisallowUnknownFields()
		// The decoder's own error can quote a piece of the value, so it is
		// never wrapped: the message is ours alone.
		if err := dec.Decode(&doc); err != nil || dec.More() {
			return nil, fmt.Errorf(`%w: %s is not a JSON object {"type": "<secret type>", "fields": {"<field>": "<string>"}}`, ErrEnvSecret, key)
		}
		if doc.Type == "" {
			return nil, fmt.Errorf("%w: %s names no secret type (its \"type\")", ErrEnvSecret, key)
		}
		byName[name] = key
		out = append(out, EnvSecret{Name: name, Var: key, Type: doc.Type, Fields: doc.Fields})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Check holds an env secret's fields to its type's shape, as a create is held:
// no unknown or lifecycle field, every operator field present. A shape with no
// operator field at all is minted by the platform and cannot be declared. The
// message names the variable and the field, never a value.
func (e EnvSecret) Check(shape Shape) error {
	ops := shape.OperatorFields()
	if len(shape.Fields) > 0 && len(ops) == 0 {
		return fmt.Errorf("%w: %s declares a %s secret, which the platform mints and cannot be declared", ErrEnvSecret, e.Var, shape.Name)
	}
	if err := shape.ValidateInput(e.Fields); err != nil {
		return fmt.Errorf("%w: %s does not fit the %s type: %v", ErrEnvSecret, e.Var, shape.Name, err)
	}
	for _, f := range ops {
		if _, ok := e.Fields[f.Name]; !ok {
			return fmt.Errorf("%w: %s is missing the %s type's field %q", ErrEnvSecret, e.Var, shape.Name, f.Name)
		}
	}
	return nil
}

// Version is the env secret's version, the value an outpost is told so a
// changed credential reads as changed: a MAC of the name, the type and the
// fields under key. It carries nothing a reader without key can turn back
// toward the plaintext, and servers sharing a key agree on it.
func (e EnvSecret) Version(key []byte) string {
	m := hmac.New(sha256.New, key)
	names := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		names = append(names, k)
	}
	sort.Strings(names)
	b, _ := json.Marshal(struct {
		Name   string     `json:"name"`
		Type   string     `json:"type"`
		Fields [][]string `json:"fields"`
	}{e.Name, e.Type, pairs(e.Fields, names)})
	m.Write(b)
	return fmt.Sprintf("env:%x", m.Sum(nil))
}

func pairs(m map[string]string, keys []string) [][]string {
	out := make([][]string, len(keys))
	for i, k := range keys {
		out[i] = []string{k, m[k]}
	}
	return out
}
