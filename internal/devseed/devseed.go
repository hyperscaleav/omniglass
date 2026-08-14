// Package devseed installs a small example estate (locations, users, grants, and a
// worked reachability check) for a dev environment, applied by `make dev` through the
// trusted direct-DB lane. It is idempotent: rows that already exist are left untouched,
// so it runs safely on every start.
//
// It is deliberately separate from the boot seed (internal/seed), which installs
// ship-with reference data on every server start in every environment. These are
// operator rows, not reference data, and they must NEVER run in production. The
// grants and locations here reference boot-seed reference data (roles, location
// types) by foreign key, so the boot seed must run first.
package devseed

import (
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"gopkg.in/yaml.v3"
)

//go:embed fixtures.yaml
var fixturesYAML []byte

// Doc is the parsed example-data fixture.
type Doc struct {
	Locations         []Location        `yaml:"locations"`
	Users             []User            `yaml:"users"`
	Variables         []Variable        `yaml:"variables"`
	Tags              []Tag             `yaml:"tags"`
	TagBindings       []TagBinding      `yaml:"tag_bindings"`
	Files             []File            `yaml:"files"`
	ProductProperties []ProductProperty `yaml:"product_properties"`
	Components        []Component       `yaml:"components"`
	Systems           []System          `yaml:"systems"`
	Members           []Member          `yaml:"members"`
	RoleAssignments   []RoleAssignment  `yaml:"role_assignments"`
	PropertyValues    []Property        `yaml:"property_values"`
}

// System is one example system: a thing in a room that either works or does not.
// Key is the fixture-local identity (see identity.go); Name is optional and its
// absence asks the platform to mint one from the system_type's stem. Standard
// names a boot-seeded blueprint whose roles it inherits live; SystemType names a
// boot-seeded coarse space classifier (what kind of space it is, a separate axis
// from the standard); Location names a fixture location BY KEY.
type System struct {
	Key         string `yaml:"key"`
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name"`
	Standard    string `yaml:"standard"`
	SystemType  string `yaml:"system_type"`
	Location    string `yaml:"location"`
}

// Member binds a component into a system without giving it a job. The dev estate
// needs the case explicitly, because it is the one an assignment cannot produce: a
// component that is in the room and accounted for while filling no declared role.
// Both ends are fixture KEYS.
type Member struct {
	System    string `yaml:"system"`
	Component string `yaml:"component"`
}

// RoleAssignment staffs one of a system's roles. It also produces membership as a
// side effect, which is the point: the seed shows both ways a component comes to
// be in a system. System and Component are fixture keys; Role names a role the
// system's standard declares.
type RoleAssignment struct {
	System    string `yaml:"system"`
	Role      string `yaml:"role"`
	Component string `yaml:"component"`
}

// ProductProperty is one line the dev estate adds to a product's declared-property
// contract: the schema half of the property primitive. Property names a row in the
// boot-seed property catalog (which owns the data type and validation). Default is
// decoded from YAML and, when present, re-encoded to jsonb (like a Variable); an
// omitted default leaves the line with none, so the seed can teach a
// default-vs-plain contrast.
type ProductProperty struct {
	Product  string `yaml:"product"`
	Property string `yaml:"property"`
	Default  any    `yaml:"default"`
	Required bool   `yaml:"required"`
}

// Component is one example device placed in the estate. Key is the
// fixture-local identity; Name is optional and its absence asks the platform to
// mint one from the product's resolved component_type stem. Location names a
// fixture location BY KEY, resolved to its id at seed time (empty for an
// unplaced component); Product names a catalog SKU, whose contract supplies the
// component's properties and whose classification supplies the name's stem. A
// system binding is omitted for now (optional on create).
type Component struct {
	Key         string `yaml:"key"`
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name"`
	Product     string `yaml:"product"`
	Location    string `yaml:"location"`
}

// PropertyValue is one example literal a component declares over its product's
// contract: an override so the effective-properties panel teaches
// direct-vs-inherited. Component is a fixture key. Value is decoded from YAML and
// re-encoded to jsonb, exactly like a Variable.
type Property struct {
	Component string `yaml:"component"`
	Property  string `yaml:"property"`
	Value     any    `yaml:"value"`
}

// File is one example file handle over the blob store: its bytes ride inline in
// the fixture (Content), hashed and deduplicated on create. Sensitive seeds a
// flagged file (admin-tier only) so the directory shows both kinds.
type File struct {
	Name        string `yaml:"name"`
	ContentType string `yaml:"content_type"`
	Content     string `yaml:"content"`
	Sensitive   bool   `yaml:"sensitive"`
}

// Tag is one example key in the governed vocabulary, optionally with a platform
// default value. Propagates is a pointer so an omitted field defaults to true (a
// tag cascades unless the fixture opts out).
type Tag struct {
	Name          string   `yaml:"name"`
	AppliesTo     []string `yaml:"applies_to"`
	Propagates    *bool    `yaml:"propagates"`
	PlatformValue string   `yaml:"platform_value"`
}

// TagBinding is one example scoped binding, setting a key's value at a fixture
// location so the effective-tags cascade comes up with an override to teach.
// Key is the TAG's name (the governed vocabulary); Location is a fixture key.
type TagBinding struct {
	Key      string `yaml:"key"`
	Location string `yaml:"location"`
	Value    string `yaml:"value"`
}

// Variable is one example platform variable (a macro). Value is decoded from YAML
// and re-encoded to jsonb, so `value: 30` seeds the number and `value: {a: 1}` the
// object. The platform tier keeps the fixture free of an owner dependency.
type Variable struct {
	Name      string `yaml:"name"`
	ValueType string `yaml:"value_type"`
	Value     any    `yaml:"value"`
}

// Location is one node of the example tree. Key is the fixture-local identity;
// Name is optional and its absence asks the platform to mint one from the
// location_type's name rule (only a POSITIONAL type has one, so a campus, a
// building and a room must each carry a name). Parent names a location declared
// earlier in the document BY KEY (empty for a root).
type Location struct {
	Key         string `yaml:"key"`
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name"`
	Type        string `yaml:"type"`
	Parent      string `yaml:"parent"`
}

// User is one example human principal with a known password and its grants.
type User struct {
	Username    string  `yaml:"username"`
	Password    string  `yaml:"password"`
	DisplayName string  `yaml:"display_name"`
	Grants      []Grant `yaml:"grants"`
}

// Grant assigns a role at a scope. ScopeRef names a fixture location BY KEY
// (resolved to its id at seed time) for a location-scoped grant; it is empty for
// the all scope.
type Grant struct {
	Role      string `yaml:"role"`
	ScopeKind string `yaml:"scope_kind"`
	ScopeRef  string `yaml:"scope_ref"`
	ScopeOp   string `yaml:"scope_op"`
}

// Fixtures parses the embedded example data. Exposed so a pure unit test can
// check the fixture's shape without a database.
func Fixtures() (Doc, error) {
	var doc Doc
	if err := yaml.Unmarshal(fixturesYAML, &doc); err != nil {
		return Doc{}, fmt.Errorf("devseed: parse fixtures: %w", err)
	}
	return doc, nil
}

// Run installs the example estate idempotently through the Storage Gateway.
// actorID is the audit actor for the created rows (empty for a system actor). The
// trusted lane grants the all scope; callers are the direct-DB commands, never a
// request handler.
func Run(ctx context.Context, gw storage.Gateway, actorID string) error {
	doc, err := Fixtures()
	if err != nil {
		return err
	}
	all := scope.Set{All: true}

	// Locations first, parents before children (the fixture is ordered so) so a
	// child's parent resolves. locIDs maps a fixture KEY to the row's id, which is
	// what every later reference (a grant's scope, a tag binding, a component's
	// placement) is resolved through: an id is a legal reference wherever a name
	// is (ADR-0062), and two of these locations are called `1`.
	locIDs := map[string]string{}
	locIndex, err := locationGenIndex(ctx, gw, all)
	if err != nil {
		return err
	}
	for _, l := range doc.Locations {
		var parentID string
		if l.Parent != "" {
			id, ok := locIDs[l.Parent]
			if !ok {
				return fmt.Errorf("devseed: location %q references unknown parent key %q", l.Key, l.Parent)
			}
			parentID = id
		}
		slot := genSlot{bucket: bucketOf(parentID, ""), class: l.Type}
		id, err := existingRowID(l.Name, l.Key, slot, locIndex, func(name string) (string, error) {
			existing, err := gw.GetLocation(ctx, name, all)
			if errors.Is(err, storage.ErrLocationNotFound) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			return existing.ID, nil
		})
		if err != nil {
			return fmt.Errorf("devseed: check location %q: %w", l.Key, err)
		}
		if id != "" {
			locIDs[l.Key] = id
			continue
		}
		// An empty Name asks the platform to name the row, which only a
		// positional location_type can answer (#687), and no shipped one is
		// (ADR-0103). So every location in this estate carries a name and the
		// spec below always supplies one; the components and the systems are
		// what demonstrate the generator.
		spec := storage.LocationSpec{Name: l.Name, DisplayName: l.DisplayName, LocationType: l.Type}
		if parentID != "" {
			p := parentID
			spec.ParentName = &p
		}
		created, err := gw.CreateLocation(ctx, actorID, spec, all)
		if err != nil {
			return fmt.Errorf("devseed: create location %q: %w", l.Key, err)
		}
		locIDs[l.Key] = created.ID
	}

	// Users next. A user that already exists (ErrUsernameTaken) is left as is,
	// grants included: those were created alongside the user on the first run, so a
	// re-run neither re-creates the user nor duplicates its grants. A user and its
	// grants are not one transaction (the Gateway has no cross-entity write), so an
	// infra fault between them could leave a user under-granted, and a re-run would
	// skip it. That is an accepted limit of dev-only fixture data: reset with
	// `docker compose down -v` and re-run `make dev`.
	for _, u := range doc.Users {
		hash, err := auth.HashPassword(u.Password)
		if err != nil {
			return fmt.Errorf("devseed: hash password for %q: %w", u.Username, err)
		}
		pr, err := gw.CreateHumanPrincipal(ctx, actorID, storage.HumanSpec{
			Username:     u.Username,
			DisplayName:  u.DisplayName,
			PasswordHash: hash,
		}, all)
		if errors.Is(err, storage.ErrUsernameTaken) {
			continue
		}
		if err != nil {
			return fmt.Errorf("devseed: create user %q: %w", u.Username, err)
		}
		for _, g := range u.Grants {
			spec := storage.GrantSpec{Role: g.Role, ScopeKind: g.ScopeKind, ScopeOp: g.ScopeOp}
			if g.ScopeKind != "all" {
				id, ok := locIDs[g.ScopeRef]
				if !ok {
					return fmt.Errorf("devseed: user %q grant references unknown location key %q", u.Username, g.ScopeRef)
				}
				spec.ScopeID = id
			}
			if _, err := gw.CreateGrant(ctx, actorID, pr.ID, spec, all); err != nil {
				return fmt.Errorf("devseed: grant %s to %q: %w", g.Role, u.Username, err)
			}
		}
	}

	// Platform variables: a couple of example macros so the Variables directory
	// comes up populated. A variable that already exists (ErrVariableExists) is
	// left as is, so a re-run adds nothing.
	for _, v := range doc.Variables {
		raw, err := json.Marshal(v.Value)
		if err != nil {
			return fmt.Errorf("devseed: encode variable %q: %w", v.Name, err)
		}
		_, err = gw.CreateVariable(ctx, actorID, storage.VariableSpec{
			Name: v.Name, ValueType: v.ValueType, OwnerKind: "platform", Value: raw,
		}, all)
		if errors.Is(err, storage.ErrVariableExists) {
			continue
		}
		if err != nil {
			return fmt.Errorf("devseed: create variable %q: %w", v.Name, err)
		}
	}

	// Tag keys, then their example bindings, so the Tags vocabulary and the
	// effective-tags cascade come up populated. A key that already exists
	// (ErrTagExists) is left as is; a binding is an upsert, so a re-run is a no-op.
	for _, tg := range doc.Tags {
		propagates := true
		if tg.Propagates != nil {
			propagates = *tg.Propagates
		}
		_, err := gw.CreateTag(ctx, actorID, storage.TagSpec{
			Name: tg.Name, AppliesTo: tg.AppliesTo, Propagates: propagates,
		}, all)
		if err != nil && !errors.Is(err, storage.ErrTagExists) {
			return fmt.Errorf("devseed: create tag %q: %w", tg.Name, err)
		}
		if tg.PlatformValue != "" {
			if _, err := gw.SetTagBinding(ctx, actorID, tg.Name, "platform", nil, tg.PlatformValue, all, all); err != nil {
				return fmt.Errorf("devseed: set platform tag %q: %w", tg.Name, err)
			}
		}
	}
	for _, b := range doc.TagBindings {
		loc, ok := locIDs[b.Location]
		if !ok {
			return fmt.Errorf("devseed: tag %q binds at unknown location key %q", b.Key, b.Location)
		}
		if _, err := gw.SetTagBinding(ctx, actorID, b.Key, "location", &loc, b.Value, all, all); err != nil {
			return fmt.Errorf("devseed: bind tag %q at %q: %w", b.Key, b.Location, err)
		}
	}

	// Files: a few example handles so the Files directory comes up populated.
	// A file handle has no natural unique key (the id is a uuid; the name is not
	// unique), so a plain re-create would duplicate rows; skip a fixture whose
	// name is already present to keep the seed idempotent. canAdmin is true here
	// (the seed runs at system scope) so a sensitive fixture can be created.
	existingFiles, err := gw.ListFiles(ctx, true)
	if err != nil {
		return fmt.Errorf("devseed: list files: %w", err)
	}
	haveFile := make(map[string]bool, len(existingFiles))
	for _, f := range existingFiles {
		haveFile[f.Name] = true
	}
	for _, f := range doc.Files {
		if haveFile[f.Name] {
			continue
		}
		if _, err := gw.CreateFile(ctx, actorID, storage.FileSpec{
			Name: f.Name, ContentType: f.ContentType, Data: []byte(f.Content), Sensitive: f.Sensitive,
		}, true); err != nil {
			return fmt.Errorf("devseed: create file %q: %w", f.Name, err)
		}
	}
	// Product contract lines: an extra property the dev estate declares on a catalog
	// product, so the contract editor comes up with an operator-added line beside the
	// boot-seed ones. The product and the property catalog must already be seeded (the
	// boot seed runs first). A default is encoded to jsonb like a variable; an omitted
	// default stays nil. The upsert is keyed on (product, property), so a re-run
	// rewrites the same row rather than adding one.
	for _, pp := range doc.ProductProperties {
		spec := storage.ProductPropertySpec{PropertyTypeName: pp.Property, Required: pp.Required}
		if pp.Default != nil {
			raw, err := json.Marshal(pp.Default)
			if err != nil {
				return fmt.Errorf("devseed: encode contract default %s/%s: %w", pp.Product, pp.Property, err)
			}
			spec.DefaultValue = raw
		}
		if err := gw.UpsertProductProperty(ctx, pp.Product, spec); err != nil {
			return fmt.Errorf("devseed: declare contract %s/%s: %w", pp.Product, pp.Property, err)
		}
	}

	// Components: an example device placed in the estate, so the Components directory
	// comes up populated. Locations and products must already be seeded (above, and the
	// boot seed) for the placement and the product binding to resolve. None of them
	// names itself, so none can be looked up by name on a second run: each is
	// recognised by its position among the platform-named rows of its product's
	// classification in its own room (see identity.go).
	compIDs := map[string]string{}
	compIndex, err := componentGenIndex(ctx, gw, all)
	if err != nil {
		return err
	}
	for _, c := range doc.Components {
		var locID string
		if c.Location != "" {
			id, ok := locIDs[c.Location]
			if !ok {
				return fmt.Errorf("devseed: component %q references unknown location key %q", c.Key, c.Location)
			}
			locID = id
		}
		slot := genSlot{bucket: bucketOf("", locID), class: productClass(c.Product)}
		id, err := existingRowID(c.Name, c.Key, slot, compIndex, func(name string) (string, error) {
			existing, err := gw.GetComponent(ctx, name, all)
			if errors.Is(err, storage.ErrComponentNotFound) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			return existing.ID, nil
		})
		if err != nil {
			return fmt.Errorf("devseed: check component %q: %w", c.Key, err)
		}
		if id != "" {
			compIDs[c.Key] = id
			continue
		}
		spec := storage.ComponentSpec{Name: c.Name, DisplayName: c.DisplayName}
		if locID != "" {
			l := locID
			spec.LocationName = &l
		}
		if c.Product != "" {
			prod := c.Product
			spec.ProductName = &prod
		}
		created, err := gw.CreateComponent(ctx, actorID, spec, all, all, all, all)
		if err != nil {
			return fmt.Errorf("devseed: create component %q: %w", c.Key, err)
		}
		compIDs[c.Key] = created.ID
	}

	// Systems: the thing in the room that either works or does not, conforming to a
	// boot-seeded standard whose roles it inherits live. Locations must already be
	// seeded for the placement to resolve. Recognised the same way a component is,
	// with the system_type as the classification whose stem is minted.
	sysIDs := map[string]string{}
	sysIndex, err := systemGenIndex(ctx, gw, all)
	if err != nil {
		return err
	}
	for _, s := range doc.Systems {
		var locID string
		if s.Location != "" {
			id, ok := locIDs[s.Location]
			if !ok {
				return fmt.Errorf("devseed: system %q references unknown location key %q", s.Key, s.Location)
			}
			locID = id
		}
		slot := genSlot{bucket: bucketOf("", locID), class: s.SystemType}
		id, err := existingRowID(s.Name, s.Key, slot, sysIndex, func(name string) (string, error) {
			existing, err := gw.GetSystem(ctx, name, all)
			if errors.Is(err, storage.ErrSystemNotFound) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			return existing.ID, nil
		})
		if err != nil {
			return fmt.Errorf("devseed: check system %q: %w", s.Key, err)
		}
		if id != "" {
			sysIDs[s.Key] = id
			continue
		}
		spec := storage.SystemSpec{Name: s.Name, DisplayName: s.DisplayName}
		if s.Standard != "" {
			std := s.Standard
			spec.StandardID = &std
		}
		if s.SystemType != "" {
			st := s.SystemType
			spec.SystemTypeID = &st
		}
		if locID != "" {
			l := locID
			spec.LocationName = &l
		}
		created, err := gw.CreateSystem(ctx, actorID, spec, all, all)
		if err != nil {
			return fmt.Errorf("devseed: create system %q: %w", s.Key, err)
		}
		sysIDs[s.Key] = created.ID
	}

	// Members: a component bound into a system without a job, which is the case an
	// assignment cannot produce. AddMember is idempotent, so a re-run changes
	// nothing and no existence check is needed. Both ends are addressed by id,
	// because a generated name is unique only within its own room.
	for _, m := range doc.Members {
		sysID, compID, err := memberEnds(sysIDs, compIDs, m.System, m.Component)
		if err != nil {
			return err
		}
		if err := gw.AddMember(ctx, actorID, sysID, compID, all); err != nil {
			return fmt.Errorf("devseed: add member %s/%s: %w", m.System, m.Component, err)
		}
	}

	// Role assignments: staffing, which also creates the membership. Last of the
	// three, since it needs both ends to exist. Idempotent on conflict.
	for _, ra := range doc.RoleAssignments {
		sysID, compID, err := memberEnds(sysIDs, compIDs, ra.System, ra.Component)
		if err != nil {
			return err
		}
		if err := gw.AssignRole(ctx, actorID, sysID, ra.Role, compID, all); err != nil {
			return fmt.Errorf("devseed: assign %s to %s/%s: %w", ra.Component, ra.System, ra.Role, err)
		}
	}

	// Property values: an override a component declares over its product's contract
	// (last, since both the component and the contract line must exist first), so the
	// effective-properties panel teaches direct-vs-inherited. The value is encoded to
	// jsonb like a variable. The set is idempotent, so a re-run changes nothing.
	for _, pv := range doc.PropertyValues {
		raw, err := json.Marshal(pv.Value)
		if err != nil {
			return fmt.Errorf("devseed: encode property value %s/%s: %w", pv.Component, pv.Property, err)
		}
		compID, ok := compIDs[pv.Component]
		if !ok {
			return fmt.Errorf("devseed: property value references unknown component key %q", pv.Component)
		}
		if _, err := gw.SetProperty(ctx, actorID, "component", compID, pv.Property, "", raw, all); err != nil {
			return fmt.Errorf("devseed: set property value %s/%s: %w", pv.Component, pv.Property, err)
		}
	}
	// A worked reachability check on a component, so the console's Reachability panel
	// renders a real verdict + availability strip instead of an empty "unknown".
	if err := seedReachability(ctx, gw, actorID, locIDs); err != nil {
		return err
	}
	// A handful of native call-started events on a boardroom video bar, so the
	// console's event panel comes up populated instead of empty. Addressed by the
	// id its fixture key resolved to: the bar's name is the platform's.
	barID, ok := compIDs[eventComponent]
	if !ok {
		return fmt.Errorf("devseed: the event fixture component %q is not in the estate", eventComponent)
	}
	if err := seedEvents(ctx, gw, barID); err != nil {
		return err
	}
	// Raw device log lines on the huddle room's display, so the console's log panel
	// (the ingest lane, ADR-0066) comes up populated. These are logs, not events.
	displayID, ok := compIDs[logComponent]
	if !ok {
		return fmt.Errorf("devseed: the log fixture component %q is not in the estate", logComponent)
	}
	if err := seedLogs(ctx, gw, displayID); err != nil {
		return err
	}
	// The edge node's own self-logs, so the node blade's Self-logs panel comes up
	// populated: the node narrating itself (ADR-0066), owner-bound to the node.
	if err := seedNodeLogs(ctx, gw); err != nil {
		return err
	}
	return nil
}

// logComponent hangs the example log lines on the huddle room's display (a
// device that emits ordinary device logs: link, CEC, EDID, input, thermal). It
// is a fixture KEY, not a name: the platform names that display, and it names it
// display-1, which two other rooms also hold.
const logComponent = "huddle-display"

// exampleLogs are the display's recent raw log lines (ADR-0066), each offset back
// from now so the panel reads as a recent window (newest last). Two carry a higher
// severity so the panel's severity badges are visible; one carries a structured
// attributes payload. minsAgo is minutes before the seed's now.
var exampleLogs = []struct {
	message  string
	severity string
	facility string
	attrs    []byte
	labels   []byte
	minsAgo  int
}{
	{message: "power state changed to on", severity: "info", facility: "kern", minsAgo: 214},
	{message: "hdmi link state changed to up", severity: "info", facility: "kern", minsAgo: 212},
	{message: "edid read complete: 3840x2160@60", severity: "info", facility: "daemon", minsAgo: 211},
	{message: "cec handshake ok", severity: "info", facility: "daemon", minsAgo: 127},
	{message: "input switched to HDMI2", severity: "notice", facility: "daemon", attrs: []byte(`{"input":"hdmi2"}`), minsAgo: 46},
	// A classified line: a log line is untyped, but labels are how it gets sorted
	// into a class (system, firmware, application) without a registry (ADR-0066).
	// This is the one seeded line carrying both structured columns.
	{message: "backlight temperature high, throttling", severity: "warning", facility: "kern", attrs: []byte(`{"celsius":71,"limit":68}`), labels: []byte(`{"class":"firmware"}`), minsAgo: 12},
}

// seedLogs installs the example log lines on the huddle display idempotently.
// The log_line table has an auto id and no natural unique key, so a naive
// re-insert would pile up duplicates on every make dev; guard on the component
// already carrying lines (ListComponentLogs unwindowed, limit 1) and skip
// when present. componentID is the row the fixture key resolved to.
func seedLogs(ctx context.Context, gw storage.Gateway, componentID string) error {
	existing, err := gw.ListComponentLogs(ctx, componentID, 0, 1)
	if err != nil {
		return fmt.Errorf("devseed: check logs: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}
	now := time.Now().UTC()
	lines := make([]storage.LogLineWrite, 0, len(exampleLogs))
	for _, l := range exampleLogs {
		lines = append(lines, storage.LogLineWrite{
			OwnerKind:  "component",
			OwnerID:    componentID,
			Source:     "syslog",
			Severity:   l.severity,
			Facility:   l.facility,
			Message:    l.message,
			Attributes: l.attrs,
			Labels:     l.labels,
			TS:         now.Add(-time.Duration(l.minsAgo) * time.Minute),
		})
	}
	if err := gw.InsertLogLines(ctx, lines); err != nil {
		return fmt.Errorf("devseed: insert logs: %w", err)
	}
	return nil
}

// exampleNodeLogs are the edge node's own operational self-logs (ADR-0066): the
// node narrating itself (connected, worklist pulled, a task skipped), not a
// component's device output. source and facility name the node subsystem; one
// carries a structured attributes payload and a higher severity so the panel's
// badge and disclosure both show. minsAgo is minutes before the seed's now.
var exampleNodeLogs = []struct {
	message  string
	source   string
	severity string
	facility string
	attrs    []byte
	minsAgo  int
}{
	{message: "connected to bus", source: "node", severity: "info", facility: "enrollment", minsAgo: 63},
	{message: "worklist pulled", source: "node", severity: "info", facility: "collection", attrs: []byte(`{"tasks":2}`), minsAgo: 63},
	{message: "worklist pulled", source: "node", severity: "info", facility: "collection", attrs: []byte(`{"tasks":2}`), minsAgo: 33},
	{message: "task skipped", source: "collection", severity: "warning", facility: "collection", attrs: []byte(`{"task":"boardroom-a-hdmi","error":"unresolved host"}`), minsAgo: 12},
	{message: "worklist pulled", source: "node", severity: "info", facility: "collection", attrs: []byte(`{"tasks":2}`), minsAgo: 3},
}

// seedNodeLogs installs the edge node's example self-logs idempotently, guarded on
// the node already carrying lines (ListNodeLogs from the epoch, limit 1) the same
// way seedLogs guards, since node_log has an auto id and no natural unique key.
func seedNodeLogs(ctx context.Context, gw storage.Gateway) error {
	existing, err := gw.ListNodeLogs(ctx, reachNode, 0, 1)
	if err != nil {
		return fmt.Errorf("devseed: check node logs: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}
	now := time.Now().UTC()
	lines := make([]storage.NodeLogWrite, 0, len(exampleNodeLogs))
	for _, l := range exampleNodeLogs {
		lines = append(lines, storage.NodeLogWrite{
			Node:       reachNode,
			Source:     l.source,
			Severity:   l.severity,
			Facility:   l.facility,
			Message:    l.message,
			Attributes: l.attrs,
			TS:         now.Add(-time.Duration(l.minsAgo) * time.Minute),
		})
	}
	if err := gw.InsertNodeLogs(ctx, lines); err != nil {
		return fmt.Errorf("devseed: insert node logs: %w", err)
	}
	return nil
}

// The worked reachability check the dev seed installs: an enrolled node and a lab
// DSP (a polaris-class 16-channel audio DSP) under the HQ boardroom, exposing two
// APIs. Each API is a protocol-named interface over its transport: `web` (the HTTP
// management API) and `qrc` (the Q-SYS-style control protocol over raw tcp). Two
// interfaces on one device is the "APIs on this box" story: an interface is an API
// we intend to call, named by its protocol, not a network interface. Each has a poll
// task and enough samples for the panel to render a live verdict + availability
// strip.
// reachComponent is a NAME the operator typed, and the one component in the
// estate that is not platform-named: there is no DSP product in the shipped
// catalog, so the generator would classify this box as a generic device and call
// it device-2, which says less than the operator's own word for it. It is
// therefore also the estate's worked example of the name pen's other state,
// sitting beside six generated siblings. It needs no ancestry in it, because a
// component's name is unique within its room rather than across the estate.
// reachLocation and reachNodeLocation are fixture KEYS.
const (
	reachComponent    = "dsp"
	reachLocation     = "boardroom"
	reachNodeLocation = "west"
	reachNode         = "edge-hq"
	reachHost         = "10.20.4.12"
)

// reachChecks are the DSP's interfaces: each named by the protocol it speaks and
// typed by its transport (the reachability axis; the driver that speaks the protocol
// over the transport is a later collection layer). flapped seeds the availability
// history with a brief-outage-then-recovered story (mostly up with a thin blip); an
// unflapped interface reads cleanly up. rttMs is the shared host ping; connMs is the
// per-port connect time.
var reachChecks = []struct {
	name    string
	itype   string
	port    int
	flapped bool
	rttMs   float64
	connMs  float64
}{
	{name: "web", itype: "http", port: 80, flapped: false, rttMs: 6.1, connMs: 2.2},
	{name: "qrc", itype: "tcp", port: 1710, flapped: true, rttMs: 6.1, connMs: 4.8},
}

// seedReachability installs the worked reachability checks idempotently through the
// Storage Gateway. Its sentinel is the first interface: samples are append-only,
// so once it exists the whole block has run and a re-run is a no-op (a second
// `make dev` must not double the samples). It authors each check the way a node
// runs one (interface + poll task) and writes a handful of samples keyed by the
// component (owner) and interface (instance), using ONLY registered canonical
// property_type names, so a wrong name would reject-not-project.
func seedReachability(ctx context.Context, gw storage.Gateway, actorID string, locIDs map[string]string) error {
	all := scope.Set{All: true}
	roomID, ok := locIDs[reachLocation]
	if !ok {
		return fmt.Errorf("devseed: the reachability location key %q is not in the estate", reachLocation)
	}
	closetID, ok := locIDs[reachNodeLocation]
	if !ok {
		return fmt.Errorf("devseed: the reachability node location key %q is not in the estate", reachNodeLocation)
	}

	// Sentinel: the first interface. Present means this block ran on an earlier start.
	existing, err := gw.ListComponentInterfaces(ctx, reachComponent)
	if err != nil {
		return fmt.Errorf("devseed: check reachability interfaces: %w", err)
	}
	for _, it := range existing {
		if it.Name == reachChecks[0].itype {
			return nil
		}
	}

	// The DSP the checks hang on, placed in the HQ boardroom. Tolerate an existing
	// component from a partial earlier run.
	if _, err := gw.GetComponent(ctx, reachComponent, all); errors.Is(err, storage.ErrComponentNotFound) {
		if _, err := gw.CreateComponent(ctx, actorID, storage.ComponentSpec{
			Name:         reachComponent,
			DisplayName:  "Boardroom DSP",
			LocationName: &roomID,
		}, all, all, all, all); err != nil {
			return fmt.Errorf("devseed: create reachability component: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("devseed: check reachability component: %w", err)
	}

	// The node, created then enrolled and claimed so it reads as enrolled in the
	// console (enrolled_at is stamped by a claim). The token lives only long enough to
	// claim; it is never returned or stored in cleartext. Tolerate an existing node.
	if _, err := gw.GetNode(ctx, reachNode, all); errors.Is(err, storage.ErrNodeNotFound) {
		if _, err := gw.CreateNode(ctx, actorID, storage.NodeSpec{
			Name:         reachNode,
			DisplayName:  "HQ Edge Node",
			Description:  "HQ network closet",
			LocationName: &closetID,
		}, all, all); err != nil {
			return fmt.Errorf("devseed: create reachability node: %w", err)
		}
		token, hash, _, err := auth.NewBearerToken()
		if err != nil {
			return fmt.Errorf("devseed: mint node token: %w", err)
		}
		if _, err := gw.SetEnrollmentToken(ctx, actorID, reachNode, hex.EncodeToString(hash), all); err != nil {
			return fmt.Errorf("devseed: enroll reachability node: %w", err)
		}
		if _, err := gw.ClaimNode(ctx, reachNode, hex.EncodeToString(auth.HashToken(token))); err != nil {
			return fmt.Errorf("devseed: claim reachability node: %w", err)
		}
		// Node tags (N2): a couple of governed tags so the console shows the node's
		// Tags panel and the list's Tags column populated. Idempotent upserts.
		nodeName := reachNode
		if _, err := gw.SetTagBinding(ctx, actorID, "environment", "node", &nodeName, "prod", all, all); err != nil {
			return fmt.Errorf("devseed: tag reachability node environment: %w", err)
		}
		if _, err := gw.SetTagBinding(ctx, actorID, "asset-id", "node", &nodeName, "NODE-EDGE-HQ", all, all); err != nil {
			return fmt.Errorf("devseed: tag reachability node asset-id: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("devseed: check reachability node: %w", err)
	}

	// Each interface: named by its protocol, typed by its transport, target = the DSP
	// host at the protocol's port. A poll task rides each; samples tell its story.
	comp := reachComponent
	node := reachNode
	now := time.Now().UTC()
	for _, c := range reachChecks {
		// The interface is protocol-named: its name is DERIVED from its transport
		// (c.itype). Its poll task derives automatically from creating it, so the
		// samples are instanced by the interface name (the transport).
		if _, err := gw.CreateInterface(ctx, actorID, storage.InterfaceSpec{
			Type:      c.itype,
			Component: &comp,
			Node:      &node,
			Params:    []byte(fmt.Sprintf(`{"target":"%s:%d"}`, reachHost, c.port)),
		}, all); err != nil {
			return fmt.Errorf("devseed: create %s interface: %w", c.itype, err)
		}
		if err := seedReachSamples(ctx, gw, c.itype, c.flapped, c.rttMs, c.connMs, now); err != nil {
			return err
		}
	}
	return nil
}

// seedReachSamples writes one interface's reachability samples: the
// interface-reachable state (a fresh "up"; when flapped, an up baseline then a brief
// outage then the recovery, so the strip reads mostly up with a thin blip) and the
// probe-layer metrics (ping + port). Owner = the component, instance = the interface
// name. Only canonical property_type names are used (reject-not-project).
func seedReachSamples(ctx context.Context, gw storage.Gateway, iface string, flapped bool, rttMs, connMs float64, now time.Time) error {
	recovered := now.Add(-30 * time.Second)
	states := []storage.PropertySampleWrite{}
	if flapped {
		states = append(states,
			storage.PropertySampleWrite{OwnerKind: "component", OwnerID: reachComponent, Key: "interface-reachable", Instance: iface, Value: "up", Source: "reachability", TS: now.Add(-2 * time.Hour)},
			storage.PropertySampleWrite{OwnerKind: "component", OwnerID: reachComponent, Key: "interface-reachable", Instance: iface, Value: "down", Source: "reachability", TS: now.Add(-6 * time.Minute)},
		)
	}
	states = append(states, storage.PropertySampleWrite{OwnerKind: "component", OwnerID: reachComponent, Key: "interface-reachable", Instance: iface, Value: "up", Source: "reachability", TS: recovered})
	if err := gw.InsertPropertySamples(ctx, states); err != nil {
		return fmt.Errorf("devseed: insert %s property samples: %w", iface, err)
	}
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: reachComponent, Key: "icmp-reachable", Instance: iface, Value: 1, Source: "icmp", TS: recovered},
		{OwnerKind: "component", OwnerID: reachComponent, Key: "icmp-rtt-avg", Instance: iface, Value: rttMs, Source: "icmp", TS: recovered},
		{OwnerKind: "component", OwnerID: reachComponent, Key: "tcp-open", Instance: iface, Value: 1, Source: "tcp", TS: recovered},
		{OwnerKind: "component", OwnerID: reachComponent, Key: "tcp-connect-time", Instance: iface, Value: connMs, Source: "tcp", TS: recovered},
	}); err != nil {
		return fmt.Errorf("devseed: insert %s metric samples: %w", iface, err)
	}
	return nil
}

// The example events the dev seed installs on a boardroom video bar: a conferencing
// endpoint publishes call-started natively (an xAPI event) so the console's event
// panel comes up populated instead of empty. eventComponent is the fixture KEY of a
// video bar seeded above (the platform names it videobar-2), resolved to an id so
// the event's component_id foreign key resolves. Every row uses the registered
// event_type key call-started (reject-not-project) and is stamped origin=caught: the
// device reported it, the platform did not derive it. Raw device logs are a separate
// ingest lane (ADR-0066), not seeded here.
const eventComponent = "bar-a"

// exampleEvents are the bar's recent call-started occurrences, each offset back from
// now so the panel reads as a recent window (spread over the last day, newest last).
// Two rows carry a structured attributes payload (the call's peer and protocol); the
// rest are plain messages. minsAgo is minutes before the seed's now.
var exampleEvents = []struct {
	message string
	attrs   []byte
	minsAgo int
}{
	{message: "call started with sip:room-204@hq.example", attrs: []byte(`{"peer":"sip:room-204@hq.example","protocol":"sip"}`), minsAgo: 1290},
	{message: "inbound call answered", minsAgo: 642},
	{message: "joined Teams meeting: Weekly Standup", attrs: []byte(`{"peer":"teams:weekly-standup","protocol":"msteams"}`), minsAgo: 213},
	{message: "call started with 4 participants", minsAgo: 27},
}

// seedEvents installs the example events on the video bar idempotently.
// The event table has an auto id (bigint identity) and no natural unique key, so a
// naive re-insert would pile up duplicates on every `make dev`; guard on the component
// already carrying events (ListComponentEvents from the epoch, limit 1) and skip when
// present, so a second run is a no-op. Owner = the component, instance empty (a
// device-level log, not per-interface). Mirrors seedReachability's sentinel pattern.
// componentID is the row the fixture key resolved to.
func seedEvents(ctx context.Context, gw storage.Gateway, componentID string) error {
	// Sentinel: any existing event on the component means this block already ran.
	existing, err := gw.ListComponentEvents(ctx, componentID, 0, 1)
	if err != nil {
		return fmt.Errorf("devseed: check events: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}
	now := time.Now().UTC()
	evs := make([]storage.EventWrite, 0, len(exampleEvents))
	for _, e := range exampleEvents {
		evs = append(evs, storage.EventWrite{
			OwnerKind:  "component",
			OwnerID:    componentID,
			Key:        "call-started",
			Origin:     "caught",
			Message:    e.message,
			Attributes: e.attrs,
			Source:     "xapi",
			TS:         now.Add(-time.Duration(e.minsAgo) * time.Minute),
		})
	}
	if err := gw.InsertEvents(ctx, evs); err != nil {
		return fmt.Errorf("devseed: insert events: %w", err)
	}
	return nil
}
