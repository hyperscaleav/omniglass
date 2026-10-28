package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// PropertyType is a canonical registered signal on the property lane of the
// catalog (#587): the non-numeric value domain a sample observes and a field
// declares (string/bool/json). Numeric signals live in MetricType, which carries
// the numeric facts (unit, precision); the lane IS the value type, so no kind
// discriminator remains. Validation/FusionPolicy are raw jsonb passed through.
// Official marks a seed-owned, read-only property.
type PropertyType struct {
	// ID is the uuid primary key; Name is the renameable handle (ADR-0062). A
	// property is addressed by name on the wire, and the id is the stable form the
	// contract and telemetry foreign keys store.
	ID           string
	Name         string
	Label        string
	DataType     string
	Validation   []byte
	FusionPolicy []byte
	Description  string
	Official     bool
	// RetentionDays is the type's own retention window in days, overriding
	// its lane's; nil is the lane's (#417). Set by SetPropertyTypeRetention,
	// official types included.
	RetentionDays *int
	// WrittenBy is the calc rule that writes this property, nil when none
	// does (#1019). Read beside the type, never stored or audited.
	WrittenBy *CalcWriter `json:"-"`
	// WriterClass is who may write this property (ADR-0161): resolved from
	// its name and WrittenBy, never stored, so it cannot drift from what a
	// write is held to.
	WriterClass writerclass.Class `json:"-"`
}

// UpsertPropertyType installs an official property, authoritative on conflict (the
// boot-seed bucket): an operator's custom properties (official=false) are keyed by a
// distinct name and untouched. The validation fragment passes the same schema guard
// as the CRUD lane: the seed must not ship a schema an operator would be refused.
func (p *PG) UpsertPropertyType(ctx context.Context, prop PropertyType) error {
	if err := checkSchemaFragment(prop.Validation, "validation"); err != nil {
		return fmt.Errorf("storage: upsert property %q: %w: %w", prop.Name, ErrPropertyTypeInvalid, err)
	}
	_, err := p.pool.Exec(ctx, `
		insert into property_type (name, label, data_type, validation, fusion_policy, description, official)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (name) do update set
			label = excluded.label, data_type = excluded.data_type,
			validation = excluded.validation, fusion_policy = excluded.fusion_policy,
			description = excluded.description, official = excluded.official`,
		prop.Name, labelOrNull(prop.Label), prop.DataType, prop.Validation, prop.FusionPolicy, prop.Description, prop.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert property %q: %w", prop.Name, err)
	}
	return nil
}

// ListPropertyTypes returns every registered property (official and custom). No
// scope.Set: the registry is fleet-wide reference data, not a scoped resource.
func (p *PG) ListPropertyTypes(ctx context.Context) ([]PropertyType, error) {
	rows, err := p.pool.Query(ctx, propertyTypeListSQL)
	if err != nil {
		return nil, fmt.Errorf("storage: list properties: %w", err)
	}
	defer rows.Close()
	var out []PropertyType
	for rows.Next() {
		prop, err := scanPropertyType(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan property: %w", err)
		}
		out = append(out, *prop)
	}
	return out, rows.Err()
}
