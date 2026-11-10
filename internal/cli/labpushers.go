package cli

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab"
	labscenario "github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A lab's outside actors (#1743) are declared by its scenario, as its
// devices are: a scale scenario's pushers per site, the dev fleet's in its
// fixtures. Each run mints every declared pusher a token, the way it mints
// each node one: the plaintext goes to the emulator on the pusher's node,
// the hash and prefix to the seed, which grants the pusher's principal its
// systems and sets the credential. One path for both lab kinds.

// minter mints a bearer token: the plaintext, its hash and its non-secret
// prefix (auth.NewBearerToken).
type minter func() (token string, hash []byte, prefix string, err error)

func mintPusher(name, profile, node string, mint minter) (lab.Pusher, error) {
	token, hash, prefix, err := mint()
	if err != nil {
		return lab.Pusher{}, fmt.Errorf("lab: mint a token for pusher %s: %w", name, err)
	}
	return lab.Pusher{Name: name, Profile: profile, Node: node, Token: token, TokenHash: hex.EncodeToString(hash), TokenPrefix: prefix}, nil
}

// scaleLabPushers mints a token for each pusher a scale plan declares, on
// its site's node. Pure but for mint.
func scaleLabPushers(plan *labscenario.Plan, mint minter) ([]lab.Pusher, error) {
	var out []lab.Pusher
	for _, site := range plan.Sites {
		for _, p := range site.Pushers {
			lp, err := mintPusher(p.Name, p.Profile, labscenario.SiteNode(site.Name), mint)
			if err != nil {
				return nil, err
			}
			out = append(out, lp)
		}
	}
	return out, nil
}

// devLabPushers mints a token for each pusher the dev fleet declares, on
// the lab's one node. A lab with no node runs no emulator, so it seeds no
// pusher and mints none. Pure but for mint.
func devLabPushers(ps []devseed.SimPusher, noNode bool, mint minter) ([]lab.Pusher, error) {
	if noNode {
		return nil, nil
	}
	out := make([]lab.Pusher, 0, len(ps))
	for _, p := range ps {
		lp, err := mintPusher(p.Name, p.Profile, lab.NodeName, mint)
		if err != nil {
			return nil, err
		}
		out = append(out, lp)
	}
	return out, nil
}

// setPusherTokens gives each pusher this run's token, replacing the last
// run's, so the emulator on its node can push with it. A pusher whose
// principal the lab's fleet does not hold (a lab kept from before it was
// declared, never seeded again) is noted, not fatal: nothing pushes for it.
func setPusherTokens(ctx context.Context, out io.Writer, gw storage.Gateway, tokens map[string]lab.PusherToken) error {
	names := make([]string, 0, len(tokens))
	for name := range tokens {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := tokens[name]
		hash, err := hex.DecodeString(t.Hash)
		if err != nil {
			return fmt.Errorf("lab seed: %s: pusher %s: the hash is not hex", lab.PusherTokensEnv, name)
		}
		ok, err := gw.SetServiceToken(ctx, "", name, hash, t.Prefix)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintf(out, "note: this lab's fleet predates the pusher %s, so it does not push; omniglass lab run --fresh adds it\n", name)
		}
	}
	return nil
}
