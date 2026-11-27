package devseed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
)

// A plan's webhook sender (#1747) is an outside actor that speaks for systems
// as a component of its own: a meeting platform account whose rooms are the
// systems it covers (the whole fleet's, #2790). It seeds as that component, one webhook endpoint of the
// driver its simulator profile names (server hosted, so on no outpost), a
// signing secret minted for this seed alone, and one identity binding per
// system, named for the system, on the driver's room identity. The simulator
// learns the bound names from the bindings and adds each as a room, so its
// report names every system. The account keeps the rooms its profile ships
// with too, and those land in the conduit's unbound inbox (#1756). Everything
// is read from the profile and the driver spec: nothing here knows the
// platform by name.

// senderRow is a sender of a plan: the index of its component in the plan's
// components, and the systems it speaks for.
type senderRow struct {
	Comp    int
	Systems []string
	// Service is a sender that is also a polled service (#2790): the
	// service's seed binds its ids (rooms and devices), so the sender seeds
	// only its endpoint and signing secret.
	Service bool
}

// plannedSender is one sender's seed, decided before any row: the driver of
// its endpoint, the input that takes the signing secret, the secret's name
// and type, and the identity key each of its systems binds on.
type plannedSender struct {
	Comp       int    // index into the plan's components
	Component  string // that component's plan key, the sender's name
	Driver     string
	Input      string
	Secret     string
	SecretType string
	Key        string
	Systems    []string
}

// senderPlans plans every sender of a plan from its profile's named driver.
// Pure: profileFor and specOf are the catalogs it reads.
func senderPlans(rows planRows, profileFor func(product string) (*profile.DeviceProfile, bool), specOf func(id string) (*driver.Spec, error)) ([]plannedSender, error) {
	out := make([]plannedSender, 0, len(rows.Senders))
	for _, sr := range rows.Senders {
		c := rows.Components[sr.Comp]
		prof, ok := profileFor(c.Product)
		if !ok || prof.Webhook == nil || prof.Webhook.Driver == "" {
			return nil, fmt.Errorf("devseed: sender %s: profile %s names no webhook driver", c.Key, c.Product)
		}
		drv := prof.Webhook.Driver
		sp, err := specOf(drv)
		if err != nil {
			return nil, fmt.Errorf("devseed: sender %s: %w", c.Key, err)
		}
		if sp.Session == nil || sp.Session.Credential == "" {
			return nil, fmt.Errorf("devseed: sender %s: driver %s declares no session credential", c.Key, drv)
		}
		secretType := ""
		for _, in := range sp.Inputs {
			if in.Name == sp.Session.Credential && in.Kind == "secret" {
				secretType = in.SecretType
			}
		}
		if secretType == "" {
			return nil, fmt.Errorf("devseed: sender %s: driver %s credential %s is no typed secret input", c.Key, drv, sp.Session.Credential)
		}
		key, systems := "", sr.Systems
		if sr.Service {
			systems = nil
		} else if key, err = identityKeyOf(sp); err != nil {
			return nil, fmt.Errorf("devseed: sender %s: driver %s: %w", c.Key, drv, err)
		}
		out = append(out, plannedSender{
			Comp: sr.Comp, Component: c.Key, Driver: drv, Input: sp.Session.Credential,
			Secret: c.Key + "-signing", SecretType: secretType, Key: key, Systems: systems,
		})
	}
	return out, nil
}

// identityKeyOf is the one identity key a driver's listeners name.
func identityKeyOf(sp *driver.Spec) (string, error) {
	key := ""
	for _, l := range sp.Listeners {
		if l.Identity == nil {
			continue
		}
		if key != "" && key != l.Identity.Key {
			return "", fmt.Errorf("listeners name identity keys %s and %s, want one", key, l.Identity.Key)
		}
		key = l.Identity.Key
	}
	if key == "" {
		return "", fmt.Errorf("no listener names an identity")
	}
	return key, nil
}

// shippedSenders plans a plan's senders against the shipped simulator
// profiles and drivers.
func shippedSenders(rows planRows) ([]plannedSender, error) {
	if len(rows.Senders) == 0 {
		return nil, nil
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		return nil, fmt.Errorf("devseed: load simulator catalog: %w", err)
	}
	return senderPlans(rows, func(p string) (*profile.DeviceProfile, bool) {
		name, _, ok := store.ForProduct(p)
		if !ok {
			return nil, false
		}
		return store.Devices[name], true
	}, seed.ShippedDriverSpec)
}

// mintSecretValue is a fresh random value for one field of a signing secret.
// Each seed mints its own, so no run shares a key with another and none is
// written anywhere but the sealed secret.
func mintSecretValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("devseed: mint a signing key: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// seedPlanSenders seeds each planned sender's secret, endpoint and bindings,
// and returns how many it seeded.
func seedPlanSenders(ctx context.Context, gw storage.Gateway, actorID string, senders []plannedSender, compIDs []string, sysIDs map[string]string) (int, error) {
	all := scope.Set{All: true}
	sc := storage.IdentityBindingScopes{ComponentRead: all, ComponentUpdate: all, SystemRead: all, SystemUpdate: all}
	for _, s := range senders {
		st, err := gw.GetSecretType(ctx, s.SecretType)
		if err != nil {
			return 0, fmt.Errorf("devseed: sender %s: secret type %s: %w", s.Component, s.SecretType, err)
		}
		fields := make(map[string]string, len(st.Fields))
		for _, f := range st.Fields {
			if f.Type != "" && f.Type != "string" {
				return 0, fmt.Errorf("devseed: sender %s: secret type %s field %s is %s, which a seed cannot mint", s.Component, s.SecretType, f.Name, f.Type)
			}
			if fields[f.Name], err = mintSecretValue(); err != nil {
				return 0, err
			}
		}
		if _, err := gw.CreateSecret(ctx, actorID, storage.SecretSpec{
			Name: s.Secret, Label: s.Component + " webhook signing key", SecretType: s.SecretType,
			OwnerKind: "platform", Fields: fields,
		}, all, true); err != nil {
			return 0, fmt.Errorf("devseed: create the signing secret of %s: %w", s.Component, err)
		}
		comp, drv := compIDs[s.Comp], s.Driver
		if _, err := gw.CreateEndpoint(ctx, actorID, storage.EndpointSpec{
			Driver: &drv, Inputs: map[string]string{s.Input: s.Secret}, Label: "Room webhooks",
			Component: &comp, SecretRead: all, SecretBind: all, CanAdmin: true,
		}, all); err != nil {
			return 0, fmt.Errorf("devseed: create the webhook endpoint of %s: %w", s.Component, err)
		}
		for _, sys := range s.Systems {
			id, ok := sysIDs[sys]
			if !ok {
				return 0, fmt.Errorf("devseed: sender %s speaks for system %q, which the plan does not seed", s.Component, sys)
			}
			if _, err := gw.PutIdentityBinding(ctx, actorID, comp, s.Key, sys, storage.IdentityBindingOwner{Kind: "system", Name: id}, sc); err != nil {
				return 0, fmt.Errorf("devseed: bind %s %s=%s to its system: %w", s.Component, s.Key, sys, err)
			}
		}
	}
	return len(senders), nil
}

// serviceRow is a polled service of a plan (#2765): the index of its component
// in the plan's components, and the systems it covers.
type serviceRow struct {
	Comp    int
	Systems []string
}

// componentKeys are the identity keys a service's poll reads that name a
// component, not a system: the meeting platform's device id (#2790). Every
// other key a service reads names a room, which is a system. An installer
// knows which of a platform's ids are rooms and which are devices; this is
// where the seed is told.
var componentKeys = map[string]bool{"ucc-device-id": true}

// seedPlanServices binds each polled service's ids as an installer would, on
// the match key each of its drivers' polls names (ADR-1282): a room id (the
// system's name, the id the simulator's world gives the room) to its system,
// and a device id (the world's id for it, scenario.DeviceID) to the component
// the plan seeded for it, for each system the service covers. Omniglass learns
// which id is which from this configuration; the simulator names its rooms and
// devices by the same ids from its world. The service's own endpoints are the
// ordinary polled ones. It returns how many services it seeded.
func seedPlanServices(ctx context.Context, gw storage.Gateway, actorID string, rows planRows, compIDs []string, sysIDs map[string]string) (int, error) {
	if len(rows.Services) == 0 {
		return 0, nil
	}
	all := scope.Set{All: true}
	sc := storage.IdentityBindingScopes{ComponentRead: all, ComponentUpdate: all, SystemRead: all, SystemUpdate: all}
	drivers := map[string]string{}
	for _, p := range seed.ShippedProducts() {
		drivers[p.Name] = p.Driver
	}
	for _, sr := range rows.Services {
		c := rows.Components[sr.Comp]
		covers := map[string]bool{}
		for _, sys := range sr.Systems {
			covers[sys] = true
		}
		sp, err := seed.ShippedDriverSpec(drivers[c.Product])
		if err != nil {
			return 0, fmt.Errorf("devseed: service %s: %w", c.Key, err)
		}
		var keys []string
		for _, p := range sp.Polls {
			if p.Records != "" && p.Identity != nil {
				keys = append(keys, p.Identity.Key)
			}
		}
		if len(keys) == 0 {
			return 0, fmt.Errorf("devseed: service %s: driver %s has no poll reading records by an identity", c.Key, drivers[c.Product])
		}
		for _, key := range keys {
			if componentKeys[key] {
				for i, row := range rows.Components {
					if row.Platform == "" || !covers[row.System] {
						continue
					}
					if _, err := gw.PutIdentityBinding(ctx, actorID, compIDs[sr.Comp], key, scenario.DeviceID(row.Name), storage.IdentityBindingOwner{Kind: "component", Name: compIDs[i]}, sc); err != nil {
						return 0, fmt.Errorf("devseed: bind %s %s=%s to its component: %w", c.Key, key, row.Name, err)
					}
				}
				continue
			}
			for _, sys := range sr.Systems {
				id, ok := sysIDs[sys]
				if !ok {
					return 0, fmt.Errorf("devseed: service %s covers system %q, which the plan does not seed", c.Key, sys)
				}
				if _, err := gw.PutIdentityBinding(ctx, actorID, compIDs[sr.Comp], key, sys, storage.IdentityBindingOwner{Kind: "system", Name: id}, sc); err != nil {
					return 0, fmt.Errorf("devseed: bind %s %s=%s to its system: %w", c.Key, key, sys, err)
				}
			}
		}
	}
	return len(rows.Services), nil
}
