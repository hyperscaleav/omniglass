package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/jackc/pgx/v5"
)

// Attach-layer sentinels (#813). ErrSpecInvalid marks a driver spec refused at
// write (create, update, or seed): the message names the fault, and the API
// tier surfaces it as a 422. ErrAttachInvalid marks an attach whose inputs do
// not satisfy the spec (missing, undeclared, or a secret reference that does
// not resolve), also a 422.
var (
	ErrSpecInvalid   = errors.New("storage: driver spec invalid")
	ErrAttachInvalid = errors.New("storage: driver attach invalid")
)

// catalogMaps is the storage-side driver.Catalog: a snapshot of the catalog
// names loaded in one pass, so validation never interleaves queries with the
// pure walk.
type catalogMaps struct {
	lanes    map[string]string
	commands map[string]bool
	secrets  map[string]bool
}

func (c catalogMaps) LaneOf(name string) (string, bool) {
	lane, ok := c.lanes[name]
	return lane, ok
}
func (c catalogMaps) CommandTypeExists(name string) bool { return c.commands[name] }
func (c catalogMaps) SecretTypeExists(name string) bool  { return c.secrets[name] }

// loadDriverCatalog snapshots the names a spec may reference: the metric and
// property catalogs as emit lanes, the command types, the secret types.
func loadDriverCatalog(ctx context.Context, q querier) (catalogMaps, error) {
	cat := catalogMaps{
		lanes:    map[string]string{},
		commands: map[string]bool{},
		secrets:  map[string]bool{},
	}
	rows, err := q.Query(ctx, `
		select name, 'metric' from metric_type
		union all
		select name, 'property' from property_type`)
	if err != nil {
		return cat, fmt.Errorf("storage: load emit catalogs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, lane string
		if err := rows.Scan(&name, &lane); err != nil {
			return cat, fmt.Errorf("storage: scan emit catalog: %w", err)
		}
		// The canon test keeps the lanes disjoint; first write wins if a
		// stranger ever collides, and validation still resolves.
		if _, dup := cat.lanes[name]; !dup {
			cat.lanes[name] = lane
		}
	}
	if err := rows.Err(); err != nil {
		return cat, fmt.Errorf("storage: read emit catalogs: %w", err)
	}
	for col, into := range map[string]map[string]bool{"command_type": cat.commands, "secret_type": cat.secrets} {
		rows, err := q.Query(ctx, `select name from `+col)
		if err != nil {
			return cat, fmt.Errorf("storage: load %s catalog: %w", col, err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return cat, fmt.Errorf("storage: scan %s catalog: %w", col, err)
			}
			into[name] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return cat, fmt.Errorf("storage: read %s catalog: %w", col, err)
		}
	}
	return cat, nil
}

// validateDriverSpec is the write gate every driver-spec path shares (create,
// update, seed upsert): parse strictly, then validate against the live
// catalogs. A nil/empty spec passes: a stub row is legal, it just cannot be
// attached.
func (p *PG) validateDriverSpec(ctx context.Context, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	sp, err := driver.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSpecInvalid, err)
	}
	cat, err := loadDriverCatalog(ctx, p.pool)
	if err != nil {
		return err
	}
	if err := sp.Validate(cat); err != nil {
		return fmt.Errorf("%w: %v", ErrSpecInvalid, err)
	}
	return nil
}

// attachPlan is what resolveAttach hands CreateEndpoint: the endpoint fields
// the spec derives, and what deriveDriverTasks needs afterwards.
type attachPlan struct {
	driverID  string
	driver    *Driver
	spec      *driver.Spec
	transport string
	params    []byte
	inputs    []byte
	catalog   catalogMaps
}

// resolveAttach turns a driver reference plus supplied inputs into the derived
// endpoint fields, refusing (ErrAttachInvalid) anything the spec does not
// sanction: an undeclared input, a missing required one, a secret reference
// that resolves to nothing or to the wrong shape. Defaults are baked here, so
// the stored inputs are the effective ones the derived tasks assume.
func resolveAttach(ctx context.Context, q querier, es EndpointSpec) (*attachPlan, error) {
	if es.Transport != "" {
		return nil, fmt.Errorf("%w: an attach declares a driver, and the transport is the spec's fact; drop the transport field", ErrAttachInvalid)
	}
	d, err := loadDriverRef(ctx, q, *es.Driver)
	if err != nil {
		return nil, err
	}
	if len(d.Spec) == 0 {
		return nil, fmt.Errorf("%w: driver %q is a stub with no spec", ErrAttachInvalid, d.Name)
	}
	sp, err := driver.Parse(d.Spec)
	if err != nil {
		// A stored spec is validated at write, so this is corruption, not input.
		return nil, fmt.Errorf("storage: stored spec for driver %q does not parse: %w", d.Name, err)
	}

	declared := map[string]driver.Input{}
	for _, in := range sp.Inputs {
		declared[in.Name] = in
	}
	for name := range es.Inputs {
		if _, ok := declared[name]; !ok {
			return nil, fmt.Errorf("%w: input %q is not declared by driver %q", ErrAttachInvalid, name, d.Name)
		}
	}
	effective := map[string]string{}
	for _, in := range sp.Inputs {
		v, supplied := es.Inputs[in.Name]
		switch {
		case supplied:
			effective[in.Name] = v
		case in.Default != "":
			effective[in.Name] = in.Default
		case in.Required:
			return nil, fmt.Errorf("%w: required input %q not supplied", ErrAttachInvalid, in.Name)
		}
	}
	for _, in := range sp.Inputs {
		if in.Kind != "secret" {
			continue
		}
		ref, ok := effective[in.Name]
		if !ok {
			continue
		}
		var typeName string
		err := q.QueryRow(ctx, `
			select st.name from secret s join secret_type st on st.id = s.secret_type
			where s.name = $1`, ref).Scan(&typeName)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: input %q references secret %q, which does not exist", ErrAttachInvalid, in.Name, ref)
		}
		if err != nil {
			return nil, fmt.Errorf("storage: resolve secret reference %q: %w", ref, err)
		}
		if typeName != in.SecretType {
			return nil, fmt.Errorf("%w: input %q needs a %s secret, and %q is a %s", ErrAttachInvalid, in.Name, in.SecretType, ref, typeName)
		}
	}

	params, err := json.Marshal(attachParams(effective))
	if err != nil {
		return nil, fmt.Errorf("storage: marshal attach params: %w", err)
	}
	inputs, err := json.Marshal(effective)
	if err != nil {
		return nil, fmt.Errorf("storage: marshal attach inputs: %w", err)
	}
	cat, err := loadDriverCatalog(ctx, q)
	if err != nil {
		return nil, err
	}
	return &attachPlan{
		driverID:  d.ID,
		driver:    d,
		spec:      sp,
		transport: sp.Transport,
		params:    params,
		inputs:    inputs,
		catalog:   cat,
	}, nil
}

// attachParams derives the transport params from the effective inputs by the
// engine's naming contract: an input named `host` is the address, an input
// named `port` joins it. Everything else stays in inputs, read by the
// interpreter, not the dialer.
func attachParams(effective map[string]string) map[string]string {
	params := map[string]string{}
	if host, ok := effective["host"]; ok {
		if port, ok := effective["port"]; ok && port != "" {
			params["target"] = net.JoinHostPort(host, port)
		} else {
			params["target"] = host
		}
	}
	return params
}

// loadDriverRef resolves a driver by name or uuid inside the caller's
// transaction, ErrTypeNotFound when absent (the registry's own sentinel, which
// the API tier maps to a 422 reference fault).
func loadDriverRef(ctx context.Context, q querier, ref string) (*Driver, error) {
	d, err := scanDriver(q.QueryRow(ctx, `select `+driverCols+` from driver where `+registryRefCol(ref)+` = $1`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: load driver %q: %w", ref, err)
	}
	return d, nil
}

// bakeEmits resolves each emit's lane (the compile step of the authoring
// model, ADR-0135) into the shared runtime unit the node parses back
// (driver.BakedEmit), so the two sides can never drift on the shape.
func bakeEmits(emits []driver.Emit, cat catalogMaps) ([]driver.BakedEmit, error) {
	out := make([]driver.BakedEmit, 0, len(emits))
	for _, em := range emits {
		lane, ok := cat.LaneOf(em.Name)
		if !ok {
			// Validated at spec write; a catalog row deleted since is the one
			// path here, and refusing names it.
			return nil, fmt.Errorf("%w: emitted name %q is no longer in any catalog", ErrAttachInvalid, em.Name)
		}
		out = append(out, driver.BakedEmit{Name: em.Name, Lane: lane, Extract: em.Extract, Transform: em.Transform})
	}
	return out, nil
}

// deriveDriverTasks writes the derived work of an attach inside the create
// transaction: a poll task per poll function, a standing listen task per
// listener (command bindings derive nothing; they actuate on demand). Ids are
// the same content hash every derived task uses, so a re-derive dedupes.
func deriveDriverTasks(ctx context.Context, tx pgx.Tx, endpointID string, plan *attachPlan) error {
	insert := func(mode string, body any) error {
		spec, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("storage: marshal derived task: %w", err)
		}
		id := taskID(endpointID, mode, spec)
		if _, err := tx.Exec(ctx, `
			insert into task (id, mode, endpoint_id, spec, enabled)
			values ($1, $2, $3, $4, true)
			on conflict (id) do nothing`, id, mode, endpointID, spec); err != nil {
			return fmt.Errorf("storage: derive %s task for endpoint %q: %w", mode, endpointID, err)
		}
		return nil
	}
	for _, p := range plan.spec.Polls {
		emits, err := bakeEmits(p.Emits, plan.catalog)
		if err != nil {
			return err
		}
		sched, req := p.Schedule, p.Request
		if err := insert("poll", driver.BakedFunction{
			Driver: plan.driver.Name, Version: plan.driver.Version, Function: p.Name,
			Schedule: &sched, Request: &req, Emits: emits,
		}); err != nil {
			return err
		}
	}
	for _, l := range plan.spec.Listeners {
		emits, err := bakeEmits(l.Emits, plan.catalog)
		if err != nil {
			return err
		}
		match := l.Match
		if err := insert("listen", driver.BakedFunction{
			Driver: plan.driver.Name, Version: plan.driver.Version, Function: l.Name,
			Arm: l.Arm, Match: &match, Emits: emits,
		}); err != nil {
			return err
		}
	}
	return nil
}

// specOrNull binds an optional jsonb spec: empty stores SQL NULL, mirroring
// what nilIfEmpty does for text columns.
func specOrNull(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// secretRefsOf maps a driver task's secret inputs to the secret rows the
// attach recorded: the spec says which inputs are secret, the endpoint's
// stored inputs say which secret each references (by name).
func secretRefsOf(driverSpec, endpointInputs []byte) (map[string]string, error) {
	sp, err := driver.Parse(driverSpec)
	if err != nil {
		return nil, fmt.Errorf("stored driver spec does not parse: %w", err)
	}
	var inputs map[string]string
	if len(endpointInputs) > 0 {
		if err := json.Unmarshal(endpointInputs, &inputs); err != nil {
			return nil, fmt.Errorf("stored endpoint inputs do not parse: %w", err)
		}
	}
	refs := map[string]string{}
	for _, in := range sp.Inputs {
		if in.Kind != "secret" {
			continue
		}
		if ref, ok := inputs[in.Name]; ok && ref != "" {
			refs[in.Name] = ref
		}
	}
	return refs, nil
}
