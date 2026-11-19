package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/settings"
)

// OmnibotModelConfig is the model omnibot runs on (#2109), read from the
// assistant settings with its key unsealed. The platform's own read: it never
// leaves the process, and no route returns it.
type OmnibotModelConfig struct {
	Provider string
	Model    string
	BaseURL  string
	Key      string
}

// ErrModelKeyInvalid is a key_secret that is not a platform api-key secret,
// or one that no longer unseals.
var ErrModelKeyInvalid = errors.New("storage: omnibot's model key is invalid")

// OmnibotModel reads the configured model, its key unsealed; nil when no
// provider is configured. A configured provider whose key is gone is
// ErrModelKeyInvalid with the reason, which never quotes the key.
func (p *PG) OmnibotModel(ctx context.Context) (*OmnibotModelConfig, error) {
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	a := s.Assistant
	if a.Provider == "" || a.Provider == "none" {
		return nil, nil
	}
	m := &OmnibotModelConfig{Provider: a.Provider, Model: a.Model, BaseURL: a.BaseURL}
	if a.KeySecret == "" {
		return m, nil
	}
	id, err := platformSecretOfType(ctx, p.pool, a.KeySecret, "api-key")
	if errors.Is(err, ErrSecretNotFound) {
		return nil, fmt.Errorf("%w: key_secret %q is not a platform api-key secret", ErrModelKeyInvalid, a.KeySecret)
	}
	if err != nil {
		return nil, err
	}
	fields, err := p.unsealSecretByID(ctx, p.pool, id)
	if err != nil || fields["key"] == "" {
		return nil, fmt.Errorf("%w: key_secret %q no longer unseals", ErrModelKeyInvalid, a.KeySecret)
	}
	m.Key = fields["key"]
	return m, nil
}

// MergePatchAssistantSettings is MergePatchSettingBinding on the platform
// assistant namespace with the model key's binding judged, as the mail
// relay's is: when the RESULTING settings bind a key_secret and it, the
// base_url or the provider changed, the caller must be able to bind that
// secret, since every run hands it to whatever base_url names.
func (p *PG) MergePatchAssistantSettings(ctx context.Context, actorID string, patch map[string]any, use SecretUse) (*SettingBinding, error) {
	return p.mergePatchSettingBinding(ctx, actorID, "platform", "assistant", patch, func(tx pgx.Tx, before settings.Settings) error {
		after, err := p.platformSettings(ctx, tx)
		if err != nil {
			return err
		}
		b, a := before.Assistant, after.Assistant
		if a.KeySecret == "" || (a.KeySecret == b.KeySecret && a.BaseURL == b.BaseURL && a.Provider == b.Provider) {
			return nil
		}
		return p.checkModelKey(ctx, tx, a.KeySecret, use)
	})
}

// checkModelKey judges binding key_secret: a PLATFORM api-key secret, by bare
// name, the caller may bind.
func (p *PG) checkModelKey(ctx context.Context, q querier, ref string, use SecretUse) error {
	if isUUID(ref) || strings.Contains(ref, "/") {
		return fmt.Errorf("%w: key_secret names a platform secret by its bare name", ErrModelKeyInvalid)
	}
	row, _, err := p.secretRowForAction(ctx, q, ref, use.Read, use.Bind, use.CanAdmin, true)
	if err != nil {
		return err
	}
	if row.ownerKind != "platform" {
		return fmt.Errorf("%w: secret %q is not a platform secret", ErrModelKeyInvalid, ref)
	}
	if _, err := platformSecretOfType(ctx, q, ref, "api-key"); errors.Is(err, ErrSecretNotFound) {
		return fmt.Errorf("%w: secret %q is not an api-key secret", ErrModelKeyInvalid, ref)
	} else if err != nil {
		return err
	}
	return nil
}
