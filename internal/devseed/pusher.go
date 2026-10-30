package devseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// SimPusher is the simulated fleet's first-party pusher (#1370): a service
// principal holding telemetry:push alone, on one system, through which the
// simulator's booking service posts the room's bookings to POST
// /telemetry:push, as a scheduling platform's integration would. A push has
// no endpoint, so it counts on its collected tier alone. Service names the
// principal; System is a fixture system key.
type SimPusher struct {
	Service string `yaml:"service"`
	System  string `yaml:"system"`
}

// PusherRole is the role the simulated pusher holds: telemetry:push and
// nothing else. An operator row, not an official one: no install ships it.
const PusherRole = "telemetry-pusher"

// GrantPusher makes service a principal that may push telemetry to the one
// system systemID and do nothing else: the pusher role, the service, and its
// one grant, scoped to that system alone. Idempotent; its token is the lab's
// to set on each run (storage.SetServiceToken).
func GrantPusher(ctx context.Context, gw storage.Gateway, actorID, service, systemID string) error {
	if err := gw.EnsureRole(ctx, actorID, storage.Role{
		Name: PusherRole, Permissions: []string{"telemetry:push"}, Label: "Telemetry pusher",
		Description: "An integration that only reports data: pushes telemetry to the owners its grant scopes, and reads nothing else.",
	}); err != nil {
		return fmt.Errorf("devseed: pusher role: %w", err)
	}
	id, err := gw.EnsureServicePrincipal(ctx, actorID, service)
	if err != nil {
		return fmt.Errorf("devseed: pusher %s: %w", service, err)
	}
	_, err = gw.CreateGrant(ctx, actorID, id, storage.GrantSpec{
		Role: PusherRole, ScopeKind: "system", ScopeID: systemID, ScopeOp: scope.OpSelf,
	}, scope.Set{All: true})
	if err != nil && !errors.Is(err, storage.ErrGrantExists) {
		return fmt.Errorf("devseed: grant pusher %s: %w", service, err)
	}
	return nil
}
