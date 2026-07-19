package settings

import (
	"errors"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
)

// ErrUnknownNamespace is returned when a write targets a namespace not in the
// Settings struct. The API maps it to 404.
var ErrUnknownNamespace = errors.New("settings: unknown namespace")

// FieldError is a per-field validation failure (an unknown key, or a value that
// fails the field's schema). The API maps it to 422.
type FieldError struct {
	Namespace string
	Key       string
	Message   string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("settings: %s.%s: %s", e.Namespace, e.Key, e.Message)
}

// reg backs the per-namespace schemas reflected from the Settings sub-struct
// types. Built once and reused across validations.
var reg = huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)

// Validate checks a merge-patch against a namespace's schema before it is stored.
// A null value is a delete (always valid, skipped). A non-null value must target a
// known field and pass that field's reflected schema (type, enum, pattern). An
// unknown namespace is ErrUnknownNamespace; an unknown key or bad value is a
// *FieldError.
func Validate(namespace string, patch map[string]any) error {
	t, ok := namespaceType[namespace]
	if !ok {
		return ErrUnknownNamespace
	}
	schema := huma.SchemaFromType(reg, t)
	for key, val := range patch {
		if val == nil {
			continue // null = delete
		}
		prop, ok := schema.Properties[key]
		if !ok {
			return &FieldError{Namespace: namespace, Key: key, Message: "unknown setting"}
		}
		res := &huma.ValidateResult{}
		pb := huma.NewPathBuffer([]byte(namespace+"."+key), 0)
		huma.Validate(reg, prop, pb, huma.ModeWriteToServer, val, res)
		if len(res.Errors) > 0 {
			return &FieldError{Namespace: namespace, Key: key, Message: res.Errors[0].Error()}
		}
	}
	return nil
}
