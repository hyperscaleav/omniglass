package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/label"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/jackc/pgx/v5"
)

// The generated label (#682): the storage half of the rule engine in
// internal/label.
//
// # The ladder
//
// A labelled entity carries a stored pair, label and the pen
// label_generated, exactly the shape name/name_generated already has.
// Reading it is three rungs: the label an operator typed, else the one the rule
// produced, else the entity's name. The first two rungs are the same column,
// told apart by the pen, and the third is the read-side fallback the console
// already applies; nothing here renders on a read.
//
// # Why stored rather than rendered per read
//
// Sort, filter and search stay in SQL rather than becoming a permanent
// limitation the moment a list paginates, one mechanism covers both generated
// fields instead of two models for one concept, and no template executes across
// 15,000 rows on every read. The staleness that buys is paid for by the
// recompute-and-compare invariant test derived data already owes in this repo.
//
// # The tiers
//
// Most specific wins, and null at a tier means "no opinion, ask the next one
// out", the same three-state discipline component_type.stem uses:
//
//	component  product.label_rule  -> component_type chain -> label_rule.component
//	system     standard.label_rule -> system_type chain    -> label_rule.system
//	location                          location_type        -> label_rule.location
//
// Locations have two tiers rather than three, and that asymmetry is the design:
// there is no location-side counterpart of a product, because a location is not
// an instance of a catalog row.
//
// # What a rule reads about PLACEMENT, and what that cost (#685)
//
// A component's map carries the label of the location it sits at and the label
// of its primary system's TYPE; a system's map carries the label of the
// location it sits at. That is the worked example this epic exists to serve:
//
//	Boardroom 204B Display   from   {{.SystemTypeLabel}} {{.LocationLabel}} {{.TypeName}}
//
// The keys are FLAT, never dotted. The map is a closed map[string]string and
// that flatness is half the sandbox argument in ADR-0098: a value that is a
// string cannot be traversed through, so there is no {{.Location.Label}} to
// widen into a handle on another row.
//
// Which location, precisely: the row's OWN location_id, not an ancestor of it
// and not the location its plane root sits at. A component's label says where
// that component is, so it reads the column that says so; a nested component
// with no location of its own reads placement as absent, exactly as an unplaced
// one does. The alternative (walk to the plane root, read that) would make a
// label depend on an ANCESTOR COMPONENT's placement, so moving a parent would
// stale every descendant, and it costs a recursive walk per row on a path that
// already runs on every create.
//
// LocationLabel is the location's READ LADDER, the label an operator typed or
// the platform rendered, else the location's own name, not the raw column. A
// location rule ships now (#657), so most rows carry a label; the ladder is
// still what a rule reads, because an estate that predates the rule and has not
// run :recomputeLabels has none, and reading the column alone would render
// placement as blank for every row in it.
//
// # The cost of that, which is this whole slice
//
// Slice 2 could enumerate five write paths and call the set complete, because
// every fact its map held was the labelled row's own. That argument is void
// now, and it was weaker than it looked even then: TypeName, ProductName,
// VendorName, Stem and TypeAbbrev were already columns on OTHER rows (a
// registry's), and so was the rule itself. What placement changes is that the
// other row is now an ESTATE row an operator edits daily rather than a catalog
// row they edit rarely.
//
// The set of acts is therefore derived from the map rather than asserted, and
// split by blast radius (see label_recompute.go):
//
//   - Bounded by placement, so cascaded EAGERLY inside the act's own
//     transaction: the labelled row's own create/rename/move/reclassify/reset,
//     a location's rename or relabel or reclassify (the rows AT it), a system's
//     reclassify (its member components), and every act that moves a
//     component's primary membership.
//   - Bounded only by the estate, so left to the preview-then-apply verb: a
//     rule changing at any tier, a classification row's label, stem or
//     abbrev changing, and the acronym list changing. Rewriting 15,000 rows as
//     a side effect of one catalog edit is exactly what the epic refuses to do
//     silently.

// ErrInvalidLabelRule is a label rule that does not parse. It is refused at the
// surface that accepts the text (a 422 at rule-edit time), which is the whole
// reason a rule is compiled at the moment it is WRITTEN and not only at the
// moment it is used: a rule that cannot compile must never reach a column that
// a later write path will execute.
var ErrInvalidLabelRule = errors.New("storage: label rule does not parse")

// ErrLabelRuleKindUnknown is a global-tier read or write naming an entity kind
// that carries no label rule (the table's own CHECK refuses the write; this is
// the same refusal before the round trip, and with the offending kind named).
var ErrLabelRuleKindUnknown = errors.New("storage: unknown label rule entity kind")

// grammarEngine compiles a rule for VALIDATION, and carries no dictionary on
// purpose.
//
// Parsing and rendering need different engines, and it is worth being precise
// about why, because it is the whole reason an operator-editable dictionary did
// not ripple through every write path in this file. [label.New] binds the same
// four function NAMES whatever dictionary it is given, and the grammar check
// walks a static allowlist, so whether a rule parses is a fact about the rule
// alone: every engine agrees, and a validator needs no current one. What the
// dictionary changes is what `title` DOES when the compiled rule executes, which
// only the render path cares about.
//
// The corollary is a rule this file keeps: a template compiled here is thrown
// away. Rendering with it would render with an empty dictionary, and the failure
// would be a quietly mis-cased label rather than an error.
var grammarEngine = label.Basic

// labelEngine returns the engine for the acronym dictionary in force RIGHT NOW,
// resolved from the settings cascade (the shipped list, the operator file, the
// platform override) and cached against that dictionary, so an unchanged list
// costs a comparison rather than a rebuild.
//
// It resolves the setting per call rather than per process, which is what makes
// "an operator adds an acronym and it takes effect without a restart" true for
// free instead of true by way of an invalidation protocol somebody has to
// remember to fire. The cost is one read of a table with a row per overridden
// namespace, on a write path that is already several round trips deep. A caller
// rendering MANY rows (the bulk recompute, #685) resolves the engine once and
// passes it down; that is why the render functions below take one rather than
// reaching for it themselves.
//
// The querier is the caller's, and it is not a convenience. Every caller here is
// mid-transaction, so it is holding a connection; reading the override level off
// the POOL would acquire a second one, and a pool whose connections are all held
// by transactions each waiting for a second connection is deadlocked, not slow.
// Reading in the caller's transaction also means the dictionary and the row
// being stamped come from one snapshot.
func (p *PG) labelEngine(ctx context.Context, q querier) (*label.Engine, error) {
	doc, locks, err := p.settingLevel(ctx, q, "platform")
	if err != nil {
		return nil, fmt.Errorf("storage: resolve the acronym dictionary: %w", err)
	}
	s, err := settings.Typed(p.settings.ResolveOverride(doc, locks).Values)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve the acronym dictionary: %w", err)
	}
	return p.labelEngines.For(s.Label.Acronyms), nil
}

// LabelRuleKinds are the entity kinds that carry a global label rule, matching
// the label_rule table's CHECK. Node is deliberately absent: its name is a NATS
// subject kept globally unique, it has no type and no parent, so it is not a
// labelled entity in this sense.
var LabelRuleKinds = []string{"component", "system", "location"}

func validLabelRuleKind(kind string) bool {
	for _, k := range LabelRuleKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// validateLabelRule compiles a rule so a write can refuse it. A nil patch field
// (unchanged) and an empty rule (this tier has no opinion) are both fine; only
// text that does not compile is refused.
func validateLabelRule(rule *string) error {
	if rule == nil || *rule == "" {
		return nil
	}
	if _, err := grammarEngine.Parse(*rule); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidLabelRule, err)
	}
	return nil
}

// nilIfEmpty decodes the house three-state string sentinel for a nullable
// column: an omitted field is nil already, an explicit "" becomes SQL null, and
// a value passes through. So "nothing at this tier" has exactly one
// representation, where two spellings of absent would give the resolver a third
// state to decide about and a reader a difference to misinterpret.
//
// It started life on the label rule and generalized with #716, which put the
// same sentinel on the three other inherited registry facts (stem, icon,
// abbrev). The columns differ; the decoding does not, so it is one function.
func nilIfEmpty(s *string) *string {
	if s != nil && *s == "" {
		return nil
	}
	return s
}

// renderLabel is where a rule's SECOND failure mode is absorbed. A rule that
// does not parse was refused at edit time; a rule that fails while EXECUTING is
// only discoverable here, against one entity's facts, and by then a write is
// half done.
//
// So it degrades: an unrenderable rule yields no label, the row's pen keeps
// pointing at the platform, and the read ladder falls through to the entity's
// name. It never propagates, because the alternative is a create that fails on
// a template typo, or a bulk recompute that stops at whichever row first
// tripped it.
// The engine is passed in rather than fetched, because it carries the acronym
// dictionary and the dictionary is a setting: resolving it here would put a
// settings read inside every row of a bulk recompute.
func renderLabel(eng *label.Engine, rule string, data label.Data) string {
	if rule == "" {
		return ""
	}
	compiled, err := eng.Parse(rule)
	if err != nil {
		return ""
	}
	rendered, err := compiled.Render(data)
	if err != nil {
		return ""
	}
	return rendered
}

// --- the global tier ---------------------------------------------------------

// LabelRule is one row of the global tier: the rule that applies to an entity
// kind when neither its type nor its specific artifact has an opinion.
//
// Default is boot-seed space, rewritten authoritatively on every start so a
// later release can improve the shipped rule. Template is operator space, nil
// until an operator overrides it. Effective is which one wins, resolved here
// rather than left for each caller to coalesce.
type LabelRule struct {
	EntityKind string
	Default    string
	Template   *string
	Effective  string
}

const labelRuleCols = `entity_kind, default_template, template, coalesce(template, default_template)`

func scanLabelRule(row pgx.Row) (*LabelRule, error) {
	var r LabelRule
	if err := row.Scan(&r.EntityKind, &r.Default, &r.Template, &r.Effective); err != nil {
		return nil, err
	}
	return &r, nil
}

// UpsertLabelRuleDefault installs the rule this release ships for an entity
// kind, the boot-seed phase's write. Authoritative on default_template and
// deliberately blind to template: re-seeding restates what we ship and never
// reaches an operator's override, which is the whole reason the two are
// separate columns.
func (p *PG) UpsertLabelRuleDefault(ctx context.Context, kind, template string) error {
	if !validLabelRuleKind(kind) {
		return fmt.Errorf("%w: %q", ErrLabelRuleKindUnknown, kind)
	}
	if _, err := grammarEngine.Parse(template); err != nil {
		return fmt.Errorf("%w: shipped rule for %s: %s", ErrInvalidLabelRule, kind, err)
	}
	if _, err := p.pool.Exec(ctx, `
		insert into label_rule (entity_kind, default_template)
		values ($1, $2)
		on conflict (entity_kind) do update
			set default_template = excluded.default_template,
			    updated_at       = now()`, kind, template); err != nil {
		return fmt.Errorf("storage: upsert label rule %q: %w", kind, err)
	}
	return nil
}

// ListLabelRules returns the global tier, one row per entity kind.
func (p *PG) ListLabelRules(ctx context.Context) ([]LabelRule, error) {
	rows, err := p.pool.Query(ctx, `select `+labelRuleCols+` from label_rule order by entity_kind`)
	if err != nil {
		return nil, fmt.Errorf("storage: list label rules: %w", err)
	}
	defer rows.Close()
	var out []LabelRule
	for rows.Next() {
		r, err := scanLabelRule(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan label rule: %w", err)
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetLabelRule reads one entity kind's global rule.
func (p *PG) GetLabelRule(ctx context.Context, kind string) (*LabelRule, error) {
	if !validLabelRuleKind(kind) {
		return nil, fmt.Errorf("%w: %q", ErrLabelRuleKindUnknown, kind)
	}
	r, err := scanLabelRule(p.pool.QueryRow(ctx, `select `+labelRuleCols+` from label_rule where entity_kind = $1`, kind))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get label rule %q: %w", kind, err)
	}
	return r, nil
}

// SetLabelRule writes an operator's override of a kind's global rule, or clears
// it back to the shipped default when template is empty. Refuses a rule that
// does not compile (ErrInvalidLabelRule, a 422) before it is stored, and audits
// the change.
//
// Clearing is restore-to-defaults, and it needs no verb of its own the way
// :resetName does: unlike a name, a rule HAS a representable unset state, so
// the empty string says it.
func (p *PG) SetLabelRule(ctx context.Context, actorID, entityKind, template string) (*LabelRule, error) {
	if !validLabelRuleKind(entityKind) {
		return nil, fmt.Errorf("%w: %q", ErrLabelRuleKindUnknown, entityKind)
	}
	if err := validateLabelRule(&template); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set label rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := scanLabelRule(tx.QueryRow(ctx, `select `+labelRuleCols+` from label_rule where entity_kind = $1`, entityKind))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: load label rule %q: %w", entityKind, err)
	}
	after, err := scanLabelRule(tx.QueryRow(ctx, `
		update label_rule set template = $2, updated_at = now()
		where entity_kind = $1
		returning `+labelRuleCols, entityKind, nilIfEmpty(&template)))
	if err != nil {
		return nil, fmt.Errorf("storage: set label rule %q: %w", entityKind, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "label_rule", entityKind, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set label rule: %w", err)
	}
	return after, nil
}

// globalLabelRule reads the effective global rule for an entity kind inside the
// caller's transaction. A kind with no row at all (a database seeded before
// this slice, or a test that skipped the boot seed) is no rule rather than an
// error: the resolver's job is to answer "what rule applies", and "none" is an
// answer.
func globalLabelRule(ctx context.Context, q querier, kind string) (string, error) {
	var rule string
	err := q.QueryRow(ctx, `select coalesce(template, default_template) from label_rule where entity_kind = $1`, kind).Scan(&rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve global label rule %q: %w", kind, err)
	}
	return rule, nil
}

// firstRule returns the most specific tier with an opinion. Empty and null are
// the same answer at every tier (nilIfEmpty keeps writes to one spelling),
// so this is the whole of the precedence logic.
func firstRule(tiers ...string) string {
	for _, r := range tiers {
		if r != "" {
			return r
		}
	}
	return ""
}

// --- component ---------------------------------------------------------------

// componentLabelInputs is the per-PRODUCT half of a component's label: the
// resolved rule and every classification fact a rule can read. Split from the
// row's own facts because it is identical for every component sharing a
// product, which is what lets a bulk recompute (#685) resolve it once per
// distinct product rather than once per row.
type componentLabelInputs struct {
	rule                    string
	typeName, abbrev, stem  string
	productName, vendorName string
}

// componentLabelChain resolves the rule and the classification facts for one
// product: the product's own row and its component_type's label in a
// single query (shadow-resolved, so an operator's fork of a shipped type is
// what a rule sees), then the inherited facts up the type chain, then the
// global tier.
func componentLabelChain(ctx context.Context, q querier, productID string) (componentLabelInputs, error) {
	var (
		in       componentLabelInputs
		prodRule *string
		typeID   uuid.UUID
	)
	// The global tier rides along on this query rather than costing a round trip
	// of its own: it is a left join on a three-row table keyed by its primary
	// key, so it is free, and reading it unconditionally beats reading it only
	// when the two more specific tiers turn out empty. This is a WRITE path
	// every component create, rename, move, reclassify and reset goes through,
	// and the estate this epic exists for has 15,000 components in it.
	var global string
	err := q.QueryRow(ctx, `
		select p.label, coalesce(v.label, ''), p.label_rule, p.component_type_id,
		       coalesce(s.image->>'label', ct.label),
		       coalesce(lr.template, lr.default_template, '')
		from product p
		join component_type ct on ct.id = p.component_type_id
		left join vendor v on v.id = p.vendor_id
		left join registry_shadow s on s.registry = '`+componentTypeRegistry+`' and s.row_id = ct.id
		left join label_rule lr on lr.entity_kind = 'component'
		where p.id = $1`, productID).
		Scan(&in.productName, &in.vendorName, &prodRule, &typeID, &in.typeName, &global)
	if errors.Is(err, pgx.ErrNoRows) {
		return in, ErrProductNotFound
	}
	if err != nil {
		return in, fmt.Errorf("storage: resolve label facts for product %q: %w", productID, err)
	}
	stem, _, abbrev, chainRule, _, err := resolveTypeFacts(ctx, q, typeID)
	if err != nil {
		return in, fmt.Errorf("storage: resolve label facts for product %q: %w", productID, err)
	}
	in.stem, in.abbrev = stem, abbrev
	in.rule = firstRule(derefRule(prodRule), chainRule, global)
	return in, nil
}

// componentLabelData is the closed data map for one component, built from the
// one declaration of that map (componentLabelKeys in label_keys.go, #729) rather
// than from a literal here. The keys, their meanings and the docs that teach them
// are all readers of that declaration, so a key cannot be added to the sandbox
// without the page that teaches it moving too.
func componentLabelData(c *Component, in componentLabelInputs, pl componentPlacement) label.Data {
	return labelData(componentLabelKeys, componentFacts{c: c, in: in, pl: pl})
}

// componentPlacement is the per-ROW half of a component's label inputs: the two
// facts that live on rows the component does not own. Kept apart from
// componentLabelInputs because the two have different batching shapes, which is
// the whole difficulty of the bulk recompute: the classification half is
// identical for every component sharing a product, and this half is identical
// for nobody.
type componentPlacement struct {
	locationLabel   string
	systemTypeLabel string
}

// systemPlacement is componentPlacement on the system tier. A system reads only
// its location: its own type is already TypeName, and giving one fact two
// spellings is a trap for a rule author rather than a convenience.
type systemPlacement struct {
	locationLabel string
}

// locationReadLadder is the SQL for "what does this location READ as", the
// first two rungs of the ladder the console applies (the label an operator
// typed, else the location's own name). It is a fragment rather than a helper
// call because every use is inside a join in a larger query.
//
// The third rung, a label the platform generated, is the same column as the
// first: the pen tells them apart and a rule does not care which of the two it
// is reading, only that it is what an operator sees.
const locationReadLadder = `coalesce(nullif(l.label, ''), l.name, '')`

// locationReadLabel is locationReadLadder in Go, for the write paths that have
// the row in hand and need to know whether what a rule reads about this
// location just MOVED. The two must agree, and a test holds them to it: a
// cascade that fires on the column rather than on the ladder misses the case
// that matters most, an operator clearing a typed label so the name shows
// through.
func locationReadLabel(l *Location) string {
	if l.Label != "" {
		return l.Label
	}
	return l.Name
}

// componentPlacements resolves the placement facts for a whole page in ONE
// statement, whatever the page size. The single-row stamp calls it with one id
// rather than having a query of its own, so there is one definition of what
// LocationLabel and SystemTypeLabel mean and no second one to drift.
//
// The primary membership is a left join with the is_primary predicate ON the
// join, not in the WHERE: a component in no system at all must still come back
// with a row, carrying its location and an absent system type, or the caller
// would read "unbound" as "missing" and skip the stamp entirely.
func componentPlacements(ctx context.Context, q querier, ids []string) (map[string]componentPlacement, error) {
	out := make(map[string]componentPlacement, len(ids))
	seeds := distinctIDs(ids)
	if len(seeds) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		select c.id, `+locationReadLadder+`, coalesce(st.label, '')
		from component c
		left join location l on l.id = c.location_id
		left join system_member m on m.component_id = c.id and m.is_primary
		left join system s on s.id = m.system_id
		left join system_type st on st.id = s.system_type_id
		where c.id = any($1::uuid[])`, seeds)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve placement facts for %d components: %w", len(seeds), err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var pl componentPlacement
		if err := rows.Scan(&id, &pl.locationLabel, &pl.systemTypeLabel); err != nil {
			return nil, fmt.Errorf("storage: scan component placement facts: %w", err)
		}
		out[id] = pl
	}
	return out, rows.Err()
}

// systemPlacements is componentPlacements on the system tier.
func systemPlacements(ctx context.Context, q querier, ids []string) (map[string]systemPlacement, error) {
	out := make(map[string]systemPlacement, len(ids))
	seeds := distinctIDs(ids)
	if len(seeds) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		select s.id, `+locationReadLadder+`
		from system s
		left join location l on l.id = s.location_id
		where s.id = any($1::uuid[])`, seeds)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve placement facts for %d systems: %w", len(seeds), err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var pl systemPlacement
		if err := rows.Scan(&id, &pl.locationLabel); err != nil {
			return nil, fmt.Errorf("storage: scan system placement facts: %w", err)
		}
		out[id] = pl
	}
	return out, rows.Err()
}

// renderComponentLabel returns the label a component's rules produce, or "" for
// no rule, an unrenderable rule, or a rule with nothing to say about this row.
// A component with no product (unreachable through the API, which requires one)
// has no classification to resolve a rule from and gets no label.
func renderComponentLabel(ctx context.Context, q querier, eng *label.Engine, c *Component) (string, error) {
	if c.ProductID == nil {
		return "", nil
	}
	in, err := componentLabelChain(ctx, q, *c.ProductID)
	if err != nil {
		return "", err
	}
	places, err := componentPlacements(ctx, q, []string{c.ID})
	if err != nil {
		return "", err
	}
	return renderLabel(eng, in.rule, componentLabelData(c, in, places[c.ID])), nil
}

// stampComponentLabel re-renders a platform-owned label and writes it, returning
// the row as it now reads so a caller's audit image carries the label it just
// changed. A no-op when the operator holds the pen, and a no-op when the render
// matches what is already stored, so an unrelated write costs no statement.
//
// A rule producing nothing stores SQL NULL rather than an empty string. The pen
// stays with the platform either way: a rule that has nothing to say about this
// row today says something tomorrow, and a row marked operator-owned because a
// rule once rendered empty would never be reconsidered by the recompute that
// exists to fix exactly that.
func (p *PG) stampComponentLabel(ctx context.Context, tx pgx.Tx, c *Component) (*Component, error) {
	if !c.LabelGenerated {
		return c, nil
	}
	eng, err := p.labelEngine(ctx, tx)
	if err != nil {
		return nil, err
	}
	rendered, err := renderComponentLabel(ctx, tx, eng, c)
	if err != nil {
		return nil, err
	}
	if rendered == c.Label {
		return c, nil
	}
	stamped, err := scanComponent(tx.QueryRow(ctx,
		`update component set label = $2, updated_at = now() where id = $1 returning `+componentCols,
		c.ID, rendered))
	if err != nil {
		return nil, fmt.Errorf("storage: stamp component label: %w", err)
	}
	return stamped, nil
}

// --- system ------------------------------------------------------------------

type systemLabelInputs struct {
	rule                   string
	typeName, abbrev, stem string
	standardName           string
}

// systemLabelChain resolves a system's rule and classification facts, reading
// the global tier itself. It is the single-row entry point; a bulk recompute
// calls systemLabelChainWith with a global rule it read once for the whole
// page, which is why the two are split.
func systemLabelChain(ctx context.Context, q querier, s *System) (systemLabelInputs, error) {
	global, err := globalLabelRule(ctx, q, "system")
	if err != nil {
		return systemLabelInputs{}, err
	}
	return systemLabelChainWith(ctx, q, s.StandardID, s.SystemTypeID, global)
}

// systemLabelChainWith resolves a system's rule and classification facts from
// its two classifier ids. Both are optional (a one-off system conforms to no
// standard, and a system need not be typed), so each leg is skipped rather than
// defaulted when its id is absent.
//
// It takes the ids rather than the *System because the answer depends on
// nothing else about the row: every system sharing a (standard, system_type)
// pair gets the identical result, which is what lets a recompute over 15,000
// systems resolve it once per distinct pair instead of once per row.
func systemLabelChainWith(ctx context.Context, q querier, standardID, systemTypeID *string, global string) (systemLabelInputs, error) {
	var in systemLabelInputs
	var standardRule, typeRule string
	if standardID != nil {
		var rule *string
		if err := q.QueryRow(ctx, `select label, label_rule from standard where id = $1`, *standardID).
			Scan(&in.standardName, &rule); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return in, fmt.Errorf("storage: resolve label facts for standard %q: %w", *standardID, err)
		}
		standardRule = derefRule(rule)
	}
	if systemTypeID != nil {
		typeID, err := uuid.Parse(*systemTypeID)
		if err != nil {
			return in, fmt.Errorf("storage: system_type id %q is not a uuid: %w", *systemTypeID, err)
		}
		if err := q.QueryRow(ctx, `select label from system_type where id = $1`, typeID).Scan(&in.typeName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return in, fmt.Errorf("storage: resolve label facts for system_type %q: %w", typeID, err)
		}
		stem, _, abbrev, chainRule, err := resolveSystemTypeFacts(ctx, q, typeID)
		if err != nil {
			return in, fmt.Errorf("storage: resolve label facts for system_type %q: %w", typeID, err)
		}
		in.stem, in.abbrev, typeRule = stem, abbrev, chainRule
	}
	in.rule = firstRule(standardRule, typeRule, global)
	return in, nil
}

// systemLabelData is the closed data map for one system, built from
// systemLabelKeys. Ordinal joined it with #686, which gave a system a generated
// name and the number behind it, and #693 made the shipped rule READ it, which
// is where the two halves of a divisible boardroom stop reading alike.
//
// The value is the number the system's NAME carries (see mintedOrdinalText), so
// the first of its stem is empty here exactly as it is bare there. The mint is
// resolved from the same stem the generator mints from, rather than by looking
// at the name, because the suppression is a property of the mint (ADR-0101) and
// reading it off the string would be a second implementation of the rule.
func systemLabelData(s *System, in systemLabelInputs, pl systemPlacement) label.Data {
	return labelData(systemLabelKeys, systemFacts{s: s, in: in, pl: pl})
}

func renderSystemLabel(ctx context.Context, q querier, eng *label.Engine, s *System) (string, error) {
	in, err := systemLabelChain(ctx, q, s)
	if err != nil {
		return "", err
	}
	places, err := systemPlacements(ctx, q, []string{s.ID})
	if err != nil {
		return "", err
	}
	return renderLabel(eng, in.rule, systemLabelData(s, in, places[s.ID])), nil
}

// stampSystemLabel is stampComponentLabel on the system tier; see that function
// for the pen and empty-render reasoning.
func (p *PG) stampSystemLabel(ctx context.Context, tx pgx.Tx, s *System) (*System, error) {
	if !s.LabelGenerated {
		return s, nil
	}
	eng, err := p.labelEngine(ctx, tx)
	if err != nil {
		return nil, err
	}
	rendered, err := renderSystemLabel(ctx, tx, eng, s)
	if err != nil {
		return nil, err
	}
	if rendered == s.Label {
		return s, nil
	}
	stamped, err := scanSystem(tx.QueryRow(ctx,
		`update system set label = $2, updated_at = now() where id = $1 returning `+systemCols,
		s.ID, rendered))
	if err != nil {
		return nil, fmt.Errorf("storage: stamp system label: %w", err)
	}
	return stamped, nil
}

// --- location ----------------------------------------------------------------

type locationLabelInputs struct {
	rule     string
	typeName string
}

// locationLabelChain resolves a location's rule and classification facts,
// reading the global tier itself. Split from locationLabelChainWith for the
// reason systemLabelChain is: a recompute reads the global rule once.
func locationLabelChain(ctx context.Context, q querier, l *Location) (locationLabelInputs, error) {
	global, err := globalLabelRule(ctx, q, "location")
	if err != nil {
		return locationLabelInputs{}, err
	}
	return locationLabelChainWith(ctx, q, l.LocationTypeID, global)
}

// locationLabelChainWith resolves a location's rule and classification facts
// from its type id. Two tiers rather than three: there is no location-side
// counterpart of a product, because a location is not an instance of a catalog
// row.
func locationLabelChainWith(ctx context.Context, q querier, locationTypeID, global string) (locationLabelInputs, error) {
	var in locationLabelInputs
	// Resolved over the operator's shadow (#703): a forked shipped type's
	// label and label_rule are the ones its locations are labelled by,
	// or the fork would be invisible on the surface it exists to change.
	lt, err := scanLocationTypeResolved(q.QueryRow(ctx, locationTypeResolved+` where lt.id = $1`, locationTypeID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return in, fmt.Errorf("storage: resolve label facts for location_type %q: %w", locationTypeID, err)
	}
	if lt != nil {
		in.typeName = lt.Label
		in.rule = firstRule(derefRule(lt.LabelRule), global)
		return in, nil
	}
	in.rule = firstRule("", global)
	return in, nil
}

// locationLabelData is the closed data map for one location, built from
// locationLabelKeys, whose comment carries why the absences are absent.
//
// The Ordinal argument is worth keeping here beside the other two maps that DO
// carry the key. #687 gave a location an ordinal, and the key would still be
// REDUNDANT now that #693 has settled what it would hold: the number the name
// shows. A positional location's name IS that number, so "{{.TypeName}}
// {{.Name}}" renders "Floor 3" with nothing added, and a stemmed one carries it
// in the name too, so the shipped rule's "{{title (words .Name)}}" reads "Wing 2"
// off `wing-2` and "Wing" off the suppressed `wing`.
//
// The older half of this argument was that a key here would print "Wing 1" for
// the only wing in a building, which the epic was filed about. That is no
// longer what it would do (mintedOrdinalText suppresses it), so it is retired
// rather than repeated: redundancy is the reason that survives. Adding a key is
// the only way to widen what a rule can see, so it is not added on a maybe.
func locationLabelData(l *Location, in locationLabelInputs) label.Data {
	return labelData(locationLabelKeys, locationFacts{l: l, in: in})
}

func renderLocationLabel(ctx context.Context, q querier, eng *label.Engine, l *Location) (string, error) {
	in, err := locationLabelChain(ctx, q, l)
	if err != nil {
		return "", err
	}
	return renderLabel(eng, in.rule, locationLabelData(l, in)), nil
}

// stampLocationLabel is stampComponentLabel on the location tier; see that
// function for the pen and empty-render reasoning.
func (p *PG) stampLocationLabel(ctx context.Context, tx pgx.Tx, l *Location) (*Location, error) {
	if !l.LabelGenerated {
		return l, nil
	}
	eng, err := p.labelEngine(ctx, tx)
	if err != nil {
		return nil, err
	}
	rendered, err := renderLocationLabel(ctx, tx, eng, l)
	if err != nil {
		return nil, err
	}
	if rendered == l.Label {
		return l, nil
	}
	stamped, err := scanLocation(tx.QueryRow(ctx,
		`update location set label = $2, updated_at = now() where id = $1 returning `+locationCols,
		l.ID, rendered))
	if err != nil {
		return nil, fmt.Errorf("storage: stamp location label: %w", err)
	}
	return stamped, nil
}

// --- shared helpers ----------------------------------------------------------

// derefRule reads a nullable rule column as the empty string, the one spelling
// of "this tier has no opinion" the resolver understands.
func derefRule(rule *string) string {
	if rule == nil {
		return ""
	}
	return *rule
}

// mintedOrdinalText renders an ordinal for the data map: the number the row's
// NAME carries, and the empty string when it carries none.
//
// Empty is what a rule prints for it and what {{if}} reads as false, so
// "{{.TypeName}}{{if .Ordinal}} {{.Ordinal}}{{end}}" is the whole of the
// suppression rule an operator has to write, on either tier.
//
// A row reaches the empty case two ways and they mean the same thing to a
// reader. An operator named it, so the platform allocated no number at all; or
// the platform named it and its mint SUPPRESSED the number, which is the first
// of its stem in a bucket (ADR-0101: a room's only boardroom is `boardroom`,
// not `boardroom-1`).
//
// The second case is #693 and it reverses what this map said before. The key
// used to be the stored ordinal verbatim, on the argument that a rule should
// read the number rather than the name's spelling of it; the shipped system
// rule then declined to read it, because "Boardroom 1" beside a system NAMED
// `boardroom` is the exact defect the ordinal's suppression exists to prevent.
// Deriving the value from the mint settles that once, for every rule, instead
// of leaving each author to write the suppression by hand and get "Boardroom 1"
// when they forget: the key is the number the name shows, so a label and a name
// cannot disagree about how many of a thing there are.
func mintedOrdinalText(m nameMint, n *int) string {
	if n == nil || m.suppresses(*n) {
		return ""
	}
	return fmt.Sprintf("%d", *n)
}

// labelPen decides who owns the label after a write that carried a label
// field, the same three-state every patch field uses. A nil field leaves the
// pen where it is; a value claims it for the operator; an explicit empty string
// hands it back to the platform, and the caller then stamps.
//
// This is the mechanism behind "setting a label by hand clears the pen, and
// clearing the field returns it": both halves are one comparison, not two
// separate code paths that could drift apart.
func labelPen(current bool, patch *string) bool {
	if patch == nil {
		return current
	}
	return *patch == ""
}
