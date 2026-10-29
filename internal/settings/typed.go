package settings

import (
	"context"
	"encoding/json"
	"fmt"
)

// Typed marshals a resolved settings map through JSON into the typed Settings
// struct. Defaults fill every leaf that declares one, so a mismatch is a
// programming error surfaced as an error. An optional setting (no default tag,
// json omitempty) that no level set is absent and reads as its zero value, which
// its reader takes as unset (RetentionSettings.NodeLogDays, #1350).
func Typed(d Doc) (Settings, error) {
	var s Settings
	b, err := json.Marshal(d)
	if err != nil {
		return s, fmt.Errorf("settings: marshal effective doc: %w", err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("settings: unmarshal into Settings: %w", err)
	}
	return s, nil
}

// EffectiveTyped resolves the effective document and returns it typed. This is the
// app-wide accessor: any Go code that needs a setting reads a field off Settings.
func (s *Service) EffectiveTyped(ctx context.Context) (Settings, error) {
	d, err := s.Effective(ctx)
	if err != nil {
		return Settings{}, err
	}
	return Typed(d)
}
