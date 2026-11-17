package api

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The consent half of OAuth sign-in (#2048, ADR-2048): the console's consent
// page reads the client and the roles the person may approve, then posts the
// person's answer with their session. The answer is where to send the browser
// back to: the client's redirect carrying a code, or access_denied.

type oauthClientInput struct {
	ID string `path:"id" doc:"The client id, from the authorization request"`
}

type oauthClientOutput struct {
	Body struct {
		ID           string   `json:"id"`
		ClientName   string   `json:"client_name" doc:"The name the client registered under; its token acts as this agent"`
		RedirectURIs []string `json:"redirect_uris" doc:"The exact URIs a code may be sent back to"`
		MetadataHost string   `json:"metadata_host,omitempty" doc:"The host serving the client's metadata document, when the client is named by one (#2074): the one part of its identity it cannot choose"`
		Roles        []string `json:"roles" doc:"The roles you may approve for the client: every role whose capabilities you hold"`
	}
}

type authorizeOAuthClientInput struct {
	ID   string `path:"id" doc:"The client id, from the authorization request"`
	Body struct {
		RedirectURI         string   `json:"redirect_uri" doc:"The redirect URI from the authorization request; one the client registered"`
		CodeChallenge       string   `json:"code_challenge" doc:"The PKCE challenge from the authorization request"`
		CodeChallengeMethod string   `json:"code_challenge_method" enum:"S256" doc:"The PKCE method; S256 only"`
		State               string   `json:"state,omitempty" doc:"The client's state, echoed back"`
		Resource            string   `json:"resource" doc:"The resource the token is for: this server's MCP endpoint"`
		Roles               []string `json:"roles,omitempty" doc:"The roles to approve; each must be one whose capabilities you hold. Required when approved"`
		Approved            bool     `json:"approved" doc:"Whether you approve the client; false sends it back with access_denied"`
	}
}

type authorizeOAuthClientOutput struct {
	Body struct {
		RedirectTo string `json:"redirect_to" doc:"Where to send the browser back to: the client's redirect with a code, or with access_denied"`
	}
}

func (a *authenticator) registerOAuthConsent(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-oauth-client",
		Method:      http.MethodGet,
		Path:        "/auth/oauth-clients/{id}",
		Summary:     "Read an OAuth client asking for your consent",
		Description: "Returns a registered OAuth client (the name it gave, its redirect URIs) and the roles you may approve for it, for the console's consent page. Requires authentication; the client is the one named in the authorization request.",
		Errors:      []int{http.StatusNotFound},
		Middlewares: huma.Middlewares{a.authn},
	}, a.getOAuthClientHandler)

	huma.Register(api, huma.Operation{
		OperationID: "authorize-oauth-client",
		Extensions:  plaintext(),
		Method:      http.MethodPost,
		Path:        "/auth/oauth-clients/{id}:authorize",
		Summary:     "Approve or deny an OAuth client",
		Description: "Answers an OAuth client's authorization request as you. Approved, it issues a one-time code (good for five minutes) the client exchanges for an agent token accepted on the MCP channel only and holding only the roles you chose, each one whose capabilities you hold (else 403); denied, it sends the client back with access_denied. Returns where to send the browser back to. Requires authentication; an agent token or an impersonated session cannot answer.",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
		Middlewares: huma.Middlewares{a.authn},
	}, a.authorizeOAuthClientHandler)
}

// approvableRoles are the roles whose capabilities the caller holds, sorted.
func (a *authenticator) approvableRoles(ctx context.Context) ([]string, error) {
	idx, err := a.roleIndex(ctx)
	if err != nil {
		return nil, err
	}
	perms, _ := permsFrom(ctx)
	out := []string{}
	for name := range idx {
		if perms.Covers(idx.Flatten([]string{name})) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (a *authenticator) oauthClient(ctx context.Context, id string) (storage.OAuthClient, error) {
	c, found, err := a.gw.GetOAuthClient(ctx, id)
	if err != nil {
		return storage.OAuthClient{}, huma.Error500InternalServerError("read oauth client")
	}
	if !found {
		return storage.OAuthClient{}, huma.Error404NotFound("no such OAuth client")
	}
	return c, nil
}

func (a *authenticator) getOAuthClientHandler(ctx context.Context, in *oauthClientInput) (*oauthClientOutput, error) {
	c, err := a.oauthClient(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	roles, err := a.approvableRoles(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("read oauth client")
	}
	out := &oauthClientOutput{}
	out.Body.ID, out.Body.ClientName, out.Body.RedirectURIs, out.Body.Roles = c.ID, c.Name, c.RedirectURIs, roles
	if u, err := url.Parse(c.MetadataURL); err == nil && c.MetadataURL != "" {
		out.Body.MetadataHost = u.Host
	}
	return out, nil
}

func (a *authenticator) authorizeOAuthClientHandler(ctx context.Context, in *authorizeOAuthClientInput) (*authorizeOAuthClientOutput, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	if pr.Human == nil {
		return nil, huma.Error422UnprocessableEntity("only a person can approve an OAuth client")
	}
	if impersonationMode(ctx) != "" {
		return nil, huma.Error403Forbidden("an impersonated session cannot approve an OAuth client; its person must")
	}
	c, err := a.oauthClient(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	// A document client's redirect URIs are what its document said when
	// authorize last read it; past its ten minutes that is not trusted.
	if c.MetadataURL != "" && time.Since(c.FetchedAt) >= oauthMetadataFresh {
		return nil, huma.Error422UnprocessableEntity("the client's metadata document is stale; start the sign-in again from the client")
	}
	b := in.Body
	if !registered(c, b.RedirectURI) {
		return nil, huma.Error422UnprocessableEntity("the redirect_uri is not one the client registered")
	}
	origin := originFrom(ctx)
	if b.Resource != mcpResource(origin) {
		return nil, huma.Error422UnprocessableEntity("the resource is not this server's MCP endpoint")
	}
	if b.CodeChallenge == "" || b.CodeChallengeMethod != "S256" {
		return nil, huma.Error422UnprocessableEntity("PKCE with the S256 method is required")
	}
	back := url.Values{"iss": {origin}}
	if b.State != "" {
		back.Set("state", b.State)
	}
	out := &authorizeOAuthClientOutput{}
	if !b.Approved {
		back.Set("error", "access_denied")
		back.Set("error_description", "the person denied the request")
		out.Body.RedirectTo = redirectWith(b.RedirectURI, back)
		return out, nil
	}
	if len(b.Roles) == 0 {
		return nil, huma.Error422UnprocessableEntity("approve at least one role for the client")
	}
	idx, err := a.roleIndex(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("authorize oauth client")
	}
	perms, _ := permsFrom(ctx)
	for _, r := range b.Roles {
		if _, ok := idx[r]; !ok {
			return nil, huma.Error422UnprocessableEntity("unknown role: " + r)
		}
		if !perms.Covers(idx.Flatten([]string{r})) {
			return nil, huma.Error403Forbidden("the role " + r + " grants a capability you do not hold")
		}
	}
	code, hash, err := newOAuthCode()
	if err != nil {
		return nil, huma.Error500InternalServerError("authorize oauth client")
	}
	if err := a.gw.CreateOAuthCode(ctx, storage.OAuthCode{
		Hash: hash, ClientID: c.ID, PrincipalID: pr.ID, RedirectURI: b.RedirectURI,
		CodeChallenge: b.CodeChallenge, Resource: b.Resource, Roles: b.Roles,
		ExpiresAt: time.Now().Add(oauthCodeLifetime),
	}); err != nil {
		return nil, huma.Error500InternalServerError("authorize oauth client")
	}
	back.Set("code", code)
	out.Body.RedirectTo = redirectWith(b.RedirectURI, back)
	return out, nil
}
