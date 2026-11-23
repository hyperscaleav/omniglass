package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Identity links (#2362, ADR-2361 point 2): a person signs in by single
// sign-on only through a link to their identity at the provider, iss|sub.
// Email never links. An admin links another person under user:link-identity
// (all scope) behind the takeover guard a password reset takes, since a link
// an admin chooses is a way to sign in as its target; a person links their own
// identity with their current password, through a sign-in at the provider
// bound to their session, which is an owner's only path. Unlinking takes the
// same gate. Every link and unlink is audited.

type identityLinkBody struct {
	ID         string     `json:"id" doc:"The link's id, which unlinking names"`
	Identifier string     `json:"identifier" doc:"The identity at the provider: its issuer and subject, iss|sub, exactly as its id_tokens carry them"`
	Issuer     string     `json:"issuer" doc:"The identifier's issuer half"`
	Subject    string     `json:"subject" doc:"The identifier's subject half"`
	LinkedBy   string     `json:"linked_by,omitempty" doc:"The username of whoever made the link: the person themselves or an administrator; absent once that user is purged"`
	Self       bool       `json:"self" doc:"True when the person made the link themselves"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" doc:"When the person last signed in through it"`
	// RefreshAcceptedAt is when the provider last accepted the link's refresh.
	RefreshAcceptedAt *time.Time `json:"refresh_accepted_at,omitempty" doc:"When the provider last accepted the link's refresh token, at sign-in or at a re-check"`
}

func toIdentityLinkBody(l storage.IdentityLink) identityLinkBody {
	iss, sub, _ := strings.Cut(l.Identifier, "|")
	return identityLinkBody{ID: l.ID, Identifier: l.Identifier, Issuer: iss, Subject: sub, LinkedBy: l.LinkedByUsername,
		Self: l.LinkedBy == l.UserID, CreatedAt: l.CreatedAt, LastUsedAt: l.LastUsedAt, RefreshAcceptedAt: l.RefreshAcceptedAt}
}

type identityLinksOutput struct {
	Body struct {
		Links []identityLinkBody `json:"links" nullable:"false"`
	}
}

type identityLinkOutput struct {
	Body identityLinkBody
}

type createIdentityLinkInput struct {
	ID   string `path:"id" doc:"The person, by uuid or username"`
	Body struct {
		Identifier string `json:"identifier" minLength:"3" maxLength:"2048" doc:"The identity at the provider as iss|sub: the issuer exactly as its id_tokens carry it, a bar, and the subject verbatim (the provider's admin console shows it, and an unlinked sign-in names it)"`
	}
}

type identityLinkPathInput struct {
	ID   string `path:"id" doc:"The person, by uuid or username"`
	Link string `path:"link" doc:"The link's id"`
}

type startSelfLinkInput struct {
	Body struct {
		Provider        string `json:"provider,omitempty" default:"default" doc:"The identity provider to link, by name"`
		CurrentPassword string `json:"current_password" doc:"Your current password: a link outlives the session that makes it, so it is confirmed as a password change is"`
	}
}

type startSelfLinkOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      struct {
		URL string `json:"url" doc:"The provider's sign-in page to send the browser to; signing in there links that identity to you and returns to your profile"`
	}
}

type unlinkSelfInput struct {
	Link string `path:"link" doc:"The link's id"`
	Body struct {
		CurrentPassword string `json:"current_password" doc:"Your current password, as linking asks"`
	}
}

// validIdentifier holds an identifier to the iss|sub shape: an https (or
// http) issuer URL and a non-empty subject.
func validIdentifier(id string) bool {
	iss, sub, ok := strings.Cut(id, "|")
	return ok && sub != "" && (strings.HasPrefix(iss, "https://") || strings.HasPrefix(iss, "http://")) && len(iss) > len("https://")
}

func mapIdentityLinkErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrIdentityLinked):
		return huma.Error409Conflict("that identity is already linked to a person")
	case errors.Is(err, storage.ErrIdentityLinkNotFound):
		return huma.Error404NotFound("identity link not found")
	}
	return mapUserErr(err)
}

// linkGuard is the takeover guard a link or an unlink takes on another
// person: not yourself, never an owner, and only a person whose capabilities
// your all-scope grants cover, exactly as a password reset.
func (a *authenticator) linkGuard(ctx context.Context, ref string) (*storage.User, error) {
	id, err := a.resolveUserRef(ctx, ref)
	if err != nil {
		return nil, err
	}
	if actorID(ctx) == id {
		return nil, huma.Error422UnprocessableEntity("link your own identity from your profile, which confirms your current password")
	}
	target, err := a.gw.GetUser(ctx, id, a.scopeFor(ctx, "user", "link-identity"))
	if err != nil {
		return nil, mapUserErr(err)
	}
	if target.Person == nil {
		return nil, huma.Error422UnprocessableEntity("only people sign in through an identity provider")
	}
	switch err := a.checkTakeoverGuard(ctx, target); {
	case errors.Is(err, errOwnerTarget):
		return nil, huma.Error403Forbidden("an owner's identity links are their own: an owner links from their profile")
	case errors.Is(err, errCapabilityEscalation):
		return nil, huma.Error403Forbidden("cannot link a person whose capabilities exceed yours")
	case err != nil:
		return nil, huma.Error500InternalServerError("link identity")
	}
	actor, _ := userFrom(ctx)
	if ok, err := a.allScopeCovers(ctx, actor, target); err != nil {
		return nil, huma.Error500InternalServerError("link identity")
	} else if !ok {
		return nil, huma.Error403Forbidden("linking this person requires all-scope authority over their capabilities")
	}
	return target, nil
}

// confirmPassword checks the caller's current password, as a password change does.
func (a *authenticator) confirmPassword(ctx context.Context, pr *storage.User, password string) error {
	if pr.Person == nil {
		return huma.Error422UnprocessableEntity("only people sign in through an identity provider")
	}
	if _, err := a.gw.AuthenticatePassword(ctx, pr.Person.Username, password); err != nil {
		if errors.Is(err, storage.ErrBadCredentials) || errors.Is(err, storage.ErrAccountLocked) {
			return huma.Error403Forbidden("current password is incorrect")
		}
		return huma.Error500InternalServerError("confirm password")
	}
	return nil
}

func registerIdentityLinkRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-user-identity-links",
		Method:      http.MethodGet,
		Path:        "/users/{id}/identity-links",
		Summary:     "List a person's identity links",
		Description: "Lists the identities at the identity provider a person signs in through (single sign-on), each iss|sub with who made it and when it was last used. Gated by user:read:admin (all-scope), as the user directory is.",
	}, "user", "read", "admin"), func(ctx context.Context, in *userPathInput) (*identityLinksOutput, error) {
		id, err := a.resolveUserRef(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if _, err := gw.GetUser(ctx, id, a.scopeFor(ctx, "user", "read")); err != nil {
			return nil, mapUserErr(err)
		}
		ls, err := gw.ListIdentityLinks(ctx, id)
		if err != nil {
			return nil, err
		}
		out := &identityLinksOutput{}
		out.Body.Links = make([]identityLinkBody, 0, len(ls))
		for _, l := range ls {
			out.Body.Links = append(out.Body.Links, toIdentityLinkBody(l))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-user-identity-link",
		Method:        http.MethodPost,
		Path:          "/users/{id}/identity-links",
		DefaultStatus: http.StatusCreated,
		Summary:       "Link a person to an identity at the provider",
		Description:   "Links another person to an identity at the identity provider (iss|sub), so they sign in through single sign-on as that person. Email never links. Gated by user:link-identity (all-scope) and the takeover guard a password reset takes: refused on yourself (link from your profile), on an owner (an owner links their own identity), or on a person whose capabilities exceed yours. An identity already linked to anyone is a 409. Audited, the link recording who made it; granting the person owner later deletes it.",
		Errors:        []int{http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity},
	}, "user", "link-identity"), func(ctx context.Context, in *createIdentityLinkInput) (*identityLinkOutput, error) {
		target, err := a.linkGuard(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		ident := strings.TrimSpace(in.Body.Identifier)
		if !validIdentifier(ident) {
			return nil, huma.Error422UnprocessableEntity("identifier must be the provider's issuer URL, a bar, and the subject: https://login.example.com|248289761001")
		}
		l, err := gw.LinkIdentity(ctx, actorID(ctx), target.ID, ident)
		if err != nil {
			return nil, mapIdentityLinkErr(err)
		}
		actor, _ := userFrom(ctx)
		if actor != nil && actor.Person != nil {
			l.LinkedByUsername = actor.Person.Username
		}
		return &identityLinkOutput{Body: toIdentityLinkBody(*l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-user-identity-link",
		Method:        http.MethodDelete,
		Path:          "/users/{id}/identity-links/{link}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Unlink a person from an identity at the provider",
		Description:   "Removes one of another person's identity links; every session and token they signed in through it ends with it. Gated by user:link-identity (all-scope) and the same takeover guard as linking. Audited.",
		Errors:        []int{http.StatusForbidden, http.StatusNotFound},
	}, "user", "link-identity"), func(ctx context.Context, in *identityLinkPathInput) (*struct{}, error) {
		target, err := a.linkGuard(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if err := gw.UnlinkIdentity(ctx, actorID(ctx), target.ID, in.Link); err != nil {
			return nil, mapIdentityLinkErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-auth-me-identity-links",
		Method:      http.MethodGet,
		Path:        "/auth/me/identity-links",
		Summary:     "List your own identity links",
		Description: "Lists the identities at the identity provider you sign in through. Requires authentication; self-scoped (never another person's).",
		Middlewares: huma.Middlewares{a.authn},
	}, func(ctx context.Context, _ *struct{}) (*identityLinksOutput, error) {
		pr, ok := userFrom(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("unauthenticated")
		}
		ls, err := gw.ListIdentityLinks(ctx, pr.ID)
		if err != nil {
			return nil, err
		}
		out := &identityLinksOutput{}
		out.Body.Links = make([]identityLinkBody, 0, len(ls))
		for _, l := range ls {
			out.Body.Links = append(out.Body.Links, toIdentityLinkBody(l))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "start-auth-me-identity-link",
		Method:      http.MethodPost,
		Path:        "/auth/me/identity-links:start",
		Summary:     "Start linking your own identity at the provider",
		Description: "Confirms your current password and starts a sign-in at the identity provider bound to this session: send the browser to the returned url, and signing in there links that identity to you (an owner's only way to gain single sign-on). Requires authentication; self-scoped (it only ever links to you). Needs a session; refused to an agent token. A wrong password is a 403; single sign-on not configured is a 409.",
		Errors:      []int{http.StatusForbidden, http.StatusConflict},
		Middlewares: huma.Middlewares{a.authn},
	}, func(ctx context.Context, in *startSelfLinkInput) (*startSelfLinkOutput, error) {
		pr, ok := userFrom(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("unauthenticated")
		}
		if impersonationMode(ctx) != "" {
			return nil, huma.Error403Forbidden("cannot link an identity while impersonating")
		}
		if err := a.confirmPassword(ctx, pr, in.Body.CurrentPassword); err != nil {
			return nil, err
		}
		sess, _ := sessionHashFrom(ctx)
		provider := in.Body.Provider
		if provider == "" {
			provider = storage.SSOProviderDefault
		}
		to, cookie, err := a.beginOIDC(ctx, provider, originFrom(ctx), pr.ID, sess)
		switch {
		case errors.Is(err, errSSOOff):
			return nil, huma.Error409Conflict("single sign-on is not configured on this server")
		case err != nil:
			return nil, huma.Error503ServiceUnavailable("the identity provider is unreachable or misconfigured")
		}
		out := &startSelfLinkOutput{SetCookie: cookie}
		out.Body.URL = to
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "unlink-auth-me-identity-link",
		Method:        http.MethodPost,
		Path:          "/auth/me/identity-links/{link}:unlink",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Unlink one of your own identity links",
		Description:   "Confirms your current password and removes one of your identity links; every session and token through it ends with it. Requires authentication; self-scoped (a link that is not yours is a 404). Refused to an agent token. Audited.",
		Errors:        []int{http.StatusForbidden, http.StatusNotFound},
		Middlewares:   huma.Middlewares{a.authn},
	}, func(ctx context.Context, in *unlinkSelfInput) (*struct{}, error) {
		pr, ok := userFrom(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("unauthenticated")
		}
		if err := a.confirmPassword(ctx, pr, in.Body.CurrentPassword); err != nil {
			return nil, err
		}
		if err := gw.UnlinkIdentity(ctx, pr.ID, pr.ID, in.Link); err != nil {
			return nil, mapIdentityLinkErr(err)
		}
		return nil, nil
	})
}
