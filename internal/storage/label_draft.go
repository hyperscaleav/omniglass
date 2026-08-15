package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/label"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// The draft identity render (#699, #702): the name and the label a row WOULD
// carry, answered before the row exists, for a create form that wants to show
// the operator what the platform is about to name and label rather than only
// promising that it will.
//
// # Why this is not the preview ADR-0104 refused
//
// That refusal is about MINTING. A draft preview that allocated an ordinal took
// the same pg_advisory_xact_lock on the placement bucket that real creates
// take, so a form previewing per picker change would serialise the fleet's
// creates behind a UI affordance.
//
// A render allocates nothing. It resolves the rule through the same tiers, over
// the same closed data map, with the same one engine, and it READS the lowest
// free ordinal in the placement bucket ([previewName]) rather than allocating
// one. No lock, no write transaction, no allocation, and no second
// implementation of anything: every function it calls is the one the create's
// own stamp calls.
//
// The other half of that refusal was that the answer is provisional, and it is:
// another create can take the number in between. That is answered by the
// PRECONDITION rather than by hiding the number (#702). The form posts the
// ordinal it was shown, the create compares it with what it allocated
// (confirmOrdinal), and a create that would land a different name is refused
// with the number that moved. An operator is either given the name they were
// shown or told it changed, and never quietly handed a third thing.
//
// # What it reuses, and why that is the point
//
// componentLabelChain / systemLabelChainWith / locationLabelChainWith resolve
// the tiers; componentLabelData / systemLabelData / locationLabelData build the
// closed map (which IS the sandbox: a key absent there is a key a rule cannot
// reach, and this file adds none); renderLabel executes and degrades;
// previewName reads the ordinal from the same siblings the allocator tests, with
// the same mint. What the draft supplies differently is only the two facts a row
// that does not exist cannot have of its own, and it now supplies BOTH from the
// same primitives the create uses rather than leaving either as a token.
//
// # Placement, and the scopes that guard it
//
// The draft takes the same scopes its create takes, in the same order, because
// it resolves the same references: a component's parent within the caller's
// component:create scope (the set the create resolves it in), its location
// within location:read (the set that decides whether the rendered string may
// carry that label), and its system within system:read AND system:update, which
// is the set the create's bind resolves in because that bind writes the system's
// membership (#707, #713). An out-of-scope reference is the same non-disclosing
// not-found a direct read would give. Rendering it anyway would make this route
// a disclosure channel, and resolving it in a WIDER set than the create's would
// make it a preview of a create the platform refuses.
//
// The parent is new here (#702) and it is not about the label at all: a
// location's map carries no placement (ADR-0098's exclusion survives on that
// tier), but the ordinal is read from a BUCKET, and the bucket is exactly the
// parent, else the location, else the unplaced one. So a location's draft now
// takes a scope where it took none, and what it guards is the sibling read
// rather than the render.

// DraftLabel is what a create form is told about the identity it is about to
// produce: the name, the ordinal that name was minted from, the rendered label,
// and the rule that rendered it.
//
// The NAME is here (#702) rather than resolved a second time in the browser
// because the label is rendered FROM it: the server has already resolved the
// stem, the mint and the ordinal to answer at all, so returning them costs
// nothing and removes the console's need to know what a mint looks like. It is
// also the only way the two halves of a create form can be one answer rather
// than two that can disagree.
//
// The rule travels with the answer for two reasons. It is the only way to tell
// "no rule applies at any tier" (both label fields empty, which an operator
// reaches by clearing the rule at every tier) from "a rule applies and had
// nothing to say about this row" (a rule, an empty label). And a surface that
// shows where a value came from teaches the mechanism it operates, which a bare
// string cannot.
type DraftLabel struct {
	// Name is the name the create would stamp: the one the caller supplied, or
	// the one the platform would mint at Ordinal.
	Name string
	// Ordinal is the number Name was minted from, and 0 when the caller
	// supplied the name, because that row carries no ordinal at all (#681). It
	// is what a form posts back as its precondition, and 0 is deliberately not
	// a legal value to post: absent and zero are the same state, which is "the
	// platform allocated nothing here".
	Ordinal int
	// Label is the rendered label, or "" when no rule resolves or the rule
	// renders nothing. Empty is not a failure: the read ladder's third rung is
	// the entity's own name, so an empty label means the name shows instead.
	Label string
	// Rule is the template that produced Label, resolved through the same tier
	// precedence a create uses, or "" when no tier had an opinion.
	Rule string
}

// ComponentLabelDraft is a component that does not exist yet. Name empty is the
// operator handing the pen to the platform, exactly as it is on ComponentSpec,
// so the draft and the create it describes are asked the same question in the
// same shape and cannot be given different answers by a caller that fills one
// field and not the other.
type ComponentLabelDraft struct {
	ProductName string
	Name        string
	// ParentName, LocationName and SystemName mirror the create's own placement
	// fields. The first two are also the NAME's placement bucket (a parent wins
	// over a location, and neither is the unplaced bucket), which is what the
	// previewed ordinal is read from: a form that showed the ordinal of a
	// bucket the create does not write into would be worse than showing none.
	ParentName   string
	LocationName string
	SystemName   string
}

// SystemLabelDraft is ComponentLabelDraft on the system tier.
type SystemLabelDraft struct {
	SystemTypeRef string
	StandardRef   string
	Name          string
	ParentName    string
	LocationName  string
}

// LocationLabelDraft is ComponentLabelDraft on the location tier. Its LABEL
// carries no placement: a location's data map is its name and its type, and
// deliberately nothing about where it sits (ADR-0098's exclusion survives on
// this tier). Its NAME does: a location has two placement buckets, under a
// parent or at the root, so ParentName is what the previewed ordinal is read
// from.
type LocationLabelDraft struct {
	LocationTypeRef string
	Name            string
	ParentName      string
}

// RenderComponentDraftLabel renders the name and the label a component create
// would stamp, without creating anything.
//
// The scopes are the create's own, in the create's own order and now in the
// create's own number (#713): create resolves the PARENT (the set
// CreateComponent resolves it in, and the bucket the previewed ordinal is read
// from), locationRead the location whose label the map reads, and systemRead
// with systemUpdate the system, through the same read-then-action split the
// create's bind uses. The permission to ASK is the component's own :create and
// lives at the route (see internal/api/label_draft.go); these four decide what
// the answer may contain.
func (p *PG) RenderComponentDraftLabel(ctx context.Context, d ComponentLabelDraft, create, locationRead, systemRead, systemUpdate scope.Set) (DraftLabel, error) {
	productID, err := registryID(ctx, p.pool, "product", d.ProductName, ErrProductNotFound)
	if err != nil {
		return DraftLabel{}, err
	}
	in, err := componentLabelChain(ctx, p.pool, productID)
	if err != nil {
		return DraftLabel{}, err
	}
	// Placement first, because the NAME is read from it now: the ordinal is the
	// lowest free one among the siblings in this exact bucket, so a draft that
	// resolved the placement after minting would be answering about a different
	// bucket than the create writes into.
	parentID, err := draftParentID(ctx, p.pool, componentConfig, d.ParentName, "component", create, ErrComponentNotFound, ErrParentComponentNotFound)
	if err != nil {
		return DraftLabel{}, err
	}
	var (
		pl         componentPlacement
		locationID *string
	)
	if d.LocationName != "" {
		loc, err := p.GetLocation(ctx, d.LocationName, locationRead)
		if err != nil {
			return DraftLabel{}, err
		}
		pl.locationLabel = locationReadLabel(loc)
		locationID = &loc.ID
	}
	if d.SystemName != "" {
		// resolvePlacementWriteRef, the create's own resolver, not a read (#713).
		// A component create INSERTS this system's membership, so the create
		// requires system:update over it and resolves it there; a draft that
		// resolved the same reference in system:read alone would preview a create
		// the platform then refuses, and the preview would carry that system's
		// type label back out (the disclosure the read scope exists to close, one
		// tier further along). The read set still decides what the refusal may
		// SAY, exactly as it does on the create.
		sys, err := resolvePlacementWriteRef(ctx, p.pool, systemConfig, d.SystemName, systemRead, systemUpdate)
		if errors.Is(err, ErrSystemForbidden) {
			return DraftLabel{}, ErrSystemBindForbidden
		}
		if err != nil {
			return DraftLabel{}, err
		}
		if pl.systemTypeLabel, err = systemTypeLabelOf(ctx, p.pool, sys.SystemTypeID); err != nil {
			return DraftLabel{}, err
		}
	}
	c := Component{Name: d.Name}
	if d.Name == "" {
		// The same refusal generateNameForProduct raises, from the same
		// resolved stem: a form must never lock a field over a value the
		// create then declines to produce. componentLabelChain has already
		// walked the chain for the map's Stem key, so this costs nothing.
		if in.stem == "" {
			return DraftLabel{}, fmt.Errorf("%w: product %q", ErrComponentTypeNoStem, d.ProductName)
		}
		name, ordinal, err := previewName(ctx, p.pool, componentMint(in.stem), componentNameScope(parentID, locationID))
		if err != nil {
			return DraftLabel{}, err
		}
		c.Name, c.Ordinal = name, &ordinal
	}
	eng, err := p.labelEngine(ctx, p.pool)
	if err != nil {
		return DraftLabel{}, err
	}
	return draftedLabel(eng, in.rule, componentLabelData(&c, in, pl), c.Name, c.Ordinal), nil
}

// RenderSystemDraftLabel renders the name and the label a system create would
// stamp. create resolves the parent (the name's bucket), locationRead the one
// placement fact a system's map carries.
func (p *PG) RenderSystemDraftLabel(ctx context.Context, d SystemLabelDraft, create, locationRead scope.Set) (DraftLabel, error) {
	var systemTypeID, standardID *string
	if d.SystemTypeRef != "" {
		id, err := registryID(ctx, p.pool, "system_type", d.SystemTypeRef, ErrUnknownSystemType)
		if err != nil {
			return DraftLabel{}, err
		}
		systemTypeID = &id
	}
	if d.StandardRef != "" {
		id, err := registryID(ctx, p.pool, "standard", d.StandardRef, ErrUnknownStandard)
		if err != nil {
			return DraftLabel{}, err
		}
		standardID = &id
	}
	global, err := globalLabelRule(ctx, p.pool, "system")
	if err != nil {
		return DraftLabel{}, err
	}
	in, err := systemLabelChainWith(ctx, p.pool, standardID, systemTypeID, global)
	if err != nil {
		return DraftLabel{}, err
	}
	parentID, err := draftParentID(ctx, p.pool, systemConfig, d.ParentName, "system", create, ErrSystemNotFound, ErrParentSystemNotFound)
	if err != nil {
		return DraftLabel{}, err
	}
	var (
		pl         systemPlacement
		locationID *string
	)
	if d.LocationName != "" {
		loc, err := p.GetLocation(ctx, d.LocationName, locationRead)
		if err != nil {
			return DraftLabel{}, err
		}
		pl.locationLabel = locationReadLabel(loc)
		locationID = &loc.ID
	}
	s := System{Name: d.Name}
	if d.Name == "" {
		// generateNameForSystemType's two refusals, in its order: an
		// unclassified system has no registry row to take a stem from, and a
		// classified one whose chain sets none anywhere is a registry defect.
		// The console reads them as one state and says two different things,
		// because the operator's next move differs.
		if systemTypeID == nil {
			return DraftLabel{}, ErrSystemTypeRequiredForName
		}
		if in.stem == "" {
			return DraftLabel{}, fmt.Errorf("%w: system_type %s", ErrSystemTypeNoStem, *systemTypeID)
		}
		name, ordinal, err := previewName(ctx, p.pool, systemMint(in.stem), systemNameScope(parentID, locationID))
		if err != nil {
			return DraftLabel{}, err
		}
		s.Name, s.Ordinal = name, &ordinal
	}
	eng, err := p.labelEngine(ctx, p.pool)
	if err != nil {
		return DraftLabel{}, err
	}
	return draftedLabel(eng, in.rule, systemLabelData(&s, in, pl), s.Name, s.Ordinal), nil
}

// RenderLocationDraftLabel renders the name and the label a location create
// would stamp. Its one scope is the create's own, and it guards the PARENT
// rather than the render: a location's data map reads nothing off another
// fleet row (its keys are the location's own name and its type's display
// name), but the previewed ordinal is read from the parent's bucket, which is
// the same reference CreateLocation resolves in this same set.
func (p *PG) RenderLocationDraftLabel(ctx context.Context, d LocationLabelDraft, create scope.Set) (DraftLabel, error) {
	// Resolved first and unconditionally, so an unknown location_type is the
	// same ErrUnknownType a create gives rather than a label rendered with an
	// empty type name. locationLabelChainWith below tolerates a missing row on
	// purpose (a recompute must not stop on one), which is exactly the
	// tolerance a form must not inherit. It also has to be the uuid rather than
	// the operator-facing id, because that resolver keys on the column a stored
	// Location carries.
	locationTypeID, err := registryID(ctx, p.pool, "location_type", d.LocationTypeRef, ErrUnknownType)
	if err != nil {
		return DraftLabel{}, err
	}
	rule, err := locationNameRule(ctx, p.pool, locationTypeID)
	if err != nil {
		return DraftLabel{}, err
	}
	global, err := globalLabelRule(ctx, p.pool, "location")
	if err != nil {
		return DraftLabel{}, err
	}
	in, err := locationLabelChainWith(ctx, p.pool, locationTypeID, global)
	if err != nil {
		return DraftLabel{}, err
	}
	parentID, err := draftParentID(ctx, p.pool, locationConfig, d.ParentName, "location", create, ErrLocationNotFound, ErrParentNotFound)
	if err != nil {
		return DraftLabel{}, err
	}
	l := Location{Name: d.Name}
	if d.Name == "" {
		if rule == nil {
			return DraftLabel{}, fmt.Errorf("%w: location_type %s", ErrLocationTypeNoNameRule, locationTypeID)
		}
		name, ordinal, err := previewName(ctx, p.pool, rule.mint(), locationNameScope(parentID))
		if err != nil {
			return DraftLabel{}, err
		}
		l.Name, l.Ordinal = name, &ordinal
	}
	eng, err := p.labelEngine(ctx, p.pool)
	if err != nil {
		return DraftLabel{}, err
	}
	// A location's map deliberately carries no Ordinal key (labels.go), so the
	// previewed number reaches this tier's label only through the NAME the rule
	// reads, which is exactly how a stored location's own label is rendered.
	return draftedLabel(eng, in.rule, locationLabelData(&l, in), l.Name, l.Ordinal), nil
}

// draftParentID resolves the parent reference a drafted name's bucket is read
// from, within the caller's CREATE scope on that tier, and answers the
// parent-specific sentinel a create answers rather than the generic not-found.
//
// The create scope, not a read scope, and the difference matters: the reference
// is resolved here for the same reason CreateComponent resolves it, to decide
// which bucket the row lands in, so a draft resolving it in a wider set would
// preview an ordinal for a create the caller cannot make. The ordinal it
// discloses is one the caller could allocate for themselves by creating, which
// is what makes previewing it disclose nothing new.
//
// An empty ref is the PARENTLESS bucket, and it takes the create's own all-scope
// gate rather than resolving to nil unguarded (#702 review). Every one of the
// three creates refuses a parentless row to a caller whose create scope is not
// all (CreateLocation, CreateComponent, CreateSystem each answer cfg.forbidden),
// so a draft that answered it previewed a bucket the caller could never write
// into.
//
// That was a disclosure and not only an inconsistency. previewName reads the
// bucket's sibling NAMES and returns the lowest free ordinal, so the answer
// tells the caller which names in that bucket are taken. On the root bucket the
// probe is choosable: an operator holding location_type:create (seeded admin)
// forks a location type, writes any stem into its name rule, and asks the draft
// which ordinal is free for it. The function's own argument, that the caller
// could allocate the disclosed ordinal themselves by creating, is exactly what
// does not hold here.
//
// The gate is unconditional rather than only when a name is being drafted, for
// the same reason the create's is: what a caller cannot create, a form has no
// business rehearsing. The permission to ASK is still the route's gate; this is
// the scope, which is the other layer and enforced separately.
func draftParentID[T any](ctx context.Context, q querier, cfg scopedConfig[T], ref, resource string, create scope.Set, notFound, parentNotFound error) (*string, error) {
	if ref == "" {
		if !create.All {
			return nil, cfg.forbidden
		}
		return nil, nil
	}
	parent, err := resolveScopedRef(ctx, q, cfg, ref, resource, create)
	if errors.Is(err, notFound) {
		return nil, parentNotFound
	}
	if err != nil {
		return nil, err
	}
	id := cfg.idOf(parent)
	return &id, nil
}

// draftedLabel executes a resolved rule over a drafted map and returns it with
// the identity it was rendered for.
//
// Nothing is poked into the map any more (#702). It used to write the ordinal
// TOKEN over the Ordinal key, because the number was unknowable; the number is
// now read from the bucket before the map is built, so the drafted row carries
// it in the same field a stored row does and the three data builders stay what
// makes this safe: a key not added there is a key no rule can reach, and a
// draft adds none.
func draftedLabel(eng *label.Engine, rule string, data label.Data, name string, ordinal *int) DraftLabel {
	d := DraftLabel{Name: name, Label: renderLabel(eng, rule, data), Rule: rule}
	if ordinal != nil {
		d.Ordinal = *ordinal
	}
	return d
}

// registryID resolves a catalog reference (a name or a uuid) to the row's id,
// answering with the caller's own not-found sentinel so the refusal reads the
// way the create's does. Catalogs are not scoped trees, so there is no scope to
// inject here; the scoped refs a draft touches go through GetLocation and
// GetSystem instead.
//
// table is a compile-time constant at every call site, never operator input.
func registryID(ctx context.Context, q querier, table, ref string, notFound error) (string, error) {
	var id string
	err := q.QueryRow(ctx, `select id from `+table+` where `+registryRefCol(ref)+` = $1`, ref).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", notFound
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve %s %q: %w", table, ref, err)
	}
	return id, nil
}

// systemTypeLabelOf reads what SystemTypeLabel means for one system: its type's
// label, absent for an unclassified one. It is the single-row twin of
// the join componentPlacements does for a page, and the acceptance test (a
// drafted label compared with the one the create then stores, with a rule
// reading both placement facts) is what holds the two to the same answer.
func systemTypeLabelOf(ctx context.Context, q querier, systemTypeID *string) (string, error) {
	if systemTypeID == nil {
		return "", nil
	}
	var name string
	err := q.QueryRow(ctx, `select coalesce(label, '') from system_type where id = $1`, *systemTypeID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve system_type label %q: %w", *systemTypeID, err)
	}
	return name, nil
}
