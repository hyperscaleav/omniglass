package api_test

import (
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/egress"
)

// An MCP client signs in by its client id metadata document (#2074): its
// client_id is an https URL serving its metadata, so it never registers. The
// server fetches the document through the egress contract, holds it to the
// URL it was fetched from, and sends codes only to the redirect URIs it lists.
// The document server here is a TLS test server on loopback, which the egress
// contract refuses unless the install's allow list names it. Skipped under
// -short.

// docServer serves a client metadata document at /client.json and counts the
// requests that reach it.
type docServer struct {
	srv  *httptest.Server
	hits atomic.Int32
	mu   sync.Mutex
	doc  any
}

func newDocServer(t *testing.T) *docServer {
	t.Helper()
	d := &docServer{}
	d.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.hits.Add(1)
		d.mu.Lock()
		doc := d.doc
		d.mu.Unlock()
		if r.URL.Path != "/client.json" || doc == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(d.srv.Close)
	return d
}

// url is the client id: the document's own URL.
func (d *docServer) url() string { return d.srv.URL + "/client.json" }

func (d *docServer) serve(doc any) {
	d.mu.Lock()
	d.doc = doc
	d.mu.Unlock()
}

// clientDoc is a well-formed document for clientID.
func clientDoc(clientID string, redirects ...string) map[string]any {
	if len(redirects) == 0 {
		redirects = []string{oauthRedirect}
	}
	return map[string]any{
		"client_id": clientID, "client_name": "Doc Client", "redirect_uris": redirects,
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none",
	}
}

// cimdStack is an MCP stack whose egress trusts the document server's
// certificate; allow adds loopback to the install's egress allow list.
func cimdStack(t *testing.T, d *docServer, allow bool) *mcpStack {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(d.srv.Certificate())
	s := newMCPStack(t, api.WithEgressOptions(egress.Options{RootCAs: roots}))
	if allow {
		s.c.do(s.owner, http.MethodPatch, "/settings/notification", map[string]any{"egress_allow": []string{"127.0.0.1"}}, http.StatusOK)
	}
	return s
}

// authorizeStatus sends a browser to /oauth/authorize as clientID asking for
// redirect, returning the status and where it was sent.
func (s *mcpStack) authorizeStatus(t *testing.T, clientID, redirect string) (int, string) {
	t.Helper()
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
		"code_challenge": {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"}, "code_challenge_method": {"S256"}, "state": {"st"},
	}
	resp, err := noRedirect.Get(s.base + "/oauth/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Location")
}

func TestMCPClientSignsInByMetadataDocument(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	d.serve(clientDoc(d.url()))
	s := cimdStack(t, d, true)

	// The authorization server says it takes a metadata document URL.
	var meta map[string]any
	if err := json.Unmarshal(getBody(t, s.base+"/.well-known/oauth-authorization-server"), &meta); err != nil {
		t.Fatal(err)
	}
	if meta["client_id_metadata_document_supported"] != true {
		t.Fatalf("server metadata = %v, want client_id_metadata_document_supported", meta)
	}

	h, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		ClientIDMetadataDocumentConfig: &auth.ClientIDMetadataDocumentConfig{URL: d.url()},
		RedirectURL:                    oauthRedirect,
		AuthorizationCodeFetcher:       oauthHandlerFetcher(consentAs(s, s.owner, []string{"operator"}, true)),
	})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := s.oauthConnect(h)
	if err != nil {
		t.Fatalf("connect by metadata document: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	n := 0
	for _, err := range cs.Tools(s.ctx, nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		n++
	}
	if n < 40 {
		t.Fatalf("listed %d tools, want the catalog", n)
	}

	// The consent page names the client by the host serving its document, the
	// one name it cannot choose; the document was fetched once and is cached.
	code, loc := s.authorizeStatus(t, d.url(), oauthRedirect)
	if code != http.StatusFound {
		t.Fatalf("authorize again = %d, want the consent page", code)
	}
	page, _ := url.Parse(loc)
	var client struct {
		ClientName   string `json:"client_name"`
		MetadataHost string `json:"metadata_host"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/auth/oauth-clients/"+page.Query().Get("client_id"), nil, http.StatusOK), &client); err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(d.srv.URL, "https://")
	if client.ClientName != "Doc Client" || client.MetadataHost != host {
		t.Errorf("consent client = %+v, want Doc Client served from %s", client, host)
	}
	if got := d.hits.Load(); got != 1 {
		t.Errorf("the document was fetched %d times, want once (cached)", got)
	}

	// The client refreshes under its URL client id.
	ts, err := h.TokenSource(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatal(err)
	}
	got := s.tokenRequest(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}, "client_id": {d.url()}})
	if got.Status != http.StatusOK {
		t.Fatalf("refresh by URL client id = %d %s, want 200", got.Status, got.Error)
	}

	// The stored id the consent page carries is not the client's name: a
	// refresh under it is refused, so no grant skips the document's URL.
	if bad := s.tokenRequest(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {got.RefreshToken}, "client_id": {page.Query().Get("client_id")}}); bad.Status == http.StatusOK {
		t.Errorf("refresh by the stored id = 200, want refused")
	}
}

// A document that names another client id, or a redirect URI it does not
// list, is refused at authorize, never redirected: neither URI is trusted.
func TestMetadataDocumentRefusals(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	s := cimdStack(t, d, true)

	d.serve(clientDoc("https://elsewhere.example/client.json"))
	if code, loc := s.authorizeStatus(t, d.url(), oauthRedirect); code != http.StatusBadRequest || loc != "" {
		t.Errorf("a document naming another client id = %d %q, want 400 and no redirect", code, loc)
	}

	// The same URL serving its own client id is accepted, so the refusal
	// above was the document's, not the URL's.
	d.serve(clientDoc(d.url()))
	if code, _ := s.authorizeStatus(t, d.url(), oauthRedirect); code != http.StatusFound {
		t.Fatalf("a well-formed document = %d, want the consent page", code)
	}
	if code, loc := s.authorizeStatus(t, d.url(), "http://127.0.0.1:53682/elsewhere"); code != http.StatusBadRequest || loc != "" {
		t.Errorf("a redirect URI the document does not list = %d %q, want 400 and no redirect", code, loc)
	}

	// A metadata URL must be https with a path: plain http is not one.
	if code, _ := s.authorizeStatus(t, strings.Replace(d.url(), "https://", "http://", 1), oauthRedirect); code != http.StatusBadRequest {
		t.Errorf("an http client id = %d, want 400", code)
	}
}

// A document client is known by its URL alone: a document past its ten
// minutes is read again, and one that no longer holds refuses the client,
// whether named by its URL or by the stored id the consent page carries.
func TestMetadataDocumentStaleRefetchFailsClosed(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	d.serve(clientDoc(d.url()))
	s := cimdStack(t, d, true)

	code, loc := s.authorizeStatus(t, d.url(), oauthRedirect)
	if code != http.StatusFound {
		t.Fatalf("a well-formed document = %d, want the consent page", code)
	}
	page, _ := url.Parse(loc)
	rowID := page.Query().Get("client_id")
	if code, loc := s.authorizeStatus(t, rowID, oauthRedirect); code != http.StatusBadRequest || loc != "" {
		t.Errorf("a document client by its stored id = %d %q, want 400 and no redirect", code, loc)
	}

	conn, err := pgx.Connect(s.ctx, s.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(s.ctx)
	if _, err := conn.Exec(s.ctx, `UPDATE oauth_client SET fetched_at = now() - interval '11 minutes' WHERE metadata_url = $1`, d.url()); err != nil {
		t.Fatal(err)
	}
	// Consent straight to the stored id, never through authorize, does not
	// approve a document past its ten minutes.
	if code, out := s.c.send(s.owner, http.MethodPost, "/auth/oauth-clients/"+rowID+":authorize", map[string]any{
		"redirect_uri": oauthRedirect, "code_challenge": "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		"code_challenge_method": "S256", "resource": s.base + "/mcp", "roles": []string{"viewer"}, "approved": true,
	}); code != http.StatusUnprocessableEntity || !strings.Contains(string(out), "metadata document") {
		t.Errorf("consent to a stale document client = %d %s, want 422", code, out)
	}
	d.serve(clientDoc("https://elsewhere.example/client.json"))
	if code, loc := s.authorizeStatus(t, d.url(), oauthRedirect); code != http.StatusBadRequest || loc != "" {
		t.Errorf("a stale document that no longer holds = %d %q, want 400 and no redirect", code, loc)
	}
	if got := d.hits.Load(); got != 2 {
		t.Errorf("the document was fetched %d times, want twice (read again once stale)", got)
	}
}

// A client id resolving to a private address is refused without a request
// to it: the egress contract judges the dialed address before connecting.
func TestMetadataDocumentPrivateAddressNeverFetched(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	d.serve(clientDoc(d.url()))
	s := cimdStack(t, d, false)

	if code, loc := s.authorizeStatus(t, d.url(), oauthRedirect); code != http.StatusBadRequest || loc != "" {
		t.Errorf("a loopback client id = %d %q, want 400 and no redirect", code, loc)
	}
	if got := d.hits.Load(); got != 0 {
		t.Errorf("the loopback document server saw %d requests, want none", got)
	}
}

func getBody(t *testing.T, u string) []byte {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
