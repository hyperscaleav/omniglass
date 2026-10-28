// The tests drive scripts/agent-token.sh and the scripts/agent-bin/gh shim
// against a fake GitHub API: an httptest server that checks every App JWT
// against the test key's public half and mints installation tokens, so the
// signing, the cache, the git wiring and the gh wiring are all exercised for
// real with only GitHub itself faked (#1281).
package scripts

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	agentAppID          = "424242"
	agentInstallationID = 77
	agentBotID          = 9001
)

// fakeGitHub is the slice of GitHub's API the script talks to.
type fakeGitHub struct {
	t   *testing.T
	pub *rsa.PublicKey

	mu        sync.Mutex
	minted    []string // tokens issued, in order
	paths     []string // "METHOD path" for every request
	ttl       time.Duration
	installed bool
	lastBody  map[string]any
}

func (f *fakeGitHub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/hyperscaleav/omniglass/installation", func(w http.ResponseWriter, r *http.Request) {
		if !f.appAuth(w, r) {
			return
		}
		if !f.installed {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		fmt.Fprintf(w, `{"id":%d}`, agentInstallationID)
	})
	mux.HandleFunc(fmt.Sprintf("POST /app/installations/%d/access_tokens", agentInstallationID), func(w http.ResponseWriter, r *http.Request) {
		if !f.appAuth(w, r) {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.lastBody = body
		tok := fmt.Sprintf("ghs_fake%d", len(f.minted)+1)
		f.minted = append(f.minted, tok)
		exp := time.Now().Add(f.ttl).UTC().Format(time.RFC3339)
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"token":%q,"expires_at":%q}`, tok, exp)
	})
	mux.HandleFunc("GET /app", func(w http.ResponseWriter, r *http.Request) {
		if !f.appAuth(w, r) {
			return
		}
		fmt.Fprint(w, `{"slug":"omniglass-agent"}`)
	})
	mux.HandleFunc("GET /users/{login}", func(w http.ResponseWriter, r *http.Request) {
		if !f.tokenAuth(w, r) {
			return
		}
		if r.PathValue("login") != "omniglass-agent[bot]" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"id":%d}`, agentBotID)
	})
	mux.HandleFunc("GET /installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		if !f.tokenAuth(w, r) {
			return
		}
		fmt.Fprint(w, `{"repositories":[{"full_name":"hyperscaleav/omniglass"}]}`)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	})
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// appAuth accepts only a JWT the test key signed, issued by the App, inside
// GitHub's ten-minute window.
func (f *fakeGitHub) appAuth(w http.ResponseWriter, r *http.Request) bool {
	if err := f.checkJWT(bearer(r)); err != nil {
		f.t.Errorf("%s %s: bad App JWT: %v", r.Method, r.URL.Path, err)
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

// tokenAuth accepts only an installation token this fake minted.
func (f *fakeGitHub) tokenAuth(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, tok := range f.minted {
		if bearer(r) == tok {
			return true
		}
	}
	f.t.Errorf("%s %s: not an installation token: %q", r.Method, r.URL.Path, bearer(r))
	w.WriteHeader(http.StatusUnauthorized)
	return false
}

func (f *fakeGitHub) checkJWT(jwt string) error {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return fmt.Errorf("%d parts", len(parts))
	}
	dec := base64.RawURLEncoding
	var header struct{ Alg, Typ string }
	var claims struct {
		Iat, Exp int64
		Iss      string
	}
	for i, v := range []any{&header, &claims} {
		raw, err := dec.DecodeString(parts[i])
		if err != nil {
			return fmt.Errorf("part %d: %w", i, err)
		}
		if err := json.Unmarshal(raw, v); err != nil {
			return fmt.Errorf("part %d: %w", i, err)
		}
	}
	sig, err := dec.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(f.pub, crypto.SHA256, sum[:], sig); err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	now := time.Now().Unix()
	switch {
	case header.Alg != "RS256":
		return fmt.Errorf("alg %q", header.Alg)
	case claims.Iss != agentAppID:
		return fmt.Errorf("iss %q", claims.Iss)
	case claims.Iat > now:
		return fmt.Errorf("iat %d is in the future", claims.Iat)
	case claims.Exp <= now:
		return fmt.Errorf("exp %d has passed", claims.Exp)
	case claims.Exp-claims.Iat > 600:
		return fmt.Errorf("lifetime %ds is over GitHub's ten minutes", claims.Exp-claims.Iat)
	}
	return nil
}

func (f *fakeGitHub) mints() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.minted)
}

func (f *fakeGitHub) called(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.paths {
		if p == path {
			return true
		}
	}
	return false
}

// ---- the harness -------------------------------------------------------------

type agentRig struct {
	t      *testing.T
	gh     *fakeGitHub
	dir    string
	keyPEM string
	cache  string
	env    []string
}

func newAgentRig(t *testing.T) *agentRig {
	t.Helper()
	requireAgentTools(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	keyPEM := filepath.Join(dir, "app.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyPEM, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeGitHub{t: t, pub: &key.PublicKey, ttl: time.Hour, installed: true}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "cache", "token.json")
	env := []string{}
	for _, kv := range os.Environ() {
		name := kv[:strings.IndexByte(kv, '=')]
		if strings.HasPrefix(name, "AGENT_") || strings.HasPrefix(name, "GIT_") ||
			name == "GH_TOKEN" || name == "GITHUB_TOKEN" || name == "HOME" || name == "XDG_CONFIG_HOME" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"AGENT_APP_ID="+agentAppID,
		"AGENT_APP_KEY="+keyPEM,
		"AGENT_API="+srv.URL,
		"AGENT_TOKEN_CACHE="+cache,
	)
	return &agentRig{t: t, gh: fake, dir: dir, keyPEM: keyPEM, cache: cache, env: env}
}

// requireAgentTools skips off CI on a host without what agent-token.sh needs;
// in CI a missing tool fails, so the gate never goes quiet.
func requireAgentTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "jq", "openssl", "curl", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s is not on PATH: scripts/agent-token.sh and its tests need it", tool)
			}
			t.Skipf("%s is not on PATH: scripts/agent-token.sh needs it, so its tests skip here", tool)
		}
	}
}

type agentResult struct {
	code           int
	stdout, stderr string
}

// run runs name (a path relative to scripts/) in dir with extra env and stdin.
func (a *agentRig) run(dir, stdin string, extra []string, name string, args ...string) agentResult {
	a.t.Helper()
	script, err := filepath.Abs(name)
	if err != nil {
		a.t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(append([]string{}, a.env...), extra...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	r := agentResult{}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			a.t.Fatal(err)
		}
		r.code = ee.ExitCode()
	}
	r.stdout, r.stderr = out.String(), errb.String()
	return r
}

func (a *agentRig) token(extra ...string) agentResult {
	a.t.Helper()
	return a.run(".", "", extra, "agent-token.sh", "token")
}

func mustOK(t *testing.T, r agentResult) {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
	}
}

// ---- token -------------------------------------------------------------------

func TestAgentTokenMintsAnInstallationTokenNarrowedToTheRepo(t *testing.T) {
	a := newAgentRig(t)
	r := a.token()
	mustOK(t, r)
	if got := strings.TrimSpace(r.stdout); got != "ghs_fake1" {
		t.Fatalf("token = %q, want ghs_fake1", got)
	}
	repos, _ := a.gh.lastBody["repositories"].([]any)
	if len(repos) != 1 || repos[0] != "omniglass" {
		t.Fatalf("mint body repositories = %v, want [omniglass]", a.gh.lastBody["repositories"])
	}
}

func TestAgentTokenReusesTheCachedToken(t *testing.T) {
	a := newAgentRig(t)
	mustOK(t, a.token())
	r := a.token()
	mustOK(t, r)
	if got := strings.TrimSpace(r.stdout); got != "ghs_fake1" {
		t.Fatalf("second token = %q, want the cached ghs_fake1", got)
	}
	if n := a.gh.mints(); n != 1 {
		t.Fatalf("minted %d tokens, want 1", n)
	}
}

func TestAgentTokenReMintsATokenNearItsExpiry(t *testing.T) {
	a := newAgentRig(t)
	a.gh.ttl = 5 * time.Minute // under the 10 minute default floor
	mustOK(t, a.token())
	r := a.token()
	mustOK(t, r)
	if got := strings.TrimSpace(r.stdout); got != "ghs_fake2" {
		t.Fatalf("token = %q, want a fresh ghs_fake2", got)
	}
}

func TestAgentTokenCacheIsPrivate(t *testing.T) {
	a := newAgentRig(t)
	mustOK(t, a.token())
	info, err := os.Stat(a.cache)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("cache mode = %o, want 600", mode)
	}
	dirInfo, err := os.Stat(filepath.Dir(a.cache))
	if err != nil {
		t.Fatal(err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("cache dir mode = %o, want 700", mode)
	}
}

func TestAgentTokenSkipsTheLookupForAGivenInstallation(t *testing.T) {
	a := newAgentRig(t)
	mustOK(t, a.token(fmt.Sprintf("AGENT_INSTALLATION_ID=%d", agentInstallationID)))
	if a.gh.called("GET /repos/hyperscaleav/omniglass/installation") {
		t.Fatal("looked the installation up although AGENT_INSTALLATION_ID was given")
	}
}

func TestAgentTokenNamesTheMissingConfiguration(t *testing.T) {
	a := newAgentRig(t)
	for _, tc := range []struct {
		name, env, want string
	}{
		{"no app id", "AGENT_APP_ID=", "AGENT_APP_ID is not set"},
		{"app id not a number", "AGENT_APP_ID=omniglass", "AGENT_APP_ID must be a number"},
		{"no key", "AGENT_APP_KEY=", "AGENT_APP_KEY is not set"},
		{"unreadable key", "AGENT_APP_KEY=" + filepath.Join(a.dir, "missing.pem"), "is not a readable file"},
		{"bad min ttl", "AGENT_TOKEN_MIN_TTL=soon", "AGENT_TOKEN_MIN_TTL must be"},
		{"bad repo", "AGENT_REPO=omniglass", "AGENT_REPO must be owner/name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := a.token(tc.env)
			if r.code != 2 || !strings.Contains(r.stderr, tc.want) {
				t.Fatalf("exit %d, stderr %q; want exit 2 naming %q", r.code, r.stderr, tc.want)
			}
		})
	}
}

func TestAgentTokenRefusesAKeyThatIsNotAPrivateKey(t *testing.T) {
	a := newAgentRig(t)
	junk := filepath.Join(a.dir, "junk.pem")
	if err := os.WriteFile(junk, []byte("not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := a.token("AGENT_APP_KEY=" + junk)
	if r.code != 1 || !strings.Contains(r.stderr, "could not sign") {
		t.Fatalf("exit %d, stderr %q; want exit 1 naming the signing failure", r.code, r.stderr)
	}
	if a.gh.mints() != 0 {
		t.Fatal("minted a token with an unsigned JWT")
	}
}

func TestAgentTokenNamesAnAppThatIsNotInstalled(t *testing.T) {
	a := newAgentRig(t)
	a.gh.installed = false
	r := a.token()
	if r.code != 1 || !strings.Contains(r.stderr, "HTTP 404: Not Found") || !strings.Contains(r.stderr, "not installed on hyperscaleav/omniglass") {
		t.Fatalf("exit %d, stderr %q; want exit 1 naming GitHub's 404 and the repo", r.code, r.stderr)
	}
	if strings.TrimSpace(r.stdout) != "" {
		t.Fatalf("printed %q on failure", r.stdout)
	}
}

func TestAgentTokenUsage(t *testing.T) {
	a := newAgentRig(t)
	for _, args := range [][]string{{}, {"nope"}, {"token", "extra"}, {"git-credential"}, {"git-credential", "fill"}} {
		r := a.run(".", "", nil, "agent-token.sh", args...)
		if r.code != 2 || !strings.Contains(r.stderr, "usage:") {
			t.Errorf("args %v: exit %d, stderr %q; want usage and exit 2", args, r.code, r.stderr)
		}
	}
}

// ---- git -------------------------------------------------------------------

func TestAgentTokenAnswersGitForGitHubOnly(t *testing.T) {
	a := newAgentRig(t)
	r := a.run(".", "protocol=https\nhost=github.com\n\n", nil, "agent-token.sh", "git-credential", "get")
	mustOK(t, r)
	if r.stdout != "username=x-access-token\npassword=ghs_fake1\n" {
		t.Fatalf("github.com answer = %q", r.stdout)
	}
	for _, in := range []string{"protocol=https\nhost=gitlab.com\n\n", "protocol=http\nhost=github.com\n\n"} {
		r := a.run(".", in, nil, "agent-token.sh", "git-credential", "get")
		mustOK(t, r)
		if r.stdout != "" {
			t.Fatalf("answered %q for %q, want nothing", r.stdout, in)
		}
	}
	for _, op := range []string{"store", "erase"} {
		r := a.run(".", "protocol=https\nhost=github.com\npassword=x\n\n", nil, "agent-token.sh", "git-credential", op)
		mustOK(t, r)
		if r.stdout != "" {
			t.Fatalf("%s printed %q", op, r.stdout)
		}
	}
}

func git(t *testing.T, a *agentRig, dir string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = a.env
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestAgentTokenSetupMakesTheBotTheAuthorAndTheOnlyCredential(t *testing.T) {
	a := newAgentRig(t)
	// The architect's own helper, inherited from global config, must never answer.
	git(t, a, a.dir, "", "config", "--global", "credential.helper", "!f() { echo username=fullmetal-fred; echo password=architect; }; f")
	clone := filepath.Join(a.dir, "clone")
	git(t, a, a.dir, "", "init", "-q", clone)

	r := a.run(clone, "", nil, "agent-token.sh", "setup")
	mustOK(t, r)

	if got := strings.TrimSpace(git(t, a, clone, "", "config", "user.name")); got != "omniglass-agent[bot]" {
		t.Fatalf("user.name = %q", got)
	}
	wantEmail := fmt.Sprintf("%d+omniglass-agent[bot]@users.noreply.github.com", agentBotID)
	if got := strings.TrimSpace(git(t, a, clone, "", "config", "user.email")); got != wantEmail {
		t.Fatalf("user.email = %q, want %q", got, wantEmail)
	}
	filled := git(t, a, clone, "protocol=https\nhost=github.com\n\n", "credential", "fill")
	if !strings.Contains(filled, "password=ghs_fake1") || strings.Contains(filled, "architect") {
		t.Fatalf("git credential fill = %q, want the App token and never the architect's", filled)
	}

	// A second setup leaves exactly one reset and one helper.
	mustOK(t, a.run(clone, "", nil, "agent-token.sh", "setup"))
	helpers := strings.Split(strings.TrimRight(git(t, a, clone, "", "config", "--local", "--get-all", "credential.https://github.com.helper"), "\n"), "\n")
	if len(helpers) != 2 || helpers[0] != "" || !strings.HasSuffix(helpers[1], "git-credential") {
		t.Fatalf("helpers after two setups = %q", helpers)
	}
}

func TestAgentTokenSetupRunsOnlyInAClone(t *testing.T) {
	a := newAgentRig(t)
	outside := filepath.Join(a.dir, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	r := a.run(outside, "", []string{"GIT_CEILING_DIRECTORIES=" + a.dir}, "agent-token.sh", "setup")
	if r.code != 2 || !strings.Contains(r.stderr, "inside the clone") {
		t.Fatalf("exit %d, stderr %q; want exit 2", r.code, r.stderr)
	}
}

func TestAgentTokenWhoami(t *testing.T) {
	a := newAgentRig(t)
	r := a.run(".", "", nil, "agent-token.sh", "whoami")
	mustOK(t, r)
	if r.stdout != "omniglass-agent[bot]\nhyperscaleav/omniglass\n" {
		t.Fatalf("whoami = %q", r.stdout)
	}
}

// ---- gh ----------------------------------------------------------------------

// fakeGhDir holds a gh that reports the credentials it was run with.
func fakeGhDir(t *testing.T, a *agentRig) string {
	t.Helper()
	dir := filepath.Join(a.dir, "realbin")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"$GH_TOKEN|${GITHUB_TOKEN:-unset}|$*\"\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func pathOf(env []string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			return strings.TrimPrefix(kv, "PATH=")
		}
	}
	return ""
}

func TestAgentGhShimRunsTheRealGhAsTheApp(t *testing.T) {
	a := newAgentRig(t)
	shimDir, err := filepath.Abs("agent-bin")
	if err != nil {
		t.Fatal(err)
	}
	path := shimDir + ":" + fakeGhDir(t, a) + ":" + pathOf(a.env)
	r := a.run(".", "", []string{"PATH=" + path, "GH_TOKEN=architect", "GITHUB_TOKEN=architect"}, "agent-bin/gh", "api", "/user")
	mustOK(t, r)
	if got := strings.TrimSpace(r.stdout); got != "ghs_fake1|unset|api /user" {
		t.Fatalf("real gh saw %q, want the App token, no GITHUB_TOKEN, and the args", got)
	}
}

func TestAgentGhShimNamesAMissingRealGh(t *testing.T) {
	a := newAgentRig(t)
	shimDir, err := filepath.Abs("agent-bin")
	if err != nil {
		t.Fatal(err)
	}
	// Only the shim's own dir and the basics: no gh anywhere else.
	bare := filepath.Join(a.dir, "bare")
	if err := os.Mkdir(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"bash", "dirname", "basename"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skip(tool, "missing")
		}
		if err := os.Symlink(p, filepath.Join(bare, tool)); err != nil {
			t.Fatal(err)
		}
	}
	r := a.run(".", "", []string{"PATH=" + shimDir + ":" + bare}, "agent-bin/gh", "api", "/user")
	if r.code != 127 || !strings.Contains(r.stderr, "no gh on PATH besides this shim") {
		t.Fatalf("exit %d, stderr %q; want 127 naming the missing gh", r.code, r.stderr)
	}
	if a.gh.mints() != 0 {
		t.Fatal("minted a token with no gh to give it to")
	}
}
