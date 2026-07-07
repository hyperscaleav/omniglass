package storage

import "context"

// realActorKey is the context key under which the API sets the real actor behind
// an impersonated request. It is exported (as RealActorContextKey) because the
// API's authn middleware sets it while the gateway's audit writer reads it, both
// on the request context; the real actor is a request-scoped ambient identity
// (like a trace id), so it rides the context rather than threading a parameter
// through every mutating gateway method.
type realActorKey struct{}

// RealActorContextKey is the context key carrying the real actor principal id when
// a request is impersonated. Set it via WithRealActor (or huma.WithValue in
// middleware); the audit writer records it as the row's real_actor_principal_id.
var RealActorContextKey = realActorKey{}

// WithRealActor returns a context carrying the real actor principal id behind an
// impersonated request. An empty id leaves the audit real_actor null.
func WithRealActor(ctx context.Context, realActorID string) context.Context {
	return context.WithValue(ctx, RealActorContextKey, realActorID)
}

// realActorFrom reads the real actor principal id from the context, empty if none.
func realActorFrom(ctx context.Context) string {
	id, _ := ctx.Value(RealActorContextKey).(string)
	return id
}
