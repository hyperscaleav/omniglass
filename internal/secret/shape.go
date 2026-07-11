package secret

import "fmt"

// Origin marks who fills a field: an operator at creation, or the secret's own
// lifecycle machinery (an oauth2 access_token, an expiry). Slice-1 shapes are
// all operator-origin; the dimension is carried so the later lifecycle slice
// adds no migration.
type Origin string

const (
	OriginOperator  Origin = "operator"
	OriginLifecycle Origin = "lifecycle"
)

// Field is one member of a secret_type shape: its name, scalar type, whether it
// is sensitive (encrypted at rest, masked on display), and its origin.
type Field struct {
	Name   string `json:"name"`
	Type   string `json:"type"` // string | int | bool (scalar); slice 1 uses string
	Secret bool   `json:"secret"`
	Origin Origin `json:"origin"`
}

// Shape is a secret_type: the named, per-field-typed structure a secret takes
// (snmp_community, basic_auth, oauth2, ...). official marks the ship-with set.
type Shape struct {
	Name     string  `json:"name"`
	Official bool    `json:"official"`
	Fields   []Field `json:"fields"`
}

// Field returns the named field and whether it exists.
func (s Shape) Field(name string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// OperatorFields are the fields an operator must or may supply at creation
// (lifecycle-origin fields are filled later, never required at create).
func (s Shape) OperatorFields() []Field {
	out := make([]Field, 0, len(s.Fields))
	for _, f := range s.Fields {
		if f.Origin != OriginLifecycle {
			out = append(out, f)
		}
	}
	return out
}

// Validate rejects an operator-supplied value map: an unknown field, or a
// lifecycle-origin field the operator tried to set. It does not require every
// operator field (that is the create gate's call, so a partial update stays
// possible); it only refuses fields that do not belong.
func (s Shape) ValidateInput(value map[string]string) error {
	for name := range value {
		f, ok := s.Field(name)
		if !ok {
			return fmt.Errorf("secret: unknown field %q for shape %q", name, s.Name)
		}
		if f.Origin == OriginLifecycle {
			return fmt.Errorf("secret: field %q is lifecycle-managed, not operator-set", name)
		}
	}
	return nil
}
