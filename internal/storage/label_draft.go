package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/label"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// The draft label render (#699): the label a row WOULD carry, answered before
// the row exists, for a create form that wants to show the operator what the
// platform is about to name and label rather than only promising that it will.
//
// # Why this is not the preview ADR-0104 refused
//
// That refusal is about MINTING. A draft preview that allocated an ordinal
// bought a provisional number (another create can take it between the preview
// and the commit) and, worse, took the same pg_advisory_xact_lock on the
// placement bucket that real creates take, so a form previewing per picker
// change would serialise the estate's creates behind a UI affordance.
//
// A render allocates nothing. It resolves the rule through the same tiers, over
// the same closed data map, with the same one engine, and writes the token
// [OrdinalToken] where the ordinal would go. No lock, no write transaction, no
// allocation, and no second implementation of anything: every function it calls
// is the one the create's own stamp calls. The one fact it cannot know stays
// visibly unknown instead of being guessed at.
//
// # What it reuses, and why that is the point
//
// componentLabelChain / systemLabelChainWith / locationLabelChainWith resolve
// the tiers; componentLabelData / systemLabelData / locationLabelData build the
// closed map (which IS the sandbox: a key absent there is a key a rule cannot
// reach, and this file adds none); renderLabel executes and degrades. What the
// draft supplies differently is only the two facts a row that does not exist
// cannot have: its own name, and its ordinal.
//
// # Placement, and the scope that guards it
//
// A component's map reads the LABEL of its location and the label of its
// primary system's TYPE, and a system's reads its location's label. Those are
// other rows, so the draft resolves them through the caller's read scope
// (GetLocation, GetSystem) and an out-of-scope placement is the same
// non-disclosing not-found a direct read would give. Rendering it anyway would
// make this route a disclosure channel for a label the caller holds no grant to
// read. A LOCATION's map carries no placement at all, which is why its draft
// takes no scope: there is nothing on another estate row for it to leak.

// OrdinalToken stands where the ordinal would go in a drafted name. It is a
// letter rather than a digit so nothing rendered from a draft can be read as
// the value the row will actually get: "display-n" is visibly a shape, where
// "display-4" is a promise the next create in the bucket can break.
//
// The console writes the same token into the name shape it resolves in the
// browser (web/src/lib/namegen.ts's ORDINAL_TOKEN), so the two locked fields on
// a create form agree about which character is the unknown one.
const OrdinalToken = "n"

// shape is [nameMint.name] with a token written where the number goes: the same
// three cases in the same order, so a shape the console shows can never
// describe a name the mint does not produce.
//
// The suppressing case returns the bare stem, which is exactly name(1): the
// first of its stem in a bucket carries no ordinal at all (ADR-0101), so there
// is no place in it for a token. The sentence that has to travel with that
// shape (the second one is "<stem>-2") is the surface's job, not the mint's.
func (m nameMint) shape(token string) string {
	if m.stem == "" {
		return token
	}
	if m.bareFirst {
		return m.stem
	}
	return m.stem + "-" + token
}

// DraftLabel is what a create form is told about the label it is about to
// produce: the rendered string, and the rule that rendered it.
//
// The rule travels with the answer for two reasons. It is the only way to tell
// "no rule applies at any tier" (both fields empty, which is every location in
// a shipped estate) from "a rule applies and had nothing to say about this row"
// (a rule, an empty label). And a surface that shows where a value came from
// teaches the mechanism it operates, which a bare string cannot.
type DraftLabel struct {
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
	ProductName  string
	Name         string
	LocationName string
	SystemName   string
}

// SystemLabelDraft is ComponentLabelDraft on the system tier.
type SystemLabelDraft struct {
	SystemTypeRef string
	StandardRef   string
	Name          string
	LocationName  string
}

// LocationLabelDraft is ComponentLabelDraft on the location tier. It carries no
// placement: a location's data map is its name and its type, and deliberately
// nothing about where it sits (ADR-0098's exclusion survives on this tier).
type LocationLabelDraft struct {
	LocationTypeRef string
	Name            string
}

// RenderComponentDraftLabel renders the label a component create would stamp,
// without creating anything.
//
// The two scopes are the placement's, not the component's: locationRead guards
// the location whose label the map reads, systemRead the system whose type
// label it reads. The permission to ASK is the component's own :create and
// lives at the route (see internal/api/label_draft.go); these two decide what
// the answer may contain.
func (p *PG) RenderComponentDraftLabel(ctx context.Context, d ComponentLabelDraft, locationRead, systemRead scope.Set) (DraftLabel, error) {
	productID, err := registryID(ctx, p.pool, "product", d.ProductName, ErrProductNotFound)
	if err != nil {
		return DraftLabel{}, err
	}
	in, err := componentLabelChain(ctx, p.pool, productID)
	if err != nil {
		return DraftLabel{}, err
	}
	c := Component{Name: d.Name}
	generated := d.Name == ""
	if generated {
		// The same refusal generateNameForProduct raises, from the same
		// resolved stem: a form must never lock a field over a value the
		// create then declines to produce. componentLabelChain has already
		// walked the chain for the map's Stem key, so this costs nothing.
		if in.stem == "" {
			return DraftLabel{}, fmt.Errorf("%w: product %q", ErrComponentTypeNoStem, d.ProductName)
		}
		c.Name = componentMint(in.stem).shape(OrdinalToken)
	}
	var pl componentPlacement
	if d.LocationName != "" {
		loc, err := p.GetLocation(ctx, d.LocationName, locationRead)
		if err != nil {
			return DraftLabel{}, err
		}
		pl.locationLabel = locationReadLabel(loc)
	}
	if d.SystemName != "" {
		sys, err := p.GetSystem(ctx, d.SystemName, systemRead)
		if err != nil {
			return DraftLabel{}, err
		}
		if pl.systemTypeLabel, err = systemTypeLabelOf(ctx, p.pool, sys.SystemTypeID); err != nil {
			return DraftLabel{}, err
		}
	}
	eng, err := p.labelEngine(ctx, p.pool)
	if err != nil {
		return DraftLabel{}, err
	}
	return draftedLabel(eng, in.rule, componentLabelData(&c, in, pl), generated), nil
}

// RenderSystemDraftLabel renders the label a system create would stamp.
// locationRead guards the one placement fact a system's map carries.
func (p *PG) RenderSystemDraftLabel(ctx context.Context, d SystemLabelDraft, locationRead scope.Set) (DraftLabel, error) {
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
	s := System{Name: d.Name}
	generated := d.Name == ""
	if generated {
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
		s.Name = systemMint(in.stem).shape(OrdinalToken)
	}
	var pl systemPlacement
	if d.LocationName != "" {
		loc, err := p.GetLocation(ctx, d.LocationName, locationRead)
		if err != nil {
			return DraftLabel{}, err
		}
		pl.locationLabel = locationReadLabel(loc)
	}
	eng, err := p.labelEngine(ctx, p.pool)
	if err != nil {
		return DraftLabel{}, err
	}
	return draftedLabel(eng, in.rule, systemLabelData(&s, in, pl), generated), nil
}

// RenderLocationDraftLabel renders the label a location create would stamp. It
// takes no scope because a location's data map reads nothing off another estate
// row: its keys are the location's own name and its type's display name, both
// of which the caller supplied or can read from the registry.
func (p *PG) RenderLocationDraftLabel(ctx context.Context, d LocationLabelDraft) (DraftLabel, error) {
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
	l := Location{Name: d.Name}
	generated := d.Name == ""
	if generated {
		if rule == nil {
			return DraftLabel{}, fmt.Errorf("%w: location_type %s", ErrLocationTypeNoNameRule, locationTypeID)
		}
		l.Name = rule.mint().shape(OrdinalToken)
	}
	eng, err := p.labelEngine(ctx, p.pool)
	if err != nil {
		return DraftLabel{}, err
	}
	// No ordinal override: a location's map deliberately carries no Ordinal key
	// (labels.go), so generated is not consulted here and draftedLabel's
	// override finds nothing to write.
	return draftedLabel(eng, in.rule, locationLabelData(&l, in), generated), nil
}

// draftedLabel executes a resolved rule over a drafted map, writing the ordinal
// token into the one key a row that does not exist cannot answer.
//
// The override happens here rather than inside the three data builders because
// those builders ARE the sandbox: what makes them safe is that a key not added
// there is a key no rule can reach, and a draft adds none. Ordinal is already a
// key on the two maps that have one, and its value for a generated name is
// genuinely unknown, so the token is what an honest render puts in it. An
// operator-typed name leaves it alone: that row will carry no ordinal at all
// (the column is null by design, #681), so the empty string ordinalText already
// produced is the true value and not a placeholder.
func draftedLabel(eng *label.Engine, rule string, data label.Data, generated bool) DraftLabel {
	if generated {
		if _, ok := data["Ordinal"]; ok {
			data["Ordinal"] = OrdinalToken
		}
	}
	return DraftLabel{Label: renderLabel(eng, rule, data), Rule: rule}
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
// display name, absent for an unclassified one. It is the single-row twin of
// the join componentPlacements does for a page, and the acceptance test (a
// drafted label compared with the one the create then stores, with a rule
// reading both placement facts) is what holds the two to the same answer.
func systemTypeLabelOf(ctx context.Context, q querier, systemTypeID *string) (string, error) {
	if systemTypeID == nil {
		return "", nil
	}
	var name string
	err := q.QueryRow(ctx, `select coalesce(display_name, '') from system_type where id = $1`, *systemTypeID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve system_type label %q: %w", *systemTypeID, err)
	}
	return name, nil
}
