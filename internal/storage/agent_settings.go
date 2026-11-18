package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/settings"
)

// AgentSettings is the effective agent namespace (#2063): the policy the
// auth middleware applies to an agent's request by the class of its
// operation, re-read per request so a change takes at once.
func (p *PG) AgentSettings(ctx context.Context) (settings.AgentSettings, error) {
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return settings.AgentSettings{}, fmt.Errorf("storage: agent settings: %w", err)
	}
	return s.Agent, nil
}
