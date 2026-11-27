package devseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// SimPusher is one of the simulated fleet's outside actors (#1370, #1743),
// declared as a scale scenario declares its pushers: Name is its service
// user, holding telemetry:push alone on Systems (fixture system keys),
// and Profile the emulator profile with a push face that pushes as it. A
// push has no endpoint, so it counts on its collected tier alone.
type SimPusher struct {
	Name    string   `yaml:"name"`
	Profile string   `yaml:"profile"`
	Systems []string `yaml:"systems"`
}

// PusherRole is the role the simulated pusher holds: telemetry:push and
// nothing else. An operator row, not an official one: no install ships it.
const PusherRole = "telemetry-pusher"

// GrantPusher makes service a user that may push telemetry to the one
// system systemID and do nothing else: the pusher role, the service, and its
// one grant, scoped to that system alone. Idempotent; its token is the lab's
// to set on each run (storage.SetMachineToken).
func GrantPusher(ctx context.Context, gw storage.Gateway, actorID, service, systemID string) error {
	if err := gw.EnsureRole(ctx, actorID, storage.Role{
		Name: PusherRole, Permissions: []string{"telemetry:push"}, Label: "Telemetry pusher",
		Description: "An integration that only reports data: pushes telemetry to the owners its grant scopes, and reads nothing else.",
	}); err != nil {
		return fmt.Errorf("devseed: pusher role: %w", err)
	}
	id, err := gw.EnsureMachine(ctx, actorID, service)
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

// EnsureSimPushers grants every pusher the fixtures declare (#1370, #1743) its
// user and its systems on a fleet already seeded: the lab's kept run (#2194),
// where a pusher added to the fixtures after the fleet was first seeded has no
// user for the run's token to land on. It seeds nothing else and only reads
// the fleet to find each pusher's systems, so a kept lab stays as it was kept.
// Idempotent: a pusher the fleet holds is left as it is. A pusher whose system
// the fleet no longer holds (an operator deleted it) is not granted, and comes
// back in missing by name with the system key it lacked, so the caller can say
// so; nothing pushes for it.
func EnsureSimPushers(ctx context.Context, gw storage.Gateway, actorID string) (missing []string, err error) {
	doc, err := Fixtures()
	if err != nil {
		return nil, err
	}
	if len(doc.SimPushers) == 0 {
		return nil, nil
	}
	sysIDs, err := fixtureSystemIDs(ctx, gw, doc)
	if err != nil {
		return nil, err
	}
	for _, p := range doc.SimPushers {
		ids := make([]string, 0, len(p.Systems))
		for _, key := range p.Systems {
			id, ok := sysIDs[key]
			if !ok {
				missing = append(missing, p.Name+" (system "+key+")")
				ids = nil
				break
			}
			ids = append(ids, id)
		}
		for _, id := range ids {
			if err := GrantPusher(ctx, gw, actorID, p.Name, id); err != nil {
				return missing, err
			}
		}
	}
	return missing, nil
}

// fixtureSystemIDs maps each fixture system's key to the row the fleet holds
// for it, found the way runDoc finds it before it would create one: a named
// row by its name, a platform-named one by claiming the next row of its own
// placement and type in the order they were minted. Read-only; a system the
// fleet lacks is left out. Every location and system is walked in fixture
// order, as runDoc walks them, because the claim is positional.
func fixtureSystemIDs(ctx context.Context, gw storage.Gateway, doc Doc) (map[string]string, error) {
	all := scope.Set{All: true}
	locIndex, err := locationGenIndex(ctx, gw, all)
	if err != nil {
		return nil, err
	}
	locIDs := map[string]string{}
	for _, l := range doc.Locations {
		parentID := locIDs[l.Parent]
		if l.Parent != "" && parentID == "" {
			continue // its parent is gone, so it is too
		}
		id, err := existingRowID(l.Name, l.Key, genSlot{bucket: bucketOf(parentID, ""), class: l.Type}, locIndex, func(name string) (string, error) {
			existing, err := gw.GetLocation(ctx, name, all)
			if errors.Is(err, storage.ErrLocationNotFound) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			return existing.ID, nil
		})
		if err != nil {
			return nil, fmt.Errorf("devseed: find location %q: %w", l.Key, err)
		}
		if id != "" {
			locIDs[l.Key] = id
		}
	}
	sysIndex, err := systemGenIndex(ctx, gw, all)
	if err != nil {
		return nil, err
	}
	sysIDs := map[string]string{}
	for _, s := range doc.Systems {
		if s.Location != "" && locIDs[s.Location] == "" {
			continue // its room is gone, so it is too
		}
		id, err := existingRowID(s.Name, s.Key, genSlot{bucket: bucketOf("", locIDs[s.Location]), class: s.SystemType}, sysIndex, func(name string) (string, error) {
			existing, err := gw.GetSystem(ctx, name, all)
			if errors.Is(err, storage.ErrSystemNotFound) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			return existing.ID, nil
		})
		if err != nil {
			return nil, fmt.Errorf("devseed: find system %q: %w", s.Key, err)
		}
		if id != "" {
			sysIDs[s.Key] = id
		}
	}
	return sysIDs, nil
}
