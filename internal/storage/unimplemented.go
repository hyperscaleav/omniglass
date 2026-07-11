package storage

import (
	"context"
	"time"

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
func (UnimplementedGateway) IssueBearerCredential(context.Context, string, []byte, string, *time.Time) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) AuthenticateBearer(context.Context, []byte) (*Principal, error) {
	return nil, nil
}
func (UnimplementedGateway) ResolvePrincipalRef(context.Context, string) (string, error) {
	return "", nil
}
func (UnimplementedGateway) BeginImpersonation(context.Context, string, string, string, time.Duration) (string, *ImpersonationSession, error) {
	return "", nil, nil
}
func (UnimplementedGateway) AuthenticateImpersonation(context.Context, []byte) (*Principal, string, string, string, error) {
	return nil, "", "", "", nil
}
func (UnimplementedGateway) EndImpersonation(context.Context, string) error { return nil }
func (UnimplementedGateway) AuthenticatePassword(context.Context, string, string) (*Principal, error) {
	return nil, nil
}
func (UnimplementedGateway) SetPassword(context.Context, string, string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) UpdateHumanProfile(context.Context, string, HumanProfilePatch) error {
	return nil
}
func (UnimplementedGateway) ListPrincipals(context.Context, scope.Set, bool) ([]Principal, error) {
	return nil, nil
}
func (UnimplementedGateway) GetPrincipal(context.Context, string, scope.Set) (*Principal, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateHumanPrincipal(context.Context, string, HumanSpec, scope.Set) (*Principal, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdatePrincipalHuman(context.Context, string, string, AdminHumanPatch, scope.Set) (*Principal, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateGrant(context.Context, string, string, GrantSpec, scope.Set) (*Grant, error) {
	return nil, nil
}
func (UnimplementedGateway) RevokeGrant(context.Context, string, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) CreateGroup(context.Context, string, GroupSpec, scope.Set) (*Group, error) {
	return nil, nil
}
func (UnimplementedGateway) ListGroups(context.Context, scope.Set) ([]Group, error) { return nil, nil }
func (UnimplementedGateway) GetGroup(context.Context, string, scope.Set) (*Group, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateGroup(context.Context, string, string, GroupPatch, scope.Set) (*Group, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteGroup(context.Context, string, string, scope.Set) error { return nil }
func (UnimplementedGateway) AddGroupMember(context.Context, string, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) RemoveGroupMember(context.Context, string, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListGroupMembers(context.Context, string, scope.Set) ([]GroupMember, error) {
	return nil, nil
}
func (UnimplementedGateway) ListGroupsForPrincipal(context.Context, string, scope.Set) ([]Group, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateGroupGrant(context.Context, string, string, GrantSpec, scope.Set) (*Grant, error) {
	return nil, nil
}
func (UnimplementedGateway) RevokeGroupGrant(context.Context, string, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListGroupGrants(context.Context, string, scope.Set) ([]Grant, error) {
	return nil, nil
}
func (UnimplementedGateway) SetPrincipalActive(context.Context, string, string, bool, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ArchivePrincipal(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) RestorePrincipal(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) PurgePrincipal(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) SetPrincipalPassword(context.Context, string, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) RevokePrincipalBearers(context.Context, string, [][]byte) (int, error) {
	return 0, nil
}
func (UnimplementedGateway) RevokeBearer(context.Context, []byte) error { return nil }
func (UnimplementedGateway) AnyHuman(context.Context) (bool, error)     { return false, nil }
func (UnimplementedGateway) ListRoles(context.Context) ([]Role, error) { return nil, nil }
func (UnimplementedGateway) ListAuditLog(context.Context, AuditFilter) ([]AuditEntry, error) {
	return nil, nil
}
func (UnimplementedGateway) WriteAuthEvent(context.Context, string, string) error { return nil }
func (UnimplementedGateway) UpsertLocationType(context.Context, LocationType) error {
	return nil
}
func (UnimplementedGateway) ListLocationTypes(context.Context) ([]LocationType, error) {
	return nil, nil
}
func (UnimplementedGateway) InScopeIDs(context.Context, string, []string, scope.Set) (map[string]bool, error) {
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
func (UnimplementedGateway) UpsertSystemType(context.Context, SystemType) error { return nil }
func (UnimplementedGateway) ListSystemTypes(context.Context) ([]SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) ListSystems(context.Context, scope.Set) ([]System, error) {
	return nil, nil
}
func (UnimplementedGateway) GetSystem(context.Context, string, scope.Set) (*System, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateSystem(context.Context, string, SystemSpec, scope.Set) (*System, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateSystem(context.Context, string, string, SystemPatch, scope.Set, scope.Set) (*System, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSystem(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) UpsertComponentType(context.Context, ComponentType) error { return nil }
func (UnimplementedGateway) ListComponentTypes(context.Context) ([]ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) ListComponents(context.Context, scope.Set) ([]Component, error) {
	return nil, nil
}
func (UnimplementedGateway) GetComponent(context.Context, string, scope.Set) (*Component, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateComponent(context.Context, string, ComponentSpec, scope.Set) (*Component, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateComponent(context.Context, string, string, ComponentPatch, scope.Set, scope.Set) (*Component, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteComponent(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) UpsertSecretType(context.Context, SecretType) error { return nil }
func (UnimplementedGateway) ListSecretTypes(context.Context) ([]SecretType, error) {
	return nil, nil
}
func (UnimplementedGateway) GetSecretType(context.Context, string) (*SecretType, error) {
	return nil, nil
}
func (UnimplementedGateway) ListSecrets(context.Context, scope.Set) ([]Secret, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateSecret(context.Context, string, SecretSpec, scope.Set) (*Secret, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateSecret(context.Context, string, string, map[string]string, scope.Set, scope.Set) (*Secret, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSecret(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) RevealSecret(context.Context, string, string, scope.Set, scope.Set) (map[string]string, error) {
	return nil, nil
}
func (UnimplementedGateway) CopySecret(context.Context, string, string, scope.Set, scope.Set) (map[string]string, error) {
	return nil, nil
}
func (UnimplementedGateway) ResolveSecrets(context.Context, string, scope.Set) ([]ResolvedSecret, error) {
	return nil, nil
}
func (UnimplementedGateway) Close() {}
