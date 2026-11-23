package lab

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/hyperscaleav/omniglass/internal/sso"
)

// Dex's subject for a static password user is its id and connector, encoded
// as Dex does: the vector is Dex's own example user, whose id_token sub its
// docs show.
func TestDexSubjectEncodesAsDexDoes(t *testing.T) {
	if got, want := DexSubject("08a8684b-db88-4b73-90a9-3cd1661f5466", "local"), "CiQwOGE4Njg0Yi1kYjg4LTRiNzMtOTBhOS0zY2QxNjYxZjU0NjYSBWxvY2Fs"; got != want {
		t.Fatalf("DexSubject = %q, want Dex's %q", got, want)
	}
	if !strings.HasPrefix(SSODemoIdentity(), DexIssuer+"|") || strings.Count(SSODemoIdentity(), "|") != 1 {
		t.Fatalf("SSODemoIdentity = %q, want %s|<sub>", SSODemoIdentity(), DexIssuer)
	}
}

// The demo person's static password and the hash Dex checks it against are
// one pair, and the issuer is Dex's service address on the lab network.
func TestTheLabsDexIsOneConsistentDemo(t *testing.T) {
	if err := bcrypt.CompareHashAndPassword([]byte(ssoDemoPasswordHash), []byte(SSODemoPassword)); err != nil {
		t.Fatalf("the demo password does not match its hash: %v", err)
	}
	if want := "http://" + DexService + ":" + strconv.Itoa(DexPort) + "/dex"; DexIssuer != want {
		t.Fatalf("DexIssuer = %q, want %q", DexIssuer, want)
	}
	cfg := DexConfig(DexIssuer, ServerURL+sso.CallbackPath)
	for _, want := range []string{"issuer: " + DexIssuer, `"http://server:8080/auth/oidc/callback"`, "id: " + DexClientID,
		"secret: " + DexClientSecret, "userID: " + SSODemoUserID, "email: " + SSODemoEmail, "0.0.0.0:5556"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("Dex's config lacks %q:\n%s", want, cfg)
		}
	}
}

// Only a throwaway lab runs the identity provider: its issuer is an address on
// the lab network a learner's browser cannot reach. With it, Dex runs as its
// own unprivileged service from its pinned image, configured from its env,
// and the seed configures single sign-on; without it, nothing of it runs.
func TestPlanSSO(t *testing.T) {
	s, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if s.IdP.Name != "" || slices.Contains(s.Seed.Cmd, "--sso") {
		t.Fatalf("a lab without the option runs %q and seeds %v, want no identity provider", s.IdP.Name, s.Seed.Cmd)
	}
	for _, svc := range s.Services() {
		if svc.Image == DexImage {
			t.Fatalf("a lab without the option runs %s", DexImage)
		}
	}
	o := testOptions()
	o.SSO = true
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "--ephemeral") {
		t.Fatalf("a learner's lab with an identity provider: %v, want a refusal naming --ephemeral", err)
	}
	o.Project, o.Ephemeral = "walk-1", true
	if s, err = Plan(o); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.Seed.Cmd, "--sso") {
		t.Fatalf("a throwaway lab with an identity provider: the seed runs %v, want --sso", s.Seed.Cmd)
	}
	d := s.IdP
	if d.Name != DexService || d.Image != DexImage || d.Container != "walk-1-dex-1" || d.Publish != nil || len(d.CapAdd) != 0 || d.User != "" {
		t.Fatalf("the identity provider = %+v, want Dex, unpublished and unprivileged", d)
	}
	if !slices.ContainsFunc(s.Services(), func(svc Service) bool { return svc.Name == DexService }) {
		t.Fatal("Services() leaves the identity provider out")
	}
	var cfg string
	for _, e := range d.Env {
		if v, ok := strings.CutPrefix(e, dexConfigEnv+"="); ok {
			cfg = v
		}
	}
	if cfg != DexConfig(DexIssuer, ServerURL+sso.CallbackPath) {
		t.Fatalf("Dex's env config = %q, want the lab's", cfg)
	}
	if !slices.Contains(d.Entrypoint, "dex serve /tmp/dex.yaml") && !strings.Contains(strings.Join(d.Entrypoint, " "), "dex serve") {
		t.Fatalf("Dex's entrypoint %v does not serve its config", d.Entrypoint)
	}
	if len(d.Health) == 0 || !strings.Contains(strings.Join(d.Health, " "), "/dex/.well-known/openid-configuration") {
		t.Fatalf("Dex's health check %v does not read its discovery", d.Health)
	}
	if d.Labels[LabelComposeService] != DexService || d.Labels[LabelLabEphemeral] != "true" || d.Labels[LabelComposeHash] == "" {
		t.Fatalf("Dex's labels %v, want the lab's", d.Labels)
	}
	// A scale lab's seed configures no single sign-on, so it runs no provider.
	sc := scaleOptions()
	sc.SSO = true
	if _, err := Plan(sc); err == nil || !strings.Contains(err.Error(), "--sso") {
		t.Fatalf("a scale lab with an identity provider: %v, want a refusal naming --sso", err)
	}
	// The image pin is not the omniglass image's: WithImage leaves it.
	if s.WithImage("sha256:abc").IdP.Image != DexImage {
		t.Fatal("WithImage moved the identity provider off its own image")
	}
}
