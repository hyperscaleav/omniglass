package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// A standard created from a template (#827, ADR-0827): a standard written whole from a
// template, in one transaction. What it writes is an ordinary operator-owned
// standard (official false) with its choices, roles, contract lines and map,
// exactly what the standard's own routes would write piece by piece, and
// nothing that points back at the template: the template's name is recorded
// in the one audit row and nowhere else, so a release can rewrite the
// template without touching the standard.

// ErrTemplateParentMissing is a variant created before its parent: the standard
// it would hang off does not exist. Chains are not created in one call, so the
// parent is created first.
var ErrTemplateParentMissing = errors.New("storage: the standard a variant hangs off does not exist")

// StandardFromTemplate is a whole standard to write. It names everything by value
// (types, products, properties and metrics by name, a role's alternate by
// choice and alternate name), so the caller resolves nothing.
type StandardFromTemplate struct {
	// Template is the template this standard is created from, recorded in the audit
	// detail only.
	Template string
	Name     string
	Label    string
	// Parent is the standard a variant hangs off, by name or uuid; empty for
	// none. It must exist (ErrTemplateParentMissing).
	Parent     string
	Choices    []RoleChoiceSpec
	Roles      []TemplateRole
	Properties []StandardPropertySpec
	Metrics    []StandardMetricSpec
	// Map is the layout declaration, nil for none.
	Map json.RawMessage
}

// TemplateRole is one role of a standard created from a template. Choice and Alternate name the alternate a
// role joins, both or neither; the spec's AlternateID is ignored.
type TemplateRole struct {
	SystemRoleSpec
	Choice    string
	Alternate string
}

// standardFromTemplateAudit is the audit image of a standard created from a template: the standard as written, the
// template it came from, and what the template declared on it.
type standardFromTemplateAudit struct {
	*Standard
	Template   string   `json:"from_template"`
	Choices    []string `json:"choices,omitempty"`
	Roles      []string `json:"roles"`
	Properties []string `json:"properties,omitempty"`
	Metrics    []string `json:"metrics,omitempty"`
}

// CreateStandardFromTemplate writes f as one standard, audited once as a create carrying from_template, the
// template. Refusals: an illegal name is the name rule's error; a taken name
// is ErrTypeExists; a missing parent is ErrTemplateParentMissing; a role, type,
// product, property or metric that does not resolve is the error its own
// route would give (ErrRoleRefNotFound, ErrPropertyTypeNotFound,
// ErrMetricTypeNotFound); an invalid map is ErrStandardMapInvalid. Nothing
// lands on a refusal.
func (p *PG) CreateStandardFromTemplate(ctx context.Context, actorID string, f StandardFromTemplate) (*Standard, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create standard from template: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created, err := p.createStandardFromTemplateTx(ctx, tx, actorID, f)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create standard from template: %w", err)
	}
	return created, nil
}

// createStandardFromTemplateTx is CreateStandardFromTemplate inside the caller's transaction: the write a
// standard route and a system created from a template (CreatePlan)
// share.
func (p *PG) createStandardFromTemplateTx(ctx context.Context, tx pgx.Tx, actorID string, f StandardFromTemplate) (*Standard, error) {
	if err := ValidateName("standard", f.Name); err != nil {
		return nil, err
	}
	if err := validateStandardMap(f.Map); err != nil {
		return nil, err
	}
	// First, before any other lock: see roleNameLockKey. The roles below are
	// written on a standard no system conforms to yet, so no name can
	// collide, but the lock order is every role write's.
	if err := lockRoleNames(ctx, tx); err != nil {
		return nil, err
	}

	var parent *string
	if f.Parent != "" {
		var id string
		err := tx.QueryRow(ctx, `select id from standard where `+registryRefCol(f.Parent)+` = $1`, f.Parent).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %q", ErrTemplateParentMissing, f.Parent)
		}
		if err != nil {
			return nil, fmt.Errorf("storage: resolve the template parent %q: %w", f.Parent, err)
		}
		parent = &id
	}
	var mapArg any
	if len(f.Map) > 0 && string(f.Map) != "null" {
		mapArg = []byte(f.Map)
	}
	created, err := scanStandard(tx.QueryRow(ctx, `
		insert into standard (name, official, label, parent_standard_id, map)
		values ($1, false, $2, $3, $4)
		returning `+standardCols,
		f.Name, labelOrNull(f.Label), parent, mapArg))
	if err != nil {
		return nil, mapStandardWriteErr(err)
	}

	audit := standardFromTemplateAudit{Standard: created, Template: f.Template, Roles: []string{}}
	alts := map[string]string{}
	for _, c := range f.Choices {
		ids, err := templateChoice(ctx, tx, created.ID, c)
		if err != nil {
			return nil, err
		}
		for name, id := range ids {
			alts[c.Name+"/"+name] = id
		}
		audit.Choices = append(audit.Choices, c.Name)
	}
	for _, r := range f.Roles {
		if err := templateRole(ctx, tx, created.ID, r, alts); err != nil {
			return nil, err
		}
		audit.Roles = append(audit.Roles, r.Name)
	}
	for _, l := range f.Properties {
		if _, err := upsertStandardPropertyRow(ctx, tx, created.ID, l); err != nil {
			return nil, err
		}
		audit.Properties = append(audit.Properties, l.PropertyTypeName)
	}
	for _, l := range f.Metrics {
		if _, err := upsertStandardMetricRow(ctx, tx, created.ID, l); err != nil {
			return nil, err
		}
		audit.Metrics = append(audit.Metrics, l.MetricTypeName)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "standard", created.ID, nil, audit); err != nil {
		return nil, err
	}
	return created, nil
}

// templateChoice writes one choice and its alternates on a fresh standard,
// positions in declaration order, and returns the alternate ids by name.
func templateChoice(ctx context.Context, tx pgx.Tx, standardID string, c RoleChoiceSpec) (map[string]string, error) {
	if err := ValidateName("role_choice", c.Name); err != nil {
		return nil, err
	}
	var choiceID string
	if err := tx.QueryRow(ctx, `
		insert into role_choice (owner_kind, standard_id, name, label)
		values ('standard', $1, $2, $3) returning id`,
		standardID, c.Name, labelOrNull(c.Label)).Scan(&choiceID); err != nil {
		return nil, mapRoleWriteErr(err)
	}
	out := make(map[string]string, len(c.Alternates))
	for i, a := range c.Alternates {
		if err := ValidateName("choice_alternate", a.Name); err != nil {
			return nil, err
		}
		var id string
		if err := tx.QueryRow(ctx, `
			insert into choice_alternate (choice_id, owner_kind, standard_id, system_id, name, label, position)
			select rc.id, rc.owner_kind, rc.standard_id, rc.system_id, $2, $3, $4
			from role_choice rc where rc.id = $1
			returning id`, choiceID, a.Name, labelOrNull(a.Label), i+1).Scan(&id); err != nil {
			return nil, mapRoleWriteErr(err)
		}
		out[a.Name] = id
	}
	return out, nil
}

// templateRole writes one role and its typed slot on a fresh standard. A type or
// product the catalogs lack is ErrRoleRefNotFound, as SetSystemRole gives.
func templateRole(ctx context.Context, tx pgx.Tx, standardID string, r TemplateRole, alts map[string]string) error {
	if err := ValidateName("system_role", r.Name); err != nil {
		return err
	}
	impact := r.Impact
	if impact == "" {
		impact = "degraded"
	}
	if !roleImpacts[impact] {
		return ErrRoleImpact
	}
	var altID string
	if r.Choice != "" || r.Alternate != "" {
		altID = alts[r.Choice+"/"+r.Alternate]
		if altID == "" {
			return fmt.Errorf("%w: role %s names choice/alternate %s/%s, which the template does not declare",
				ErrRoleRefNotFound, r.Name, r.Choice, r.Alternate)
		}
	}
	label := r.Label
	if label == "" {
		label = r.Name
	}
	var id string
	if err := tx.QueryRow(ctx, `
		insert into system_role (owner_kind, standard_id, name, label, quorum, capacity, position_labels, impact, alternate_id)
		values ('standard', $1, $2, $3, $4, $5, $6, $7, nullif($8, '')::uuid)
		returning id`,
		standardID, r.Name, label, max(r.Quorum, 1), r.Capacity, normalizePositionLabels(r.PositionLabels), impact, altID).Scan(&id); err != nil {
		return mapRoleWriteErr(err)
	}
	return insertSeededSlot(ctx, tx, id, r.AcceptedTypes, r.PinnedProducts)
}
