package storage

import (
	"context"
	"encoding/json"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/egress"
	"time"

	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/sim/impair"
)

// UnimplementedGateway is a no-op Gateway for tests and codegen stubs that must
// satisfy the interface without a real backend. Embed it and override only the
// methods the case exercises. A new Gateway method lands here once, so stubs do
// not break as the interface grows.
type UnimplementedGateway struct{}

func (UnimplementedGateway) Ping(context.Context) error { return nil }
func (UnimplementedGateway) UpsertLabelRuleDefault(context.Context, string, string) error {
	return nil
}
func (UnimplementedGateway) ListLabelRules(context.Context) ([]LabelRule, error) { return nil, nil }
func (UnimplementedGateway) GetLabelRule(context.Context, string) (*LabelRule, error) {
	return nil, nil
}
func (UnimplementedGateway) SetLabelRule(context.Context, string, string, string) (*LabelRule, error) {
	return nil, nil
}
func (UnimplementedGateway) PreviewLabelRecompute(context.Context, string, scope.Set, scope.Set) ([]LabelChange, error) {
	return nil, nil
}
func (UnimplementedGateway) RecomputeLabels(context.Context, string, string, scope.Set, scope.Set) ([]LabelChange, error) {
	return nil, nil
}
func (UnimplementedGateway) RenderComponentDraftLabel(context.Context, ComponentLabelDraft, scope.Set, scope.Set, scope.Set, scope.Set) (DraftLabel, error) {
	return DraftLabel{}, nil
}
func (UnimplementedGateway) RenderSystemDraftLabel(context.Context, SystemLabelDraft, scope.Set, scope.Set) (DraftLabel, error) {
	return DraftLabel{}, nil
}
func (UnimplementedGateway) RenderLocationDraftLabel(context.Context, LocationLabelDraft, scope.Set) (DraftLabel, error) {
	return DraftLabel{}, nil
}
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

func (UnimplementedGateway) ResolveUsername(context.Context, string) (string, error) {
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
func (UnimplementedGateway) RenameGroup(context.Context, string, string, string, scope.Set) (*Group, error) {
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
func (UnimplementedGateway) RevokeBearerByID(context.Context, string, string, string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) RevokeBearersByPurpose(context.Context, string, string, string) (int, error) {
	return 0, nil
}
func (UnimplementedGateway) RevokeBearersByPurposeExcept(context.Context, string, string, string, [][]byte) (int, error) {
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
func (UnimplementedGateway) GetLocationType(context.Context, string) (*LocationType, error) {
	return nil, nil
}
func (UnimplementedGateway) RestoreLocationType(context.Context, string, string) (*LocationType, error) {
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
func (UnimplementedGateway) ResolveActionTarget(context.Context, string, string, scope.Set, scope.Set) (string, error) {
	return "", nil
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
func (UnimplementedGateway) RenameLocation(context.Context, string, string, string, scope.Set, scope.Set) (*Location, error) {
	return nil, nil
}
func (UnimplementedGateway) MoveLocation(context.Context, string, string, LocationMove, scope.Set, scope.Set) (*Location, error) {
	return nil, nil
}
func (UnimplementedGateway) ResetLocationName(context.Context, string, string, scope.Set, scope.Set) (*Location, error) {
	return nil, nil
}
func (UnimplementedGateway) LocationNameTaken(context.Context, string, *string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) DeleteLocation(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) UpsertStandard(context.Context, Standard) error { return nil }
func (UnimplementedGateway) ForkStandard(context.Context, string, StandardFork) (*Standard, error) {
	return nil, nil
}
func (UnimplementedGateway) ListStandards(context.Context) ([]Standard, error) {
	return nil, nil
}
func (UnimplementedGateway) GetStandard(context.Context, string) (*Standard, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateStandard(context.Context, string, Standard) (*Standard, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateStandard(context.Context, string, string, StandardPatch) (*Standard, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteStandard(context.Context, string, string) error { return nil }
func (UnimplementedGateway) ListSystems(context.Context, scope.Set) ([]System, error) {
	return nil, nil
}
func (UnimplementedGateway) GetSystem(context.Context, string, scope.Set) (*System, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateSystem(context.Context, string, SystemSpec, scope.Set, scope.Set) (*System, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateSystem(context.Context, string, string, SystemPatch, scope.Set, scope.Set) (*System, error) {
	return nil, nil
}
func (UnimplementedGateway) RenameSystem(context.Context, string, string, string, scope.Set, scope.Set) (*System, error) {
	return nil, nil
}
func (UnimplementedGateway) MoveSystem(context.Context, string, string, SystemMove, scope.Set, scope.Set, scope.Set) (*System, error) {
	return nil, nil
}
func (UnimplementedGateway) ResetSystemName(context.Context, string, string, scope.Set, scope.Set) (*System, error) {
	return nil, nil
}
func (UnimplementedGateway) SystemNameTaken(context.Context, string, *string, *string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) DeleteSystem(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListComponents(context.Context, scope.Set) ([]Component, error) {
	return nil, nil
}
func (UnimplementedGateway) GetComponent(context.Context, string, scope.Set) (*Component, error) {
	return nil, nil
}
func (UnimplementedGateway) ListComponentEndpoints(context.Context, string) ([]ComponentEndpoint, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateComponent(context.Context, string, ComponentSpec, scope.Set, scope.Set, scope.Set, scope.Set) (*Component, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateComponent(context.Context, string, string, ComponentPatch, scope.Set, scope.Set) (*Component, error) {
	return nil, nil
}
func (UnimplementedGateway) RenameComponent(context.Context, string, string, string, scope.Set, scope.Set) (*Component, error) {
	return nil, nil
}
func (UnimplementedGateway) MoveComponent(context.Context, string, string, ComponentMove, scope.Set, scope.Set, scope.Set) (*Component, error) {
	return nil, nil
}
func (UnimplementedGateway) ResetComponentName(context.Context, string, string, scope.Set, scope.Set) (*Component, error) {
	return nil, nil
}
func (UnimplementedGateway) ComponentNameTaken(context.Context, string, *string, *string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) DeleteComponent(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListEndpoints(context.Context, scope.Set) ([]Endpoint, error) {
	return nil, nil
}
func (UnimplementedGateway) GetEndpoint(context.Context, string, scope.Set) (*Endpoint, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateEndpoint(context.Context, string, EndpointSpec, scope.Set) (*Endpoint, error) {
	return nil, nil
}
func (UnimplementedGateway) RetryEndpoint(context.Context, string, string, RetryPin, scope.Set, scope.Set) (*Endpoint, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateEndpoint(context.Context, string, string, EndpointPatch, scope.Set, scope.Set) (*Endpoint, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteEndpoint(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListTasks(context.Context, scope.Set) ([]Task, error) {
	return nil, nil
}
func (UnimplementedGateway) GetTask(context.Context, string, scope.Set) (*Task, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertPropertyType(context.Context, PropertyType) error {
	return nil
}
func (UnimplementedGateway) ListPropertyTypes(context.Context) ([]PropertyType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertEventType(context.Context, EventType) error { return nil }
func (UnimplementedGateway) ListEventTypes(context.Context) ([]EventType, error) {
	return nil, nil
}
func (UnimplementedGateway) CatalogFingerprint(context.Context) (string, error) {
	return "", nil
}
func (UnimplementedGateway) CommandTargetedProperties(context.Context) (map[string]bool, error) {
	return nil, nil
}

func (UnimplementedGateway) GetEventType(context.Context, string) (*EventType, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateEventType(context.Context, string, EventTypeSpec) (*EventType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateEventType(context.Context, string, string, EventTypePatch) (*EventType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteEventType(context.Context, string, string) error {
	return nil
}
func (UnimplementedGateway) UpsertCommandType(context.Context, CommandType) error { return nil }
func (UnimplementedGateway) ListCommandTypes(context.Context) ([]CommandType, error) {
	return nil, nil
}
func (UnimplementedGateway) GetCommandType(context.Context, string) (*CommandType, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateCommandType(context.Context, string, CommandTypeSpec) (*CommandType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateCommandType(context.Context, string, string, CommandTypePatch) (*CommandType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteCommandType(context.Context, string, string) error {
	return nil
}
func (UnimplementedGateway) IssueCommand(context.Context, string, string, string, string, string, json.RawMessage, json.RawMessage) (*Command, error) {
	return nil, nil
}
func (UnimplementedGateway) CommandSettlement(context.Context, string, string, string, string) (SettlementVerdict, error) {
	return "", nil
}
func (UnimplementedGateway) GetComponentCommand(context.Context, string, int64) (*CommandRecord, error) {
	return nil, nil
}
func (UnimplementedGateway) ListComponentCommands(context.Context, string, CommandFilter) ([]CommandRecord, error) {
	return nil, nil
}
func (UnimplementedGateway) GetPropertyType(context.Context, string) (*PropertyType, error) {
	return nil, nil
}
func (UnimplementedGateway) CreatePropertyType(context.Context, string, PropertyTypeSpec) (*PropertyType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdatePropertyType(context.Context, string, string, PropertyTypePatch) (*PropertyType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeletePropertyType(context.Context, string, string) error {
	return nil
}
func (UnimplementedGateway) UpsertMetricType(context.Context, MetricType) error {
	return nil
}
func (UnimplementedGateway) ListMetricTypes(context.Context) ([]MetricType, error) {
	return nil, nil
}
func (UnimplementedGateway) GetMetricType(context.Context, string) (*MetricType, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateMetricType(context.Context, string, MetricTypeSpec) (*MetricType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateMetricType(context.Context, string, string, MetricTypePatch) (*MetricType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteMetricType(context.Context, string, string) error {
	return nil
}
func (UnimplementedGateway) InsertMetricSamples(context.Context, KeyWriter, []MetricSampleWrite) error {
	return nil
}
func (UnimplementedGateway) LatestMetric(context.Context, string, string) (*MetricSample, error) {
	return nil, nil
}
func (UnimplementedGateway) LatestMetricInstance(context.Context, string, string, string) (*MetricSample, error) {
	return nil, nil
}
func (UnimplementedGateway) InsertPropertySamples(context.Context, KeyWriter, []PropertySampleWrite) error {
	return nil
}
func (UnimplementedGateway) LatestProperty(context.Context, string, string, string) (*PropertySample, error) {
	return nil, nil
}
func (UnimplementedGateway) EndpointKeyPin(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (UnimplementedGateway) PropertyTransitions(context.Context, string, string, string, time.Duration) ([]PropertySample, error) {
	return nil, nil
}
func (UnimplementedGateway) StampEndpointsSeen(context.Context, []string) error {
	return nil
}
func (UnimplementedGateway) ComponentVerdict(context.Context, string) (string, error) {
	return "", nil
}
func (UnimplementedGateway) InsertEvents(context.Context, KeyWriter, []EventWrite) error {
	return nil
}
func (UnimplementedGateway) ListComponentEvents(context.Context, string, time.Duration, int) ([]Event, error) {
	return nil, nil
}
func (UnimplementedGateway) InsertLogLines(context.Context, []LogLineWrite) error {
	return nil
}
func (UnimplementedGateway) InsertNodeLogs(context.Context, []NodeLogWrite) error {
	return nil
}
func (UnimplementedGateway) LandBatch(context.Context, IngestBatch) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) PruneTelemetryReceipts(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}
func (UnimplementedGateway) ListComponentLogSources(context.Context, string, time.Duration) ([]string, error) {
	return nil, nil
}
func (UnimplementedGateway) ListComponentLogs(context.Context, string, time.Duration, int, LogFilter) ([]LogLine, error) {
	return nil, nil
}
func (UnimplementedGateway) ListNodeLogs(context.Context, string, time.Duration, int) ([]LogLine, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateNode(context.Context, string, NodeSpec, scope.Set, scope.Set) (*Node, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateNode(context.Context, string, string, NodePatch, scope.Set, scope.Set, scope.Set) (*Node, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteNode(context.Context, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) SetEnrollmentToken(context.Context, string, string, string, scope.Set) (*Node, error) {
	return nil, nil
}
func (UnimplementedGateway) ClaimNode(context.Context, string, string) (*Node, error) {
	return nil, nil
}
func (UnimplementedGateway) AuthenticateNode(context.Context, string, string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) RecordHeartbeat(context.Context, string) error { return nil }
func (UnimplementedGateway) RecordOrphanedBatches(context.Context, string, int64, time.Time) error {
	return nil
}
func (UnimplementedGateway) NodeWorklist(context.Context, string) (Worklist, error) {
	return Worklist{}, nil
}
func (UnimplementedGateway) PendingNodeCommands(context.Context, string) ([]CommandDelivery, error) {
	return nil, nil
}
func (UnimplementedGateway) RecordCommandExecution(context.Context, string, int64, string, string) error {
	return nil
}
func (UnimplementedGateway) SweepCommandSettlements(context.Context) error { return nil }
func (UnimplementedGateway) GetSimImpairment(context.Context, string) (impair.Impairment, error) {
	return impair.Impairment{}, nil
}
func (UnimplementedGateway) SetSimImpairment(context.Context, string, string, impair.Impairment) (impair.Impairment, error) {
	return impair.Impairment{}, nil
}
func (UnimplementedGateway) ComponentSimNode(context.Context, string) (string, error) {
	return "", nil
}
func (UnimplementedGateway) SimAddressesInUse(context.Context) (map[string]bool, error) {
	return nil, nil
}
func (UnimplementedGateway) AuditSimOperation(context.Context, string, string, any) error { return nil }
func (UnimplementedGateway) CreateSimSystem(context.Context, string, SimSystemSpec, SimSystemScopes) (*SimSystem, error) {
	return nil, nil
}
func (UnimplementedGateway) WithSimAddressLock(_ context.Context, fn func() error) error {
	return fn()
}
func (UnimplementedGateway) NodeSimDevices(context.Context, string, scope.Set) ([]SimDevice, error) {
	return nil, nil
}
func (UnimplementedGateway) ResolveTaskOwner(context.Context, string, string) (TaskOwner, bool, error) {
	return TaskOwner{}, false, nil
}
func (UnimplementedGateway) GetNode(context.Context, string, scope.Set) (*Node, error) {
	return nil, nil
}
func (UnimplementedGateway) ListNodes(context.Context, scope.Set) ([]Node, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertVendor(context.Context, Vendor) error    { return nil }
func (UnimplementedGateway) ListVendors(context.Context) ([]Vendor, error) { return nil, nil }
func (UnimplementedGateway) GetVendor(context.Context, string) (*Vendor, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateVendor(context.Context, string, Vendor) (*Vendor, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateVendor(context.Context, string, string, VendorPatch) (*Vendor, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteVendor(context.Context, string, string) error { return nil }
func (UnimplementedGateway) UpsertDriver(context.Context, Driver) error         { return nil }
func (UnimplementedGateway) ListDrivers(context.Context) ([]Driver, error)      { return nil, nil }
func (UnimplementedGateway) GetDriver(context.Context, string) (*Driver, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateDriver(context.Context, string, Driver) (*Driver, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateDriver(context.Context, string, string, DriverPatch) (*Driver, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteDriver(context.Context, string, string) error { return nil }

func (UnimplementedGateway) UpsertComponentType(context.Context, ComponentType) error { return nil }
func (UnimplementedGateway) ListComponentTypes(context.Context) ([]ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) GetComponentType(context.Context, string) (*ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateComponentType(context.Context, string, ComponentType) (*ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateComponentType(context.Context, string, string, ComponentTypePatch) (*ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) RestoreComponentType(context.Context, string, string) (*ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteComponentType(context.Context, string, string) error { return nil }
func (UnimplementedGateway) ResolveTypeFacts(context.Context, uuid.UUID) (string, string, string, []string, error) {
	return "", "", "", nil, nil
}
func (UnimplementedGateway) TypeIsWithin(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (UnimplementedGateway) UpsertSystemType(context.Context, SystemType) error { return nil }
func (UnimplementedGateway) ListSystemTypes(context.Context) ([]SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) GetSystemType(context.Context, string) (*SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateSystemType(context.Context, string, SystemType) (*SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateSystemType(context.Context, string, string, SystemTypePatch) (*SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSystemType(context.Context, string, string) error { return nil }
func (UnimplementedGateway) ResolveSystemTypeFacts(context.Context, uuid.UUID) (string, string, string, error) {
	return "", "", "", nil
}

func (UnimplementedGateway) UpsertProduct(context.Context, Product) error    { return nil }
func (UnimplementedGateway) ListProducts(context.Context) ([]Product, error) { return nil, nil }
func (UnimplementedGateway) GetProduct(context.Context, string) (*Product, error) {
	return nil, nil
}
func (UnimplementedGateway) CreateProduct(context.Context, string, Product) (*Product, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateProduct(context.Context, string, string, ProductPatch) (*Product, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteProduct(context.Context, string, string) error { return nil }
func (UnimplementedGateway) RenameRegistry(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (UnimplementedGateway) UpsertSecretType(context.Context, SecretType) error { return nil }
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
func (UnimplementedGateway) UpdateSecret(context.Context, string, string, SecretPatch, scope.Set, scope.Set, bool, bool) (*Secret, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSecret(context.Context, string, string, scope.Set, scope.Set, bool, bool) error {
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
func (UnimplementedGateway) UpdateVariable(context.Context, string, string, VariablePatch, scope.Set, scope.Set, bool) (*Variable, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteVariable(context.Context, string, string, scope.Set, scope.Set, bool) error {
	return nil
}
func (UnimplementedGateway) ResolveVariables(context.Context, string, scope.Set) ([]ResolvedVariable, error) {
	return nil, nil
}
func (UnimplementedGateway) ListProductProperties(context.Context, string) ([]ProductProperty, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertProductProperty(context.Context, string, ProductPropertySpec) error {
	return nil
}
func (UnimplementedGateway) SetProductProperty(context.Context, string, string, ProductPropertySpec) (*ProductProperty, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteProductProperty(context.Context, string, string, string) error {
	return nil
}
func (UnimplementedGateway) SetProperty(context.Context, string, string, string, string, string, json.RawMessage, scope.Set, scope.Set) (*Property, error) {
	return nil, nil
}
func (UnimplementedGateway) ClearProperty(context.Context, string, string, string, string, string, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) EffectiveProperties(context.Context, string, string, scope.Set) ([]EffectiveProperty, error) {
	return nil, nil
}
func (UnimplementedGateway) ListProductMetrics(context.Context, string) ([]ProductMetric, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertProductMetric(context.Context, string, ProductMetricSpec) error {
	return nil
}
func (UnimplementedGateway) SetProductMetric(context.Context, string, string, ProductMetricSpec) (*ProductMetric, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteProductMetric(context.Context, string, string, string) error {
	return nil
}
func (UnimplementedGateway) ListStandardMetrics(context.Context, string) ([]StandardMetric, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertStandardMetric(context.Context, string, StandardMetricSpec) error {
	return nil
}
func (UnimplementedGateway) SetStandardMetric(context.Context, string, string, StandardMetricSpec) (*StandardMetric, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteStandardMetric(context.Context, string, string, string) error {
	return nil
}
func (UnimplementedGateway) ListLocationTypeMetrics(context.Context, string) ([]LocationTypeMetric, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertLocationTypeMetric(context.Context, string, LocationTypeMetricSpec) error {
	return nil
}
func (UnimplementedGateway) SetLocationTypeMetric(context.Context, string, string, LocationTypeMetricSpec) (*LocationTypeMetric, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteLocationTypeMetric(context.Context, string, string, string) error {
	return nil
}
func (UnimplementedGateway) EffectiveMetrics(context.Context, string, string, scope.Set) ([]EffectiveMetric, error) {
	return nil, nil
}
func (UnimplementedGateway) LatestValue(context.Context, string, string, string, string, string, scope.Set) (*CurrentValue, error) {
	return nil, nil
}
func (UnimplementedGateway) PruneSamples(context.Context, string, time.Time, int) (int64, error) {
	return 0, nil
}
func (UnimplementedGateway) RetentionWindows(context.Context) (settings.RetentionSettings, error) {
	return settings.RetentionSettings{}, nil
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
func (UnimplementedGateway) UpdateTag(context.Context, string, string, TagPatch, scope.Set) (*Tag, error) {
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
func (UnimplementedGateway) ResolveTags(context.Context, string, string, scope.Set) ([]ResolvedTag, error) {
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
func (UnimplementedGateway) GetSettingOverrides(context.Context, string) ([]SettingOverride, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertSettingOverride(context.Context, string, string, string, map[string]any, []string) (*SettingOverride, error) {
	return nil, nil
}
func (UnimplementedGateway) MergePatchSettingOverride(context.Context, string, string, string, map[string]any) (*SettingOverride, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSettingOverride(context.Context, string, string, string) error {
	return nil
}
func (UnimplementedGateway) DeleteAllSettingOverrides(context.Context, string, string) error {
	return nil
}
func (UnimplementedGateway) Close() {}

func (UnimplementedGateway) ListStandardProperties(context.Context, string) ([]StandardProperty, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertStandardProperty(context.Context, string, StandardPropertySpec) error {
	return nil
}
func (UnimplementedGateway) SetStandardProperty(context.Context, string, string, StandardPropertySpec) (*StandardProperty, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteStandardProperty(context.Context, string, string, string) error {
	return nil
}
func (UnimplementedGateway) ListLocationTypeProperties(context.Context, string) ([]LocationTypeProperty, error) {
	return nil, nil
}
func (UnimplementedGateway) UpsertLocationTypeProperty(context.Context, string, LocationTypePropertySpec) error {
	return nil
}
func (UnimplementedGateway) SetLocationTypeProperty(context.Context, string, string, LocationTypePropertySpec) (*LocationTypeProperty, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteLocationTypeProperty(context.Context, string, string, string) error {
	return nil
}

func (UnimplementedGateway) EffectiveRoles(context.Context, string, scope.Set) ([]EffectiveRole, error) {
	return nil, nil
}
func (UnimplementedGateway) ListMembers(context.Context, string, scope.Set) ([]Member, error) {
	return nil, nil
}
func (UnimplementedGateway) ComponentMemberships(context.Context, string, scope.Set) ([]Member, error) {
	return nil, nil
}
func (UnimplementedGateway) AddMember(context.Context, string, string, string, scope.Set, scope.Set, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) RemoveMember(context.Context, string, string, string, scope.Set, scope.Set, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) SetPrimaryMember(context.Context, string, string, string, scope.Set, scope.Set, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) AssignRole(context.Context, string, string, string, string, scope.Set, scope.Set, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) UnassignRole(context.Context, string, string, string, string, scope.Set, scope.Set, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) SwapPositions(context.Context, string, string, string, int, int, scope.Set, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListSystemRoles(context.Context, string, string) ([]SystemRole, error) {
	return nil, nil
}
func (UnimplementedGateway) SetSystemRole(context.Context, string, string, string, SystemRoleSpec) (*SystemRole, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSystemRole(context.Context, string, string, string, string) error {
	return nil
}
func (UnimplementedGateway) SeedSystemRole(context.Context, string, string, SystemRoleSpec) error {
	return nil
}
func (UnimplementedGateway) SeedRoleChoice(context.Context, string, string, RoleChoiceSpec) (map[string]string, error) {
	return nil, nil
}
func (UnimplementedGateway) ResolveAlternate(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (UnimplementedGateway) DeleteChoice(context.Context, string, string, string, string) error {
	return nil
}
func (UnimplementedGateway) DeleteAlternate(context.Context, string, string, string, string, string) error {
	return nil
}
func (UnimplementedGateway) RaiseAlarm(context.Context, string, string, AlarmSpec) (*Alarm, error) {
	return nil, nil
}
func (UnimplementedGateway) ClearAlarm(context.Context, string, string, string) error { return nil }
func (UnimplementedGateway) AlarmTimeline(context.Context, string, string) ([]AlarmTimelineEntry, error) {
	return nil, nil
}
func (UnimplementedGateway) AcknowledgeAlarm(context.Context, string, string, string) (*Alarm, error) {
	return nil, nil
}
func (UnimplementedGateway) ListAlarms(context.Context, string, AlarmFilter) ([]Alarm, error) {
	return nil, nil
}
func (UnimplementedGateway) SystemHealth(context.Context, string, time.Duration, scope.Set) (*HealthReport, error) {
	return nil, nil
}
func (UnimplementedGateway) SystemVerdicts(context.Context, scope.Set) ([]SystemVerdict, error) {
	return nil, nil
}
func (UnimplementedGateway) LocationHealth(context.Context, string, time.Duration, scope.Set) (*HealthReport, error) {
	return nil, nil
}

func (UnimplementedGateway) FleetProjection(context.Context, scope.Set, scope.Set, scope.Set) (*FleetView, error) {
	return nil, nil
}

func (UnimplementedGateway) ListSystemEvents(context.Context, string, time.Duration, int) ([]Event, error) {
	return nil, nil
}

func (UnimplementedGateway) ListSystemAlarms(context.Context, string, time.Duration, int) ([]Alarm, error) {
	return nil, nil
}

func (UnimplementedGateway) ListSystemLogSources(context.Context, string, time.Duration) ([]string, error) {
	return nil, nil
}
func (UnimplementedGateway) ListSystemLogs(context.Context, string, time.Duration, int, LogFilter) ([]LogLine, error) {
	return nil, nil
}

func (UnimplementedGateway) ListMetricSeries(context.Context, string, string, string, time.Duration, int) ([]MetricSample, error) {
	return nil, nil
}

func (UnimplementedGateway) ListPropertySeries(context.Context, string, string, string, time.Duration, int) ([]PropertySample, error) {
	return nil, nil
}

func (UnimplementedGateway) UpsertSeverityLevel(context.Context, SeverityLevel) error { return nil }
func (UnimplementedGateway) ListSeverityLevels(context.Context) ([]SeverityLevel, error) {
	return nil, nil
}
func (UnimplementedGateway) GetSeverityLevel(context.Context, string) (*SeverityLevel, error) {
	return nil, ErrSeverityLevelNotFound
}
func (UnimplementedGateway) CreateSeverityLevel(context.Context, string, SeverityLevel, scope.Set) (*SeverityLevel, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateSeverityLevel(context.Context, string, string, SeverityLevelPatch, scope.Set) (*SeverityLevel, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSeverityLevel(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListEventRules(context.Context, scope.Set) ([]EventRule, error) {
	return nil, nil
}
func (UnimplementedGateway) GetEventRule(context.Context, string, scope.Set) (*EventRule, error) {
	return nil, ErrEventRuleNotFound
}
func (UnimplementedGateway) CreateEventRule(context.Context, string, EventRuleSpec, scope.Set) (*EventRule, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateEventRule(context.Context, string, string, EventRulePatch, scope.Set) (*EventRule, error) {
	return nil, nil
}
func (UnimplementedGateway) RenameEventRule(context.Context, string, string, string, scope.Set) (*EventRule, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteEventRule(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListCalcRules(context.Context) ([]CalcRule, error) { return nil, nil }
func (UnimplementedGateway) GetCalcRule(context.Context, string) (*CalcRule, error) {
	return nil, ErrCalcRuleNotFound
}
func (UnimplementedGateway) CreateCalcRule(context.Context, string, CalcRuleSpec, scope.Set) (*CalcRule, error) {
	return nil, ErrCalcRuleInvalid
}
func (UnimplementedGateway) UpdateCalcRule(context.Context, string, string, CalcRulePatch, scope.Set) (*CalcRule, error) {
	return nil, ErrCalcRuleNotFound
}
func (UnimplementedGateway) RenameCalcRule(context.Context, string, string, string, scope.Set) (*CalcRule, error) {
	return nil, ErrCalcRuleNotFound
}
func (UnimplementedGateway) DrainCalcPending(context.Context, int) (CalcDrain, error) {
	return CalcDrain{}, nil
}
func (UnimplementedGateway) CalcRuleCounts(context.Context, scope.Set, scope.Set) (map[string]CalcCounts, error) {
	return map[string]CalcCounts{}, nil
}
func (UnimplementedGateway) CalcRuleState(context.Context, string, scope.Set, scope.Set, int) (*CalcRuleLive, error) {
	return nil, ErrCalcRuleNotFound
}
func (UnimplementedGateway) PreviewCalcRule(context.Context, CalcRuleSpec, scope.Set, scope.Set) (*CalcPreview, error) {
	return nil, ErrCalcRuleInvalid
}
func (UnimplementedGateway) DeleteCalcRule(context.Context, string, string, scope.Set) error {
	return ErrCalcRuleNotFound
}
func (UnimplementedGateway) SweepEventRules(context.Context, time.Time) error { return nil }
func (UnimplementedGateway) ReconcileEventRules(context.Context, *RuleReconcileState) error {
	return nil
}
func (UnimplementedGateway) UpsertEventRule(context.Context, EventRuleSpec) error { return nil }
func (UnimplementedGateway) ResolveSampleKey(context.Context, string) (SampleKeyRef, error) {
	return SampleKeyRef{}, ErrSampleKeyNotFound
}
func (UnimplementedGateway) SampleKeyType(context.Context, string) (rules.Lane, string, error) {
	return "", "", ErrSampleKeyNotFound
}

func (UnimplementedGateway) PreviewEventRule(context.Context, RulePreviewSpec, scope.Set) (*RulePreview, error) {
	return nil, ErrEventRuleNotFound
}

func (UnimplementedGateway) EventRuleState(context.Context, string, scope.Set) (*EventRuleLive, error) {
	return nil, ErrEventRuleNotFound
}

func (UnimplementedGateway) ResolveWebhook(context.Context, []byte) (*WebhookTarget, error) {
	return nil, ErrWebhookNotFound
}
func (UnimplementedGateway) RotateWebhookToken(context.Context, string, string, scope.Set, scope.Set) (*Endpoint, error) {
	return nil, ErrEndpointNotFound
}
func (UnimplementedGateway) GetEndpointWebhook(context.Context, string) (*EndpointWebhook, error) {
	return nil, ErrEndpointNotFound
}
func (UnimplementedGateway) RecordWebhookRefusals(context.Context, []WebhookRefusal) error {
	return nil
}
func (UnimplementedGateway) RecordUnclaimedSources(context.Context, []UnclaimedSource) error {
	return nil
}
func (UnimplementedGateway) ListUnclaimedSources(context.Context) ([]UnclaimedSource, error) {
	return nil, nil
}
func (UnimplementedGateway) ClaimUnclaimedSource(context.Context, string, string, string) (bool, error) {
	return false, nil
}
func (UnimplementedGateway) DriverSpecFindings(context.Context, []byte) ([]driver.Finding, error) {
	return nil, nil
}
func (UnimplementedGateway) PlanDriverPreview(context.Context, string, PreviewInput) (*PreviewPlan, error) {
	return nil, nil
}
func (UnimplementedGateway) PlanEndpointRun(context.Context, string, string, string, scope.Set, scope.Set) (*EndpointPoll, error) {
	return nil, nil
}
func (UnimplementedGateway) ComponentEndpointNames(context.Context, string) ([]string, error) {
	return nil, nil
}

func (UnimplementedGateway) UpsertNotificationType(context.Context, NotificationType) error {
	return nil
}
func (UnimplementedGateway) ListNotificationTypes(context.Context) ([]NotificationType, error) {
	return nil, nil
}
func (UnimplementedGateway) GetNotificationType(context.Context, string) (*NotificationType, error) {
	return nil, ErrNotificationTypeNotFound
}
func (UnimplementedGateway) CreateNotificationType(context.Context, string, NotificationTypeSpec) (*NotificationType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateNotificationType(context.Context, string, string, NotificationTypePatch) (*NotificationType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteNotificationType(context.Context, string, string) error { return nil }
func (UnimplementedGateway) PreviewNotificationType(context.Context, string, string, string, string, *string, *string) (*NotificationPreview, error) {
	return nil, ErrNotificationTypeNotFound
}

func (UnimplementedGateway) ListNotificationTargets(context.Context, scope.Set) ([]NotificationTarget, error) {
	return nil, nil
}
func (UnimplementedGateway) GetNotificationTarget(context.Context, string, scope.Set) (*NotificationTarget, error) {
	return nil, ErrNotificationTargetNotFound
}
func (UnimplementedGateway) CreateNotificationTarget(context.Context, string, NotificationTargetSpec, scope.Set, SecretUse) (*NotificationTarget, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateNotificationTarget(context.Context, string, string, NotificationTargetPatch, scope.Set, SecretUse) (*NotificationTarget, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteNotificationTarget(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) RevealNotificationTargetURL(context.Context, string, string, scope.Set) (string, error) {
	return "", ErrNotificationTargetNotFound
}
func (UnimplementedGateway) TargetForTest(context.Context, string, scope.Set) (*TargetSend, error) {
	return nil, ErrNotificationTargetNotFound
}
func (UnimplementedGateway) AuditTargetTest(context.Context, string, string, int, bool, string) error {
	return nil
}
func (UnimplementedGateway) EgressPolicy(context.Context) (egress.Policy, error) {
	return egress.Policy{}, nil
}

func (UnimplementedGateway) ListActionRules(context.Context) ([]ActionRule, error) { return nil, nil }
func (UnimplementedGateway) GetActionRule(context.Context, string) (*ActionRule, error) {
	return nil, ErrActionRuleNotFound
}
func (UnimplementedGateway) CreateActionRule(context.Context, string, ActionRuleSpec, scope.Set) (*ActionRule, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateActionRule(context.Context, string, string, ActionRulePatch, scope.Set) (*ActionRule, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteActionRule(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ListNotifications(context.Context, NotificationFilter, scope.Set) ([]Notification, error) {
	return nil, nil
}
func (UnimplementedGateway) GetNotification(context.Context, string, scope.Set) (*Notification, error) {
	return nil, ErrNotificationNotFound
}
func (UnimplementedGateway) RetryNotification(context.Context, string, string, scope.Set, scope.Set) (*Notification, error) {
	return nil, ErrNotificationNotFound
}
func (UnimplementedGateway) DeliverNext(context.Context, SendFunc) (bool, error) { return false, nil }
func (UnimplementedGateway) NotificationBacklog(context.Context) (int, error)    { return 0, nil }
