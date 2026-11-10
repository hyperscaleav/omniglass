package simnode

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// Pusher is one outside actor the emulator runs beside its node (#1743): a
// pusher the lab's scenario declared, as a device of a profile with a push
// face, holding the bearer token the lab minted for its principal this run.
// Name is the principal's; the emulator never logs Token.
type Pusher struct {
	Name, Profile, Token string
}

// PushersEnv carries a node's pushers to its emulator, as FormatPushers
// writes them. The lab sets it per node; it is never printed.
const PushersEnv = "OMNIGLASS_SIM_PUSHERS"

// FormatPushers writes each pusher as name=profile:token, comma separated. A
// token is the scheme, its hex prefix and its base64url secret, so it carries
// none of the delimiters; ParsePushers reads it back.
func FormatPushers(ps []Pusher) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.Name + "=" + p.Profile + ":" + p.Token
	}
	return strings.Join(parts, ",")
}

// ParsePushers reads FormatPushers back. A part missing its name, profile or
// token, or a name given twice, is refused. Pure.
func ParsePushers(s string) ([]Pusher, error) {
	if s == "" {
		return nil, nil
	}
	var out []Pusher
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		name, rest, ok := strings.Cut(part, "=")
		prof, token, ok2 := strings.Cut(rest, ":")
		if !ok || !ok2 || name == "" || prof == "" || token == "" {
			// The part may hold a token: name the pusher, never echo the part.
			return nil, fmt.Errorf("sim: %s: a pusher %q is not name=profile:token", PushersEnv, name)
		}
		if seen[name] {
			return nil, fmt.Errorf("sim: %s: pusher %q is given twice", PushersEnv, name)
		}
		seen[name] = true
		out = append(out, Pusher{Name: name, Profile: prof, Token: token})
	}
	return out, nil
}

// startPushers runs each pusher as an emulator device of its profile, apart
// from the node's simulated fleet (a pusher is no component the server lists
// for it), and hands each its token: its push face then pushes for every
// system its grant names, on its profile's cadence. A pusher whose profile
// the catalog lacks or has no push face is refused, since it would never
// push. The caller stops the engine.
func startPushers(store *profile.Store, server string, ps []Pusher) (*sim.Engine, error) {
	snaps := make([]sim.DeviceSnapshot, len(ps))
	for i, p := range ps {
		prof, ok := store.Devices[p.Profile]
		if !ok {
			return nil, fmt.Errorf("sim: pusher %s names profile %s, which the catalog lacks", p.Name, p.Profile)
		}
		if prof.Push == nil {
			return nil, fmt.Errorf("sim: pusher %s names profile %s, which has no push face", p.Name, p.Profile)
		}
		snaps[i] = sim.DeviceSnapshot{DeviceID: p.Name, Hostname: p.Name, LoopbackIP: "127.0.0.1", ProfileName: p.Profile, Version: 1}
	}
	e := sim.NewEngine(store, profiles.Templates())
	if err := e.LoadDevices(snaps); err != nil {
		return nil, err
	}
	devs := e.Devices()
	if len(devs) != len(ps) {
		return nil, errors.New("sim: not every pusher loaded")
	}
	if err := e.StartAll(); err != nil {
		e.StopAll()
		return nil, err
	}
	tokens := map[string]string{}
	for _, p := range ps {
		tokens[p.Name] = p.Token
	}
	for _, d := range devs {
		d.SetPushCredential(server, tokens[d.DeviceID])
	}
	return e, nil
}
