package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"net"
	"strconv"
	"strings"

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
// the stored inputs are the effective ones the derived tasks assume. Secret
// references are stored by name; the uuid-stable binding is #820.
func (p *PG) resolveAttach(ctx context.Context, q querier, es EndpointSpec) (*attachPlan, error) {
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

	effective, err := p.resolveInputs(ctx, q, d, sp, es.Inputs, es.SecretRead, es.CanAdmin)
	if err != nil {
		return nil, err
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

// resolveInputs checks supplied inputs against a driver's declared ones and
// returns the effective set (defaults baked): an undeclared input, a missing
// required one, a number input that is not a number, or a secret reference
// the caller cannot use (resolved under the caller's secret read scope and
// admin tier) refuses with ErrAttachInvalid naming the input. The attach and
// the in-place edit (#1061) share it.
func (p *PG) resolveInputs(ctx context.Context, q querier, d *Driver, sp *driver.Spec, supplied map[string]string, secretRead scope.Set, canAdmin bool) (map[string]string, error) {
	es := struct {
		Inputs     map[string]string
		SecretRead scope.Set
		CanAdmin   bool
	}{supplied, secretRead, canAdmin}
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
		case supplied && strings.TrimSpace(v) == "" && in.Required:
			return nil, fmt.Errorf("%w: required input %q is empty", ErrAttachInvalid, in.Name)
		case supplied && strings.TrimSpace(v) != "":
			effective[in.Name] = v
		case in.Default != "":
			effective[in.Name] = in.Default
		case in.Required:
			return nil, fmt.Errorf("%w: required input %q not supplied", ErrAttachInvalid, in.Name)
		}
	}
	for _, in := range sp.Inputs {
		if v, ok := effective[in.Name]; ok {
			if err := sp.CheckInputValue(in.Name, v); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrAttachInvalid, err)
			}
		}
		if in.Kind == "number" {
			if v, ok := effective[in.Name]; ok {
				if _, err := strconv.ParseFloat(v, 64); err != nil {
					return nil, fmt.Errorf("%w: input %q must be a number, and %q is not", ErrAttachInvalid, in.Name, v)
				}
			}
		}
		if in.Kind != "secret" {
			continue
		}
		ref, ok := effective[in.Name]
		if !ok {
			continue
		}
		// Resolve the secret reference under the CALLER'S secret read scope and
		// admin tier, never by bare name: an attach must not bind (and the
		// worklist must not later deliver) a secret the operator could not
		// themselves read. A candidate outside the read scope, or admin-sensitive
		// without the admin tier, is filtered out here, so an out-of-scope or
		// privileged name is indistinguishable from an absent one (no existence
		// or type oracle). Only a reference the caller can actually see reaches
		// the type check, where naming the wrong shape is a legitimate hint.
		rows, err := q.Query(ctx, `
			select s.owner_kind,
				coalesce(s.component_id::text, s.system_id::text, s.location_id::text),
				s.admin_sensitive, st.name
			from secret s join secret_type st on st.id = s.secret_type
			where s.name = $1`, ref)
		if err != nil {
			return nil, fmt.Errorf("storage: resolve secret reference %q: %w", ref, err)
		}
		var visibleTypes []string
		for rows.Next() {
			var ownerKind, typeName string
			var ownerID *string
			var adminSensitive bool
			if err := rows.Scan(&ownerKind, &ownerID, &adminSensitive, &typeName); err != nil {
				rows.Close()
				return nil, fmt.Errorf("storage: scan secret reference %q: %w", ref, err)
			}
			if adminSensitive && !es.CanAdmin {
				continue
			}
			inScope, err := p.secretOwnerInScope(ctx, q, ownerKind, ownerID, es.SecretRead)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("storage: scope secret reference %q: %w", ref, err)
			}
			if inScope {
				visibleTypes = append(visibleTypes, typeName)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("storage: resolve secret reference %q: %w", ref, err)
		}
		switch {
		case len(visibleTypes) == 0:
			return nil, fmt.Errorf("%w: input %q references a secret that does not exist or is not yours to use", ErrAttachInvalid, in.Name)
		case len(visibleTypes) > 1:
			// Refused rather than guessed: the reference is stored by name, so
			// a collision would make the worklist deliver an arbitrary in-scope
			// credential (#820 is the uuid-stable binding).
			return nil, fmt.Errorf("%w: input %q references secret %q, which names more than one secret you can use", ErrAttachInvalid, in.Name, ref)
		case visibleTypes[0] != in.SecretType:
			return nil, fmt.Errorf("%w: input %q needs a %s secret, and %q is a %s", ErrAttachInvalid, in.Name, in.SecretType, ref, visibleTypes[0])
		}
	}

	return effective, nil
}

// attachParams derives the transport params from the effective inputs by the
// engine's naming contract: an input named `host` is the address, an input
// named `port` joins it, and an input named `tls-mode` is the tls dialer's
// verification mode (#1035). Everything else stays in inputs, read by the
// interpreter, not the dialer.
func attachParams(effective map[string]string) map[string]string {
	params := map[string]string{}
	if mode, ok := effective[driver.TLSModeInput]; ok {
		params[driver.TLSModeInput] = mode
	}
	if port, ok := effective[driver.ListenPortInput]; ok && port != "" {
		params[driver.ListenPortInput] = port
	}
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
// the API tier maps to a 422 reference fault). The row is read FOR SHARE: a
// driver update or seed upsert in flight holds a lock that conflicts, so the
// attach waits for it and derives from the spec it commits, and one that
// starts after waits for the attach, whose endpoint its re-derive then sees
// (#823). Without it an attach racing an update kept the old spec for good.
func loadDriverRef(ctx context.Context, q querier, ref string) (*Driver, error) {
	d, err := scanDriver(q.QueryRow(ctx, `select `+driverCols+` from driver where `+registryRefCol(ref)+` = $1 for share`, ref))
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

// derivedTask is one task a driver spec derives on an endpoint: its content
// hash id, its mode, and its baked spec.
type derivedTask struct {
	id   string
	mode string
	spec []byte
}

// derivedDriverTasks computes the derived work of a driver on an endpoint: a
// poll task per poll function, a standing listen task per listener (command
// bindings derive nothing; they actuate on demand). Ids are the content hash
// every derived task uses, so the same spec always derives the same ids.
func derivedDriverTasks(endpointID string, d *Driver, sp *driver.Spec, cat catalogMaps) ([]derivedTask, error) {
	var out []derivedTask
	add := func(mode string, body any) error {
		spec, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("storage: marshal derived task: %w", err)
		}
		out = append(out, derivedTask{id: taskID(endpointID, mode, spec), mode: mode, spec: spec})
		return nil
	}
	for _, p := range sp.Polls {
		emits, err := bakeEmits(p.Emits, cat)
		if err != nil {
			return nil, err
		}
		sched, req := p.Schedule, p.Request
		if err := add("poll", driver.BakedFunction{
			Driver: d.Name, Version: d.Version, Function: p.Name,
			Schedule: &sched, Request: &req, Emits: emits,
		}); err != nil {
			return nil, err
		}
	}
	for _, l := range sp.Listeners {
		emits, err := bakeEmits(l.Emits, cat)
		if err != nil {
			return nil, err
		}
		match := l.Match
		if err := add("listen", driver.BakedFunction{
			Driver: d.Name, Version: d.Version, Function: l.Name,
			Arm: l.Arm, Match: &match, Emits: emits, Unmatched: sp.Unmatched,
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// insertDerivedTasks writes derived tasks, skipping any already present (an
// unchanged function keeps its row), and reports how many it added.
func insertDerivedTasks(ctx context.Context, tx pgx.Tx, endpointID string, tasks []derivedTask) (int64, error) {
	var added int64
	for _, t := range tasks {
		tag, err := tx.Exec(ctx, `
			insert into task (id, mode, endpoint_id, spec, enabled)
			values ($1, $2, $3, $4, true)
			on conflict (id) do nothing`, t.id, t.mode, endpointID, t.spec)
		if err != nil {
			return 0, fmt.Errorf("storage: derive %s task for endpoint %q: %w", t.mode, endpointID, err)
		}
		added += tag.RowsAffected()
	}
	return added, nil
}

// deriveDriverTasks writes the derived work of an attach inside the create
// transaction.
func deriveDriverTasks(ctx context.Context, tx pgx.Tx, endpointID string, plan *attachPlan) error {
	tasks, err := derivedDriverTasks(endpointID, plan.driver, plan.spec, plan.catalog)
	if err != nil {
		return err
	}
	_, err = insertDerivedTasks(ctx, tx, endpointID, tasks)
	return err
}

// rederiveDriverTasks brings every endpoint attached through a driver to the
// driver's spec as it stands now (#823), inside the caller's transaction (a
// driver update, or the boot seed's upsert): the tasks the spec derives are
// written where missing, and the driver tasks it no longer derives are
// retired. Ids are content hashes, so an unchanged function keeps its row and
// a spec that changed nothing touches nothing; an endpoint whose tasks did
// change has its updated_at bumped, which moves the node's config generation.
func rederiveDriverTasks(ctx context.Context, tx pgx.Tx, driverID string) error {
	d, err := scanDriver(tx.QueryRow(ctx, `select `+driverCols+` from driver where id = $1`, driverID))
	if err != nil {
		return fmt.Errorf("storage: load driver %q to re-derive: %w", driverID, err)
	}
	if len(d.Spec) == 0 {
		return nil // a stub has nothing attached and derives nothing
	}
	sp, err := driver.Parse(d.Spec)
	if err != nil {
		return fmt.Errorf("storage: stored spec for driver %q does not parse: %w", d.Name, err)
	}
	cat, err := loadDriverCatalog(ctx, tx)
	if err != nil {
		return err
	}
	// FOR KEY SHARE, in id order: an endpoint a concurrent delete holds is
	// waited on and, once deleted, skipped, rather than failing the task
	// insert on its foreign key; one a delete has not reached cannot vanish
	// mid re-derive.
	rows, err := tx.Query(ctx, `select id::text from endpoint where driver_id = $1 order by id for key share`, driverID)
	if err != nil {
		return fmt.Errorf("storage: endpoints of driver %q: %w", d.Name, err)
	}
	var endpoints []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("storage: scan endpoint of driver %q: %w", d.Name, err)
		}
		endpoints = append(endpoints, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: endpoints of driver %q: %w", d.Name, err)
	}
	for _, ep := range endpoints {
		if err := rederiveEndpoint(ctx, tx, ep, d, sp, cat); err != nil {
			return err
		}
	}
	return nil
}

// rederiveEndpoint brings one endpoint's driver tasks to what the spec
// derives: new functions inserted, superseded ones retired, an unchanged one
// kept (ids are content hashes over the spec, so an address edit moves none).
// The driver update's re-derive (#823) and the in-place edit (#1061) share it.
func rederiveEndpoint(ctx context.Context, tx pgx.Tx, ep string, d *Driver, sp *driver.Spec, cat catalogMaps) error {
	tasks, err := derivedDriverTasks(ep, d, sp, cat)
	if err != nil {
		return err
	}
	added, err := insertDerivedTasks(ctx, tx, ep, tasks)
	if err != nil {
		return err
	}
	keep := make([]string, 0, len(tasks))
	for _, t := range tasks {
		keep = append(keep, t.id)
	}
	// Only driver tasks retire: the reachability probe (a bare spec) is the
	// endpoint's own and stays.
	tag, err := tx.Exec(ctx, `
		delete from task
		where endpoint_id = $1 and spec->>'driver' is not null and not (id = any($2))`, ep, keep)
	if err != nil {
		return fmt.Errorf("storage: retire superseded tasks of endpoint %q: %w", ep, err)
	}
	if added+tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `update endpoint set updated_at = now() where id = $1`, ep); err != nil {
			return fmt.Errorf("storage: bump endpoint %q after re-derive: %w", ep, err)
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
