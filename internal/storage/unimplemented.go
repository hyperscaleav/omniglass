package storage

import (
	"context"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// UnimplementedGateway is a no-op Gateway for tests and codegen stubs that must
// satisfy the interface without a real backend. Embed it and override only the
// methods the case exercises. A new Gateway method lands here once, so stubs do
// not break as the interface grows.
type UnimplementedGateway struct{}

func (UnimplementedGateway) Ping(context.Context) error             { return nil }
func (UnimplementedGateway) UpsertRole(context.Context, Role) error { return nil }
func (UnimplementedGateway) BootstrapOwner(context.Context, OwnerSpec) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) AuthenticateBearer(context.Context, []byte) (*Principal, error) {
	return nil, nil
}
func (UnimplementedGateway) ListRoles(context.Context) ([]Role, error) { return nil, nil }
func (UnimplementedGateway) UpsertLocationType(context.Context, LocationType) error {
	return nil
}
func (UnimplementedGateway) ListLocationTypes(context.Context) ([]LocationType, error) {
	return nil, nil
}
func (UnimplementedGateway) ListLocations(context.Context, scope.Set) ([]Location, error) {
	return nil, nil
}
func (UnimplementedGateway) GetLocation(context.Context, string, scope.Set) (*Location, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateLocation(context.Context, string, LocationSpec, scope.Set) (*Location, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateLocation(context.Context, string, string, LocationPatch, scope.Set, scope.Set) (*Location, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteLocation(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) Close() {}
