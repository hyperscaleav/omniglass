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
	PropertyValues    []PropertyValue   `yaml:"property_values"`
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

// Component is one example device placed in the estate. Location names a fixture
// location, resolved to its id at seed time (empty for an unplaced component);
// Product names a catalog SKU, whose contract supplies the component's properties.
// A system binding is omitted for now (optional on create).
type Component struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name"`
	Product     string `yaml:"product"`
	Location    string `yaml:"location"`
}

// PropertyValue is one example literal a component declares over its product's
// contract: an override so the effective-properties panel teaches
// direct-vs-inherited. Value is decoded from YAML and re-encoded to jsonb, exactly
// like a Variable.
type PropertyValue struct {
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

// Tag is one example key in the governed vocabulary, optionally with a global
// default value. Propagates is a pointer so an omitted field defaults to true (a
// tag cascades unless the fixture opts out).
type Tag struct {
	Name        string   `yaml:"name"`
	AppliesTo   []string `yaml:"applies_to"`
	Propagates  *bool    `yaml:"propagates"`
	GlobalValue string   `yaml:"global_value"`
}

// TagBinding is one example scoped binding, setting a key's value at a fixture
// location so the effective-tags cascade comes up with an override to teach.
type TagBinding struct {
	Key      string `yaml:"key"`
	Location string `yaml:"location"`
	Value    string `yaml:"value"`
}

// Variable is one example global variable (a macro). Value is decoded from YAML
// and re-encoded to jsonb, so `value: 30` seeds the number and `value: {a: 1}` the
// object. Global scope keeps the fixture free of an owner dependency.
type Variable struct {
	Name      string `yaml:"name"`
	ValueType string `yaml:"value_type"`
	Value     any    `yaml:"value"`
}

// Location is one node of the example tree. Parent names a location declared
// earlier in the document (empty for a root).
type Location struct {
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

// Grant assigns a role at a scope. ScopeRef names a fixture location (resolved to
// its id at seed time) for a location-scoped grant; it is empty for the all scope.
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
	// child's parent resolves. locIDs lets a later grant address a location by name.
	locIDs := map[string]string{}
	for _, l := range doc.Locations {
		if existing, err := gw.GetLocation(ctx, l.Name, all); err == nil {
			locIDs[l.Name] = existing.ID
			continue
		} else if !errors.Is(err, storage.ErrLocationNotFound) {
			return fmt.Errorf("devseed: check location %q: %w", l.Name, err)
		}
		spec := storage.LocationSpec{Name: l.Name, DisplayName: l.DisplayName, LocationType: l.Type}
		if l.Parent != "" {
			spec.ParentName = &l.Parent
		}
		created, err := gw.CreateLocation(ctx, actorID, spec, all)
		if err != nil {
			return fmt.Errorf("devseed: create location %q: %w", l.Name, err)
		}
		locIDs[l.Name] = created.ID
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
					return fmt.Errorf("devseed: user %q grant references unknown location %q", u.Username, g.ScopeRef)
				}
				spec.ScopeID = id
			}
			if _, err := gw.CreateGrant(ctx, actorID, pr.ID, spec, all); err != nil {
				return fmt.Errorf("devseed: grant %s to %q: %w", g.Role, u.Username, err)
			}
		}
	}

	// Global variables: a couple of example macros so the Variables directory comes
	// up populated. A variable that already exists (ErrVariableExists) is left as
	// is, so a re-run adds nothing.
	for _, v := range doc.Variables {
		raw, err := json.Marshal(v.Value)
		if err != nil {
			return fmt.Errorf("devseed: encode variable %q: %w", v.Name, err)
		}
		_, err = gw.CreateVariable(ctx, actorID, storage.VariableSpec{
			Name: v.Name, ValueType: v.ValueType, OwnerKind: "global", Value: raw,
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
		if tg.GlobalValue != "" {
			if _, err := gw.SetTagBinding(ctx, actorID, tg.Name, "global", nil, tg.GlobalValue, all, all); err != nil {
				return fmt.Errorf("devseed: set global tag %q: %w", tg.Name, err)
			}
		}
	}
	for _, b := range doc.TagBindings {
		loc := b.Location
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
		spec := storage.ProductPropertySpec{PropertyName: pp.Property, Required: pp.Required}
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
	// boot seed) for the placement and the product binding to resolve. Like a location,
	// a component has a stable name but no create-conflict sentinel, so check
	// GetComponent for ErrComponentNotFound first and skip when already present,
	// keeping the seed idempotent.
	for _, c := range doc.Components {
		if _, err := gw.GetComponent(ctx, c.Name, all); err == nil {
			continue
		} else if !errors.Is(err, storage.ErrComponentNotFound) {
			return fmt.Errorf("devseed: check component %q: %w", c.Name, err)
		}
		spec := storage.ComponentSpec{Name: c.Name, DisplayName: c.DisplayName}
		if c.Location != "" {
			loc := c.Location
			spec.LocationName = &loc
		}
		if c.Product != "" {
			prod := c.Product
			spec.ProductName = &prod
		}
		if _, err := gw.CreateComponent(ctx, actorID, spec, all); err != nil {
			return fmt.Errorf("devseed: create component %q: %w", c.Name, err)
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
		if _, err := gw.SetPropertyValue(ctx, actorID, "component", pv.Component, pv.Property, "", raw, all); err != nil {
			return fmt.Errorf("devseed: set property value %s/%s: %w", pv.Component, pv.Property, err)
		}
	}
	// A worked reachability check on a component, so the console's Reachability panel
	// renders a real verdict + availability strip instead of an empty "unknown".
	if err := seedReachability(ctx, gw, actorID); err != nil {
		return err
	}
	// A handful of example log occurrences on the lobby display, so the console's
	// event-log panel comes up populated instead of empty.
	if err := seedEvents(ctx, gw); err != nil {
		return err
	}
	return nil
}

// The worked reachability check the dev seed installs: an enrolled node and a lab
// DSP (a polaris-class 16-channel audio DSP) under the HQ boardroom, exposing two
// APIs. Each API is a protocol-named interface over its transport: `web` (the HTTP
// management API) and `qrc` (the Q-SYS-style control protocol over raw tcp). Two
// interfaces on one device is the "APIs on this box" story: an interface is an API
// we intend to call, named by its protocol, not a network interface. Each has a poll
// task and enough datapoints for the panel to render a live verdict + availability
// strip.
const (
	reachComponent = "hq-boardroom-dsp"
	reachLocation  = "hq-west-2-boardroom"
	reachNode      = "edge-hq"
	reachHost      = "10.20.4.12"
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
// Storage Gateway. Its sentinel is the first interface: datapoints are append-only,
// so once it exists the whole block has run and a re-run is a no-op (a second
// `make dev` must not double the datapoints). It authors each check the way a node
// runs one (interface + poll task) and writes a handful of datapoints keyed by the
// component (owner) and interface (instance), using ONLY registered canonical
// datapoint_type names, so a wrong name would reject-not-project.
func seedReachability(ctx context.Context, gw storage.Gateway, actorID string) error {
	all := scope.Set{All: true}

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
		loc := reachLocation
		if _, err := gw.CreateComponent(ctx, actorID, storage.ComponentSpec{
			Name:         reachComponent,
			DisplayName:  "Boardroom DSP",
			LocationName: &loc,
		}, all); err != nil {
			return fmt.Errorf("devseed: create reachability component: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("devseed: check reachability component: %w", err)
	}

	// The node, created then enrolled and claimed so it reads as enrolled in the
	// console (enrolled_at is stamped by a claim). The token lives only long enough to
	// claim; it is never returned or stored in cleartext. Tolerate an existing node.
	if _, err := gw.GetNode(ctx, reachNode, all); errors.Is(err, storage.ErrNodeNotFound) {
		reachNodeLoc := "hq-west"
		if _, err := gw.CreateNode(ctx, actorID, storage.NodeSpec{
			Name:         reachNode,
			DisplayName:  "HQ Edge Node",
			Description:  "HQ network closet",
			LocationName: &reachNodeLoc,
		}, all); err != nil {
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
		if _, err := gw.SetTagBinding(ctx, actorID, "asset_id", "node", &nodeName, "NODE-EDGE-HQ", all, all); err != nil {
			return fmt.Errorf("devseed: tag reachability node asset_id: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("devseed: check reachability node: %w", err)
	}

	// Each interface: named by its protocol, typed by its transport, target = the DSP
	// host at the protocol's port. A poll task rides each; datapoints tell its story.
	comp := reachComponent
	node := reachNode
	now := time.Now().UTC()
	for _, c := range reachChecks {
		// The interface is protocol-named: its name is DERIVED from its transport
		// (c.itype). Its poll task derives automatically from creating it, so the
		// datapoints are instanced by the interface name (the transport).
		if _, err := gw.CreateInterface(ctx, actorID, storage.InterfaceSpec{
			Type:      c.itype,
			Component: &comp,
			Node:      &node,
			Params:    []byte(fmt.Sprintf(`{"target":"%s:%d"}`, reachHost, c.port)),
		}, all); err != nil {
			return fmt.Errorf("devseed: create %s interface: %w", c.itype, err)
		}
		if err := seedReachDatapoints(ctx, gw, c.itype, c.flapped, c.rttMs, c.connMs, now); err != nil {
			return err
		}
	}
	return nil
}

// seedReachDatapoints writes one interface's reachability datapoints: the
// interface.reachable state (a fresh "up"; when flapped, an up baseline then a brief
// outage then the recovery, so the strip reads mostly up with a thin blip) and the
// probe-layer metrics (ping + port). Owner = the component, instance = the interface
// name. Only canonical datapoint_type names are used (reject-not-project).
func seedReachDatapoints(ctx context.Context, gw storage.Gateway, iface string, flapped bool, rttMs, connMs float64, now time.Time) error {
	recovered := now.Add(-30 * time.Second)
	states := []storage.StateDatapointEvent{}
	if flapped {
		states = append(states,
			storage.StateDatapointEvent{OwnerKind: "component", OwnerID: reachComponent, Key: "interface.reachable", Instance: iface, Value: "up", Source: "reachability", TS: now.Add(-2 * time.Hour)},
			storage.StateDatapointEvent{OwnerKind: "component", OwnerID: reachComponent, Key: "interface.reachable", Instance: iface, Value: "down", Source: "reachability", TS: now.Add(-6 * time.Minute)},
		)
	}
	states = append(states, storage.StateDatapointEvent{OwnerKind: "component", OwnerID: reachComponent, Key: "interface.reachable", Instance: iface, Value: "up", Source: "reachability", TS: recovered})
	if err := gw.InsertStateDatapoints(ctx, states); err != nil {
		return fmt.Errorf("devseed: insert %s state datapoints: %w", iface, err)
	}
	if err := gw.InsertMetricDatapoints(ctx, []storage.MetricDatapointEvent{
		{OwnerKind: "component", OwnerID: reachComponent, Key: "icmp.reachable", Instance: iface, Value: 1, Source: "icmp", TS: recovered},
		{OwnerKind: "component", OwnerID: reachComponent, Key: "icmp.rtt_avg", Instance: iface, Value: rttMs, Source: "icmp", TS: recovered},
		{OwnerKind: "component", OwnerID: reachComponent, Key: "tcp.open", Instance: iface, Value: 1, Source: "tcp", TS: recovered},
		{OwnerKind: "component", OwnerID: reachComponent, Key: "tcp.connect_time", Instance: iface, Value: connMs, Source: "tcp", TS: recovered},
	}); err != nil {
		return fmt.Errorf("devseed: insert %s metric datapoints: %w", iface, err)
	}
	return nil
}

// The example log occurrences the dev seed installs on the lobby display, the
// log-kind sink of the collection pipeline: a display emits device log lines (link,
// CEC, EDID, input) so the console's event-log panel comes up populated instead of
// empty. eventComponent names an existing fixture component (the display seeded above),
// so the event's component_id foreign key resolves. All rows use the registered
// log-kind key syslog.line (reject-not-project), with provenance stamped observed by
// the insert.
const eventComponent = "lobby-display"

// exampleEvents are the display's recent log lines, each offset back from now so the
// panel reads as a recent window (spread over the last few hours, newest last). One
// line carries a structured attributes payload (the switched input); the rest are
// plain messages. minsAgo is minutes before the seed's now.
var exampleEvents = []struct {
	message string
	attrs   []byte
	minsAgo int
}{
	{message: "power state changed to on", minsAgo: 214},
	{message: "hdmi link state changed to up", minsAgo: 212},
	{message: "edid read complete: 3840x2160@60", minsAgo: 211},
	{message: "cec handshake ok", minsAgo: 127},
	{message: "input switched to HDMI2", attrs: []byte(`{"input":"hdmi2"}`), minsAgo: 46},
	{message: "backlight brightness set to 80%", minsAgo: 12},
}

// seedEvents installs the example log occurrences on the lobby display idempotently.
// The event table has an auto id (bigint identity) and no natural unique key, so a
// naive re-insert would pile up duplicates on every `make dev`; guard on the component
// already carrying events (ListComponentEvents from the epoch, limit 1) and skip when
// present, so a second run is a no-op. Owner = the component, instance empty (a
// device-level log, not per-interface). Mirrors seedReachability's sentinel pattern.
func seedEvents(ctx context.Context, gw storage.Gateway) error {
	// Sentinel: any existing event on the component means this block already ran.
	existing, err := gw.ListComponentEvents(ctx, eventComponent, time.Time{}, 1)
	if err != nil {
		return fmt.Errorf("devseed: check events: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}
	now := time.Now().UTC()
	evs := make([]storage.EventOccurrence, 0, len(exampleEvents))
	for _, e := range exampleEvents {
		evs = append(evs, storage.EventOccurrence{
			OwnerKind:  "component",
			OwnerID:    eventComponent,
			Key:        "syslog.line",
			Message:    e.message,
			Attributes: e.attrs,
			Source:     "syslog",
			TS:         now.Add(-time.Duration(e.minsAgo) * time.Minute),
		})
	}
	if err := gw.InsertEvents(ctx, evs); err != nil {
		return fmt.Errorf("devseed: insert events: %w", err)
	}
	return nil
}
