package storage

import (
	"context"
	"encoding/json"
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
func (UnimplementedGateway) IssueBearerCredential(context.Context, BearerIssue) (bool, error) {
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
func (UnimplementedGateway) SetOwnAvatar(context.Context, string, string) error { return nil }
func (UnimplementedGateway) ClearOwnAvatar(context.Context, string) error       { return nil }
func (UnimplementedGateway) SetPrincipalAvatar(context.Context, string, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ClearPrincipalAvatar(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) GetHumanAvatar(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (UnimplementedGateway) GetPrincipalAvatar(context.Context, string, scope.Set) (string, bool, error) {
	return "", false, nil
}
func (UnimplementedGateway) RevokeBearer(context.Context, []byte) error { return nil }
func (UnimplementedGateway) ListBearerCredentials(context.Context, string, []byte) ([]BearerCredential, error) {
	return nil, nil
}
func (UnimplementedGateway) RevokeBearerByID(context.Context, string, string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) RevokeBearersByPurpose(context.Context, string, string) (int, error) {
	return 0, nil
}
func (UnimplementedGateway) RevokeBearersByPurposeExcept(context.Context, string, string, [][]byte) (int, error) {
	return 0, nil
}
func (UnimplementedGateway) AnyHuman(context.Context) (bool, error)    { return false, nil }
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
func (UnimplementedGateway) CreateLocationType(context.Context, string, LocationType) (*LocationType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateLocationType(context.Context, string, string, LocationTypePatch) (*LocationType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteLocationType(context.Context, string, string) error { return nil }
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
func (UnimplementedGateway) LocationNameTaken(context.Context, string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) DeleteLocation(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) UpsertSystemType(context.Context, SystemType) error { return nil }
func (UnimplementedGateway) ListSystemTypes(context.Context) ([]SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateSystemType(context.Context, string, SystemType) (*SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateSystemType(context.Context, string, string, SystemTypePatch) (*SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSystemType(context.Context, string, string) error { return nil }
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
func (UnimplementedGateway) SystemNameTaken(context.Context, string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) DeleteSystem(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) UpsertComponentType(context.Context, ComponentType) error { return nil }
func (UnimplementedGateway) ListComponentTypes(context.Context) ([]ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateComponentType(context.Context, string, ComponentType) (*ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateComponentType(context.Context, string, string, ComponentTypePatch) (*ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteComponentType(context.Context, string, string) error { return nil }
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
func (UnimplementedGateway) ComponentNameTaken(context.Context, string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) DeleteComponent(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) UpsertComponentMake(context.Context, ComponentMake) error { return nil }
func (UnimplementedGateway) ListComponentMakes(context.Context) ([]ComponentMake, error) {
	return nil, nil
}
func (UnimplementedGateway) GetComponentMake(context.Context, string) (*ComponentMake, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateComponentMake(context.Context, string, ComponentMake) (*ComponentMake, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateComponentMake(context.Context, string, string, ComponentMakePatch) (*ComponentMake, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteComponentMake(context.Context, string, string) error { return nil }
func (UnimplementedGateway) UpsertSecretType(context.Context, SecretType) error        { return nil }
func (UnimplementedGateway) ListSecretTypes(context.Context) ([]SecretType, error) {
	return nil, nil
}
func (UnimplementedGateway) GetSecretType(context.Context, string) (*SecretType, error) {
	return nil, nil
}
func (UnimplementedGateway) ListSecrets(context.Context, scope.Set, bool) ([]Secret, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateSecret(context.Context, string, SecretSpec, scope.Set, bool) (*Secret, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateSecret(context.Context, string, string, map[string]string, scope.Set, scope.Set, bool) (*Secret, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSecret(context.Context, string, string, scope.Set, scope.Set, bool) error {
	return nil
}
func (UnimplementedGateway) RevealSecret(context.Context, string, string, scope.Set, scope.Set, bool) (map[string]string, error) {
	return nil, nil
}
func (UnimplementedGateway) CopySecret(context.Context, string, string, scope.Set, scope.Set, bool) (map[string]string, error) {
	return nil, nil
}
func (UnimplementedGateway) ResolveSecrets(context.Context, string, scope.Set, bool) ([]ResolvedSecret, error) {
	return nil, nil
}
func (UnimplementedGateway) ListVariables(context.Context, scope.Set) ([]Variable, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateVariable(context.Context, string, VariableSpec, scope.Set) (*Variable, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateVariable(context.Context, string, string, json.RawMessage, scope.Set, scope.Set) (*Variable, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteVariable(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ResolveVariables(context.Context, string, scope.Set) ([]ResolvedVariable, error) {
	return nil, nil
}
func (UnimplementedGateway) ListFieldDefinitions(context.Context) ([]FieldDefinition, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateFieldDefinition(context.Context, string, FieldDefinitionSpec) (*FieldDefinition, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateFieldDefinition(context.Context, string, string, string, json.RawMessage) (*FieldDefinition, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteFieldDefinition(context.Context, string, string) error {
	return nil
}
func (UnimplementedGateway) ListTags(context.Context) ([]Tag, error) {
	return nil, nil
}
func (UnimplementedGateway) DistinctTagValues(context.Context, string) ([]string, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateTag(context.Context, string, TagSpec, scope.Set) (*Tag, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateTag(context.Context, string, string, TagSpec, scope.Set) (*Tag, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteTag(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) SetTagBinding(context.Context, string, string, string, *string, string, scope.Set, scope.Set) (*TagBinding, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteTagBinding(context.Context, string, string, string, *string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListEntityTags(context.Context, string, *string, scope.Set) ([]TagBinding, error) {
	return nil, nil
}
func (UnimplementedGateway) ResolveTags(context.Context, string, scope.Set) ([]ResolvedTag, error) {
	return nil, nil
}
func (UnimplementedGateway) EffectiveTags(context.Context, string, []string) (map[string]map[string]string, error) {
	return nil, nil
}
func (UnimplementedGateway) ListFiles(context.Context, bool) ([]File, error) { return nil, nil }
func (UnimplementedGateway) GetFile(context.Context, string, bool) (*File, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateFile(context.Context, string, FileSpec, bool) (*File, error) {
	return nil, nil
}
func (UnimplementedGateway) DownloadFile(context.Context, string, bool) (*File, []byte, error) {
	return nil, nil, nil
}
func (UnimplementedGateway) DeleteFile(context.Context, string, string, bool) error { return nil }
func (UnimplementedGateway) Close()                                                 {}
