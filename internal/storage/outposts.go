package storage

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/fingerprint"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Outpost-layer sentinel errors. An outpost is the edge runtime; it is fleet-wide
// (not tree-scoped), so its create/enroll/read paths require an all scope,
// mirroring users.
var (
	ErrOutpostNotFound   = errors.New("storage: outpost not found")
	ErrOutpostExists     = errors.New("storage: outpost name already exists")
	ErrOutpostForbidden  = errors.New("storage: action not permitted on outposts")
	ErrEnrollmentInvalid = errors.New("storage: enrollment token invalid")
)

// Outpost is the edge runtime's server-side record: the detail row of its
// kind='outpost' user. UserID is that user's id. EnrolledAt is set
// the first time the outpost claims its identity; Enrolled is a convenience derived
// from it.
type Outpost struct {
	UserID          string
	Name            string
	Label           string
	Description     string
	LocationName    *string
	LocationID      *string
	LastHeartbeatAt *time.Time
	EnrolledAt      *time.Time
	Enrolled        bool
	// OrphanedBatches is how many of this outpost's telemetry batches the
	// confinement fence orphaned (a task on another outpost, or unknown), their
	// samples dropped; LastOrphanedAt the newest (#1200). Counted, not logged
	// per batch, so an outpost stuck on a moved task is visible without a flood.
	OrphanedBatches int64
	LastOrphanedAt  *time.Time
	// PlatformComponentID is the outpost's own component in the platform system
	// (#1625), which its self-monitoring metrics land on. nil only for an outpost
	// registered before the boot seed created the platform system; the next
	// boot adopts it.
	PlatformComponentID *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// OutpostSpec is the create input. Label is the operator label (empty falls
// back to the name on read); LocationName is an optional descriptive placement.
type OutpostSpec struct {
	Name         string
	Label        string
	Description  string
	LocationName *string
}

// OutpostPatch is the update input: a nil field is left unchanged. Name is not
// patchable (it is the immutable fleet address and enrollment identity). A
// LocationName pointing at "" clears the placement.
type OutpostPatch struct {
	Label        *string
	Description  *string
	LocationName *string
}

// WorklistTask is one enabled task resolved for an outpost: the content-addressed
// task plus the placement-bound endpoint it runs over. EndpointParams and Spec
// are raw jsonb passed through to the outpost. Secrets carries a driver task's
// unsealed secret inputs (input name to the secret's fields), resolved here so
// the outpost can present the credential to the device (#814); it is empty for
// every non-driver task.
type WorklistTask struct {
	ID   string
	Mode string
	// EndpointID is outpost-unique where EndpointName is unique only per
	// component; the outpost keys a held session by it (#816).
	EndpointID     string
	EndpointName   string
	Transport      string
	EndpointParams []byte
	Spec           []byte
	Secrets        map[string]map[string]string
	// Mask is the plaintext of each secret-marked field of Secrets, which the
	// outpost masks out of what the task collects before it publishes.
	Mask []string
	// Session is the driver's session block (#1049), read off the live
	// spec: the auth step, terminal replies and re-probe a held session
	// runs. Nil for a driver that declares none and for non-driver tasks.
	Session *driver.SessionSpec
	// SessionRetry counts the operator's :retry calls on the endpoint; a
	// rejected session on the outpost redials once when it moves. Nothing else
	// moves it, so an unrelated edit leaves a rejected session alone.
	SessionRetry int64
	// HostKey is an ssh endpoint's host key pin (#1032): the operator's
	// declared ssh-host-key, else the key the outpost trusted on first use (the
	// observed one), else empty.
	HostKey string
	// CertificatePin is a tls endpoint's public key pin (#1035): the
	// operator's declared tls-certificate, else the key the outpost trusted on
	// first use, else empty.
	CertificatePin string
	// Fingerprint is the connection fingerprint (#1056 ruling): a digest
	// over every input a connection to the endpoint is made with, computed
	// here so it covers the sealed secret versions without the outpost ever
	// hashing a plaintext.
	Fingerprint string
	// Rejection is the endpoint's last published endpoint-rejection (the
	// ladder rung, the class, and the fingerprint a terminal refusal gated),
	// so a restarted outpost honors a gate it did not see happen.
	Rejection string
	// Ladder is the endpoint's resolved reachability ladder (ADR-0158), for
	// every task of a driver-attached endpoint; nil for a bare endpoint,
	// whose probe is its transport's own.
	Ladder *driver.EffectiveLadder
	// ComponentID is the component the endpoint hangs off (#1652).
	ComponentID string
	// PathUpstreams is every path dependency of the endpoint's component
	// (#1652): the upstream and its in-band endpoints on the same outpost. None
	// for an out-of-band endpoint, and none for an upstream with no in-band
	// endpoint on this outpost (a cross-outpost path is #1655).
	PathUpstreams []PathUpstream
}

// PathUpstream is one path upstream a worklist task's endpoint is reached
// through (#1652).
type PathUpstream struct {
	ComponentID   string
	ComponentName string
	EndpointIDs   []string
}

// Worklist is an outpost's resolved work plus the config generation (the max
// endpoint updated_at across the outpost's endpoints, epoch seconds; 0 when the
// outpost has no endpoints). A steady generation lets the outpost serve from cache; a
// bump forces a refresh.
type Worklist struct {
	Tasks            []WorklistTask
	ConfigGeneration int64
	// ReportSeconds is the platform's collection report window (#1369,
	// collection.report_seconds), read on every pull so an admin's change
	// reaches each outpost on its next one.
	ReportSeconds int
}

// The placement arc stores location.id, so the name is read back beside it via a
// scalar subquery: the id is what the row points at, the name is what an operator
// reads and types. The subquery form works in a plain select and in a RETURNING
// list alike, so the insert and update paths need no join.
const outpostCols = `user_id, name, coalesce(label, ''), description,
	(select l.name from location l where l.id = outpost.location_id) as location_name, location_id,
	last_heartbeat_at, enrolled_at, orphaned_batches, last_orphaned_at, platform_component_id, created_at, updated_at`

func scanOutpost(row pgx.Row) (*Outpost, error) {
	var n Outpost
	if err := row.Scan(&n.UserID, &n.Name, &n.Label, &n.Description, &n.LocationName, &n.LocationID, &n.LastHeartbeatAt, &n.EnrolledAt, &n.OrphanedBatches, &n.LastOrphanedAt, &n.PlatformComponentID, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	n.Enrolled = n.EnrolledAt != nil
	return &n, nil
}

// CreateOutpost inserts an outpost as a kind='outpost' user plus its detail row,
// writing the audit row in the same transaction (mirroring the person/machine
// create). An outpost is fleet-wide, so creation requires an all create scope (like
// a user, unlike a tree-scoped location/system/component).
func (p *PG) CreateOutpost(ctx context.Context, actorID string, spec OutpostSpec, create, locationRead scope.Set) (*Outpost, error) {
	if !create.All {
		return nil, ErrOutpostForbidden
	}
	if err := ValidateName("outpost", spec.Name); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create outpost: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	locationID, err := outpostLocationID(ctx, tx, spec.LocationName, locationRead)
	if err != nil {
		return nil, err
	}
	var pid string
	if err := tx.QueryRow(ctx, `insert into users (kind) values ('machine') returning id`).Scan(&pid); err != nil {
		return nil, fmt.Errorf("storage: create outpost user: %w", err)
	}
	n, err := scanOutpost(tx.QueryRow(ctx, `
		insert into outpost (user_id, name, label, description, location_id)
		values ($1, $2, $3, $4, $5)
		returning `+outpostCols, pid, spec.Name, labelOrNull(spec.Label), spec.Description, locationID))
	if err != nil {
		return nil, mapOutpostWriteErr(err)
	}
	// The outpost's own component in the platform system, in the same
	// transaction so no outpost exists without it (#1625). An unseeded database
	// has no platform system yet; the next boot's EnsurePlatform adopts the
	// outpost then.
	sysID, err := platformSystemID(ctx, tx)
	if err != nil {
		return nil, err
	}
	if sysID != nil {
		cid, err := p.createPlatformComponent(ctx, tx, actorID, n.Name, *sysID, false)
		if err != nil {
			return nil, err
		}
		n.PlatformComponentID = &cid
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "outpost", n.UserID, nil, n); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create outpost: %w", err)
	}
	return n, nil
}

// UpdateOutpost patches an outpost's label, description, and location (a nil
// field is left unchanged; a LocationName of "" clears the placement). name is
// not patched: it is the immutable fleet address and enrollment identity. A
// outpost is fleet-wide, so the update requires an all scope, like create. An
// unknown name is ErrOutpostNotFound; an unknown location is ErrLocationNotFound.
func (p *PG) UpdateOutpost(ctx context.Context, actorID, name string, patch OutpostPatch, read, action, locationRead scope.Set) (*Outpost, error) {
	if err := RejectAddressForm("outpost", name); err != nil {
		return nil, err
	}
	if !read.All || !action.All {
		return nil, ErrOutpostForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update outpost: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := scanOutpost(tx.QueryRow(ctx, `select `+outpostCols+` from outpost where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOutpostNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: read outpost for update %q: %w", name, err)
	}
	locationID, err := outpostLocationID(ctx, tx, patch.LocationName, locationRead)
	if err != nil {
		return nil, err
	}
	setLabel, labelVal := labelPatch(patch.Label)
	after, err := scanOutpost(tx.QueryRow(ctx, `
		update outpost set
			label  = case when $6::boolean then $2 else label end,
			description   = coalesce($3, description),
			location_id   = case when $4 then $5 else location_id end,
			updated_at    = now()
		where name = $1
		returning `+outpostCols,
		name, labelVal, patch.Description, patch.LocationName != nil, locationID, setLabel))
	if err != nil {
		return nil, mapOutpostWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "outpost", after.UserID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update outpost: %w", err)
	}
	return after, nil
}

// DeleteOutpost decommissions an outpost: a hard delete of its kind='outpost' user,
// which cascades the outpost detail row and, through it, everything keyed to the
// outpost, its interfaces and their derived tasks, its outpost-owned samples and tag
// bindings, and its enrollment credential (every referencing FK is ON DELETE
// CASCADE). An outpost is fleet-wide, so this requires an all scope, like create. An
// unknown name is ErrOutpostNotFound. Audited before the row is gone; the actor is
// the deleter (unaffected by the cascade) and audit_log.resource_id is plain
// text, not a foreign key, so the deleted outpost's user id survives there.
func (p *PG) DeleteOutpost(ctx context.Context, actorID, name string, read, action scope.Set) error {
	if err := RejectAddressForm("outpost", name); err != nil {
		return err
	}
	if !read.All || !action.All {
		return ErrOutpostForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete outpost: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pid string
	var componentID *string
	err = tx.QueryRow(ctx, `select user_id, platform_component_id from outpost where name = $1`, name).Scan(&pid, &componentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOutpostNotFound
	} else if err != nil {
		return fmt.Errorf("storage: delete outpost lookup %q: %w", name, err)
	}
	// The before image carries the name. Keying on the user id is right (a rename
	// must not orphan the row), but without the image the row is unresolvable: the
	// user is deleted in this same transaction and no API surface returns a
	// outpost's user_id, so "which outpost was deleted" would have no answer at all.
	before := map[string]any{"name": name, "user_id": pid}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "outpost", pid, before, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from users where id = $1`, pid); err != nil {
		return fmt.Errorf("storage: delete outpost user: %w", err)
	}
	// Its platform component goes with it (#1625), once the outpost row that
	// names it is gone.
	if componentID != nil {
		c, err := scanComponent(tx.QueryRow(ctx, `delete from component where id = $1 returning `+componentCols, *componentID))
		if err != nil {
			return fmt.Errorf("storage: delete outpost %q's platform component: %w", name, err)
		}
		if err := writeAuditRes(ctx, tx, actorID, "delete", "component", c.ID, c, nil); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete outpost: %w", err)
	}
	return nil
}

// SetEnrollmentToken installs the outpost's enrollment secret as a bearer
// credential ROW on its user (the same machinery a service bearer token
// uses), taking the hex sha256 of a freshly minted token (the cleartext is shown
// once by the API and never stored). Re-enrolling replaces any existing bearer
// credential, so the previous token stops working. Audited. Requires an all
// action scope.
func (p *PG) SetEnrollmentToken(ctx context.Context, actorID, name, tokenHashHex string, action scope.Set) (*Outpost, error) {
	if err := RejectAddressForm("outpost", name); err != nil {
		return nil, err
	}
	if !action.All {
		return nil, ErrOutpostForbidden
	}
	hash, err := hex.DecodeString(tokenHashHex)
	if err != nil {
		return nil, fmt.Errorf("storage: set enrollment token %q: bad hash: %w", name, err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set enrollment token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := scanOutpost(tx.QueryRow(ctx, `select `+outpostCols+` from outpost where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOutpostNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: load outpost %q: %w", name, err)
	}
	// Replace any prior bearer credential so a re-enroll invalidates the old
	// token, then install the new one. The secret is stored only as its hash; the
	// prefix is the outpost name, a non-secret locator for scanners and audit.
	if _, err := tx.Exec(ctx,
		`delete from credential where user_id = $1 and kind = 'bearer'`, before.UserID); err != nil {
		return nil, fmt.Errorf("storage: clear outpost credential %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		`insert into credential (user_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		before.UserID, hash, name); err != nil {
		return nil, fmt.Errorf("storage: set outpost credential %q: %w", name, err)
	}
	after, err := scanOutpost(tx.QueryRow(ctx, `
		update outpost set updated_at = now()
		where name = $1
		returning `+outpostCols, name))
	if err != nil {
		return nil, fmt.Errorf("storage: set enrollment token %q: %w", name, err)
	}
	// The token hash itself is never written to the audit diff (it is a secret);
	// the audit records that an enroll happened on the outpost.
	if err := writeAuditRes(ctx, tx, actorID, "enroll", "outpost", after.UserID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set enrollment token: %w", err)
	}
	return after, nil
}

// ClaimOutpost is the outpost-facing exchange: the outpost presents its enrollment token,
// and a bearer-credential match sets enrolled_at (first claim) and returns the
// outpost. No scope: the presented token is the authentication. A mismatch, an
// unenrolled outpost, or an unknown outpost is ErrEnrollmentInvalid (a claim must not
// disclose which outposts exist).
func (p *PG) ClaimOutpost(ctx context.Context, name, tokenHashHex string) (*Outpost, error) {
	pr, err := p.authenticateOutpostCredential(ctx, name, tokenHashHex)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, ErrEnrollmentInvalid
	}
	// coalesce keeps the original enrolled_at on a re-claim (idempotent). Keyed by
	// the resolved user id, so it stamps exactly the outpost that authenticated.
	n, err := scanOutpost(p.pool.QueryRow(ctx, `
		update outpost set enrolled_at = coalesce(enrolled_at, now()), updated_at = now()
		where user_id = $1
		returning `+outpostCols, pr.ID))
	if err != nil {
		return nil, fmt.Errorf("storage: mark enrolled %q: %w", name, err)
	}
	return n, nil
}

// AuthenticateOutpost reports whether the presented token hash matches the outpost's
// bearer credential. The NATS auth callback calls this to admit an outpost
// connection; a non-match, an unenrolled outpost, or an unknown outpost is a clean
// false, not an error.
func (p *PG) AuthenticateOutpost(ctx context.Context, name, tokenHashHex string) (bool, error) {
	pr, err := p.authenticateOutpostCredential(ctx, name, tokenHashHex)
	if err != nil {
		return false, err
	}
	return pr != nil, nil
}

// authenticateOutpostCredential resolves the presented token hash to a bearer
// credential via the shared AuthenticateBearer helper and confirms the owning
// user is the outpost of that name. It returns a nil user (no error) when
// the hash matches no credential, the credential belongs to a non-outpost user,
// or the outpost name does not match, so callers cannot use it to enumerate outposts.
func (p *PG) authenticateOutpostCredential(ctx context.Context, name, tokenHashHex string) (*User, error) {
	hash, err := hex.DecodeString(tokenHashHex)
	if err != nil {
		return nil, nil // a malformed hash matches nothing
	}
	pr, err := p.AuthenticateBearer(ctx, hash)
	switch {
	case errors.Is(err, ErrCredentialNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("storage: authenticate outpost %q: %w", name, err)
	}
	if pr.Outpost == nil || pr.Outpost.Name != name {
		return nil, nil
	}
	return pr, nil
}

// RecordHeartbeat stamps the outpost's last_heartbeat_at. Keyed by the outpost name the
// server extracts from the heartbeat subject (subject permissions guarantee a
// outpost can only publish to its own subject), so this trusts the name.
func (p *PG) RecordHeartbeat(ctx context.Context, name string) error {
	tag, err := p.pool.Exec(ctx, `update outpost set last_heartbeat_at = now() where name = $1`, name)
	if err != nil {
		return fmt.Errorf("storage: record heartbeat %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOutpostNotFound
	}
	return nil
}

// RecordOrphanedBatches adds batches to the outpost's orphaned-batch count and
// moves its last_orphaned_at to at, never backwards (#1200). Keyed by the outpost
// name the server took from the telemetry subject, which subject permissions
// guarantee is the publisher's own, so it trusts the name. The ingest
// consumer calls it once per outpost per flush, never once per batch.
func (p *PG) RecordOrphanedBatches(ctx context.Context, name string, batches int64, at time.Time) error {
	tag, err := p.pool.Exec(ctx, `update outpost
		set orphaned_batches = orphaned_batches + $2,
		    last_orphaned_at = greatest(last_orphaned_at, $3)
		where name = $1`, name, batches, at)
	if err != nil {
		return fmt.Errorf("storage: record orphaned batches %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOutpostNotFound
	}
	return nil
}

// pinKeyOf names the property a transport's key pin rides (declared and
// observed): the ssh host key (#1032), and the certificate's public key of
// every transport over the tls handshake (tls #1035, https #902, wss #1034),
// read off the catalog's flag rather than a second list.
var pinKeyOf = func() map[string]string {
	m := map[string]string{"ssh": probekeys.SSHHostKey}
	for _, t := range transport.All() {
		if t.TLS {
			m[t.Name] = probekeys.TLSCertificate
		}
	}
	return m
}()

// OutpostWorklist resolves an outpost's enabled tasks (joined to their endpoint) plus
// the config generation. Keyed by the outpost name the server extracts from the
// worklist subject. An unknown outpost returns an empty worklist, not an error.
func (p *PG) OutpostWorklist(ctx context.Context, name string) (Worklist, error) {
	rows, err := p.pool.Query(ctx, `
		select t.id, t.mode, i.id::text, i.name, i.transport, i.params, t.spec, i.inputs, i.component, d.spec,
			i.session_retry, coalesce(d.id::text, '')
		from task t
		join endpoint i on i.id = t.endpoint_id
		left join driver d on d.id = i.driver_id
		where i.outpost_id = (select user_id from outpost where name = $1) and t.enabled = true
		order by t.id`, name)
	if err != nil {
		return Worklist{}, fmt.Errorf("storage: outpost worklist %q: %w", name, err)
	}
	defer rows.Close()
	var wl Worklist
	type secretRef struct{ input, ref, componentID string }
	// per task: the driver inputs and the secret versions the fingerprint
	// hashes, and the component the endpoint's properties hang off.
	epInputsOf := map[int][]byte{}
	versions := map[string]map[string]string{} // endpoint id -> secret input -> sealed version
	sessionOf := map[string]*driver.SessionSpec{}
	componentOf := map[int]string{}
	pending := map[int][]secretRef{} // task index -> the secret inputs to unseal
	type pinRef struct {
		idx                   int
		componentID, instance string
		key                   string // the pin's property: ssh-host-key or tls-certificate
	}
	var pins []pinRef // ssh and tls tasks, whose key pin rides the worklist
	// A driver's spec parses once per pull, however many endpoints run it: a
	// site's endpoints share a handful of drivers (#1571).
	type parsedSpec struct {
		spec *driver.Spec
		err  error
	}
	parsed := map[string]parsedSpec{} // driver id -> its parsed spec
	for rows.Next() {
		var wt WorklistTask
		var epInputs, driverSpec []byte
		var componentID *string
		var driverID string
		if err := rows.Scan(&wt.ID, &wt.Mode, &wt.EndpointID, &wt.EndpointName, &wt.Transport, &wt.EndpointParams, &wt.Spec, &epInputs, &componentID, &driverSpec, &wt.SessionRetry, &driverID); err != nil {
			return Worklist{}, fmt.Errorf("storage: scan worklist task: %w", err)
		}
		// A driver task carries its driver's session block (#1049), read off
		// the live spec so a re-authored login reaches the outpost at its next
		// pull. A stored spec is validated at write; corruption delivers none.
		var ps parsedSpec
		if len(driverSpec) > 0 {
			var seen bool
			if ps, seen = parsed[driverID]; !seen {
				ps.spec, ps.err = driver.Parse(driverSpec)
				parsed[driverID] = ps
			}
			if sp, err := ps.spec, ps.err; err == nil {
				if len(wt.Spec) > 2 {
					wt.Session = sp.Session
				}
				l := driver.ResolveLadder(wt.Transport, sp.Ladder, sp.Session)
				wt.Ladder = &l
				sessionOf[wt.EndpointID] = sp.Session
			}
		}
		// A driver task's secret inputs travel with it: the spec says which
		// inputs are secret, the endpoint's inputs say which secret row each
		// references, and the unseal below turns the references into fields.
		// The reachability probe (a bare spec) carries only the credential its
		// rung sends: snmp's get carries the community (#821); every other
		// probe carries none.
		probe := len(wt.Spec) <= 2
		if len(driverSpec) > 0 && (!probe || wt.Transport == "snmp") {
			var refs map[string]string
			err := ps.err
			if err != nil {
				err = fmt.Errorf("stored driver spec does not parse: %w", err)
			} else {
				refs, err = secretRefsFrom(ps.spec, epInputs)
			}
			if err != nil {
				return Worklist{}, fmt.Errorf("storage: outpost worklist %q task %s: %w", name, wt.ID, err)
			}
			if probe {
				refs = rungSecretRefs(refs)
			}
			cid := ""
			if componentID != nil {
				cid = *componentID
			}
			for input, ref := range refs {
				pending[len(wl.Tasks)] = append(pending[len(wl.Tasks)], secretRef{input, ref, cid})
			}
		}
		epInputsOf[len(wl.Tasks)] = epInputs
		if componentID != nil {
			componentOf[len(wl.Tasks)] = *componentID
			wt.ComponentID = *componentID
		}
		if pinKey, pinned := pinKeyOf[wt.Transport]; pinned && componentID != nil {
			pins = append(pins, pinRef{idx: len(wl.Tasks), componentID: *componentID, instance: wt.EndpointName, key: pinKey})
		}
		wl.Tasks = append(wl.Tasks, wt)
	}
	if err := rows.Err(); err != nil {
		return Worklist{}, fmt.Errorf("storage: outpost worklist %q: %w", name, err)
	}
	// A driver spec stored, or a task derived, before the emit refusal may
	// still name a key the outpost lane may not carry: the outpost never sees it
	// (ADR-0161), whatever binary it runs.
	if err := p.stripRefusedEmits(ctx, name, wl.Tasks); err != nil {
		return Worklist{}, fmt.Errorf("storage: outpost worklist %q: %w", name, err)
	}
	// Resolve every (reference, component) pair in one statement after the
	// row scan (one connection at a time, #1581), each pair once however many
	// tasks share it. Delivery is confined to a secret that CASCADES ONTO the
	// endpoint's own component (outpostSecrets). A reference that resolves to
	// nothing deliverable is delivered as absence: the outpost lands a
	// collection-failed naming the missing credential.
	var secretRefs []outpostSecretRef
	secretAt := map[outpostSecretRef]int{}
	for _, refs := range pending {
		for _, r := range refs {
			k := outpostSecretRef{r.ref, r.componentID}
			if _, seen := secretAt[k]; !seen {
				secretAt[k] = len(secretRefs)
				secretRefs = append(secretRefs, k)
			}
		}
	}
	// A REAL failure (no key provider, a DB error, an envelope that will not
	// open) is NOT absence: swallowing it would mask infra failure as a missing
	// secret and send operators chasing a credential that exists. Fail the pull
	// instead; the outpost keeps its last-good worklist and retries next heartbeat.
	resolved, err := p.outpostSecrets(ctx, secretRefs)
	if err != nil {
		return Worklist{}, fmt.Errorf("storage: outpost worklist %q unseal: %w", name, err)
	}
	for idx, refs := range pending {
		for _, r := range refs {
			// Absence is deliverable: a reference that resolves to nothing this
			// component owns, or to more than one candidate, rides as an absent
			// credential and the outpost lands a collection-failed naming it.
			got := resolved[secretAt[outpostSecretRef{r.ref, r.componentID}]]
			if got.err != nil {
				continue
			}
			ep := wl.Tasks[idx].EndpointID
			if versions[ep] == nil {
				versions[ep] = map[string]string{}
			}
			versions[ep][r.input] = got.version
			if wl.Tasks[idx].Secrets == nil {
				wl.Tasks[idx].Secrets = map[string]map[string]string{}
			}
			wl.Tasks[idx].Secrets[r.input] = got.fields
			for _, f := range got.shape.Fields {
				if v := got.fields[f.Name]; f.Secret && v != "" {
					wl.Tasks[idx].Mask = append(wl.Tasks[idx].Mask, v)
				}
			}
		}
	}
	// An ssh task's host key pin (#1032), and a tls task's public key pin
	// (#1035): the operator's declared key wins (a :retry accepting a
	// replaced component's key; a declared row any other path left is never
	// the pin, ADR-1100), else the key the outpost trusted on first use.
	// The declared pin alone is a fingerprint input: it is the operator's
	// act. A key the outpost trusted on first use is a learned fact, and moving
	// the fingerprint on it would recycle a healthy session and lift a gate
	// held under the first fingerprint.
	declaredPin, declaredCert := map[int]string{}, map[int]string{}
	pinQs := make([]pinQuery, len(pins))
	for i, pr := range pins {
		pinQs[i] = pinQuery{pr.componentID, pr.instance, pr.key}
	}
	pinned, err := keyPins(ctx, p.pool, pinQs)
	if err != nil {
		return Worklist{}, fmt.Errorf("storage: outpost worklist %q: %w", name, err)
	}
	for i, pr := range pins {
		declared, pin := pinned[i].declared, pinned[i].pin
		switch pr.key {
		case pinKeyOf["tls"]:
			wl.Tasks[pr.idx].CertificatePin, declaredCert[pr.idx] = pin, declared
		default:
			wl.Tasks[pr.idx].HostKey, declaredPin[pr.idx] = pin, declared
		}
	}
	// The rejection an outpost published for the endpoint (its gate), one read
	// for every distinct endpoint, and the connection fingerprint each task is
	// judged against (#1056 ruling).
	var refs []endpointRef
	refAt := map[endpointRef]int{}
	for idx := range wl.Tasks {
		if cid, ok := componentOf[idx]; ok {
			r := endpointRef{cid, wl.Tasks[idx].EndpointName}
			if _, seen := refAt[r]; !seen {
				refAt[r] = len(refs)
				refs = append(refs, r)
			}
		}
	}
	rejections, err := latestRejections(ctx, p.pool, refs)
	if err != nil {
		return Worklist{}, fmt.Errorf("storage: outpost worklist %q: %w", name, err)
	}
	for idx := range wl.Tasks {
		t := &wl.Tasks[idx]
		if cid, ok := componentOf[idx]; ok {
			t.Rejection = rejections[refAt[endpointRef{cid, t.EndpointName}]]
		}
	}
	// One fingerprint per endpoint, every task of it the same: the endpoint's
	// secret versions and its driver's session block, whether or not this
	// task carries them (a probe task carries neither).
	fingerprintEndpoints(wl.Tasks, func(idx int) fingerprint.Inputs {
		t := wl.Tasks[idx]
		return fingerprint.Inputs{
			Transport:      t.Transport,
			Params:         t.EndpointParams,
			DriverInputs:   epInputsOf[idx],
			SecretVersions: versions[t.EndpointID],
			Session:        sessionOf[t.EndpointID],
			Ladder:         t.Ladder,
			HostKeyPin:     declaredPin[idx],
			CertificatePin: declaredCert[idx],
			Retry:          t.SessionRetry,
		}
	}, fingerprint.Of)
	upstreams, err := pathUpstreams(ctx, p.pool, name)
	if err != nil {
		return Worklist{}, fmt.Errorf("storage: outpost worklist %q: %w", name, err)
	}
	for idx := range wl.Tasks {
		wl.Tasks[idx].PathUpstreams = upstreams[wl.Tasks[idx].EndpointID]
	}
	// config_generation moves at operator-config pace: the max endpoint
	// updated_at (epoch seconds) across the outpost's endpoints, 0 when none.
	if err := p.pool.QueryRow(ctx, `
		select coalesce(extract(epoch from max(updated_at))::bigint, 0)
		from endpoint where outpost_id = (select user_id from outpost where name = $1)`, name).Scan(&wl.ConfigGeneration); err != nil {
		return Worklist{}, fmt.Errorf("storage: outpost config generation %q: %w", name, err)
	}
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return Worklist{}, fmt.Errorf("storage: outpost worklist %q: %w", name, err)
	}
	wl.ReportSeconds = s.Collection.ReportSeconds
	return wl, nil
}

// pathUpstreams is every in-band endpoint's path upstreams on the named outpost
// (#1652), in one statement whatever the outpost carries: for each path edge
// from the endpoint's component, the upstream and its in-band endpoints
// placed on the same outpost. An out-of-band endpoint carries none, and an
// upstream with no in-band endpoint on the outpost is left out.
func pathUpstreams(ctx context.Context, q querier, outpostName string) (map[string][]PathUpstream, error) {
	rows, err := q.Query(ctx, `
		select i.id::text, d.upstream_id::text, c.name, array_agg(u.id::text order by u.id)
		from endpoint i
		join component_dependency d on d.component_id = i.component and d.kind = 'path'
		join component c on c.id = d.upstream_id
		join endpoint u on u.component = d.upstream_id and u.outpost_id = i.outpost_id and not u.out_of_band
		where i.outpost_id = (select user_id from outpost where name = $1) and not i.out_of_band
		group by i.id, d.upstream_id, c.name
		order by i.id, d.upstream_id`, outpostName)
	if err != nil {
		return nil, fmt.Errorf("path upstreams: %w", err)
	}
	defer rows.Close()
	out := map[string][]PathUpstream{}
	for rows.Next() {
		var ep string
		var pu PathUpstream
		if err := rows.Scan(&ep, &pu.ComponentID, &pu.ComponentName, &pu.EndpointIDs); err != nil {
			return nil, fmt.Errorf("scan path upstream: %w", err)
		}
		out[ep] = append(out[ep], pu)
	}
	return out, rows.Err()
}

// GetOutpost reads one outpost by name. Requires an all read scope (an outpost is
// fleet-wide reference, not a subtree row); an unknown name is ErrOutpostNotFound.
func (p *PG) GetOutpost(ctx context.Context, name string, read scope.Set) (*Outpost, error) {
	if err := RejectAddressForm("outpost", name); err != nil {
		return nil, err
	}
	if !read.All {
		return nil, ErrOutpostForbidden
	}
	n, err := scanOutpost(p.pool.QueryRow(ctx, `select `+outpostCols+` from outpost where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOutpostNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: get outpost %q: %w", name, err)
	}
	return n, nil
}

// ListOutposts returns every outpost. Requires an all read scope.
func (p *PG) ListOutposts(ctx context.Context, read scope.Set) ([]Outpost, error) {
	if !read.All {
		return nil, ErrOutpostForbidden
	}
	rows, err := p.pool.Query(ctx, `select `+outpostCols+` from outpost order by name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list outposts: %w", err)
	}
	defer rows.Close()
	var out []Outpost
	for rows.Next() {
		n, err := scanOutpost(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan outpost: %w", err)
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func mapOutpostWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrOutpostExists
		case "23503": // foreign_key_violation (a placement resolved to a location that vanished mid-write)
			return ErrLocationNotFound
		}
	}
	return fmt.Errorf("storage: outpost write: %w", err)
}

// outpostLocationID resolves an optional placement reference (a location name or a
// uuid) to the location id the arc stores, within the caller's location:read
// scope. A nil reference leaves the placement unchanged and an empty one clears
// it, so both map to a nil id; an unknown location is ErrLocationNotFound rather
// than a NULL that would silently unplace the outpost.
//
// resolvePlacementRef is the seam #700 built for exactly this reference on the
// component and the system tiers, and this is the one caller-supplied reference
// that arc left resolving scope-blind (#705). Two of its three reasons apply
// here verbatim. The caller's own outpost create/update scope is the WRONG SET, not
// merely a stricter one: it is resolved for "outpost", and a location's scope tree
// is its own unrelated ancestor chain, so checking it against the location table
// could never match. And out of scope answers the same non-disclosing
// ErrLocationNotFound an absent location gives, because a refusal that separated
// them would confirm to a caller with no grant on that row that the row exists.
//
// What does NOT apply is the disclosure that made #700 urgent: no outpost label
// rule exists, so this write stamps nothing built from the location's own label
// and hands nothing back about it. What is left is the plain invariant, that
// ABAC scope is injected on every applicable query, and a caller-supplied
// reference resolved without it is an exception to that rather than a carve-out
// anybody decided on.
//
// The signature reconciles with the seam's by taking the read set the seam needs
// and nothing else: outpostLocationID already took a querier (the caller's tx, so a
// location written earlier in the same transaction resolves), which is exactly
// what resolvePlacementRef takes. The set is what the outpost path could not
// supply, so CreateOutpost and UpdateOutpost take it from the API layer, which injects
// location:read exactly as the system and component routes already do.
func outpostLocationID(ctx context.Context, q querier, ref *string, locationRead scope.Set) (*string, error) {
	if ref == nil || *ref == "" {
		return nil, nil
	}
	l, err := resolvePlacementRef(ctx, q, locationConfig, *ref, locationRead)
	if err != nil {
		return nil, err
	}
	return &l.ID, nil
}

// rungSecretRefs narrows an endpoint's secret references to the credential
// its reachability rung sends: an snmp get's community, the input named
// community (the engine's credential contract the poll reads too, #821).
func rungSecretRefs(refs map[string]string) map[string]string {
	out := map[string]string{}
	if ref, ok := refs[driver.SNMPCommunityInput]; ok {
		out[driver.SNMPCommunityInput] = ref
	}
	return out
}

// fingerprintEndpoints sets each task's connection fingerprint, computed once
// per endpoint from its first task's inputs (#1583): every input is the
// endpoint's, so its tasks share one, and a headquarters outpost runs several
// tasks on each of its endpoints.
func fingerprintEndpoints(tasks []WorklistTask, inputsOf func(idx int) fingerprint.Inputs, of func(fingerprint.Inputs) string) {
	byEndpoint := map[string]string{}
	for idx := range tasks {
		fp, seen := byEndpoint[tasks[idx].EndpointID]
		if !seen {
			fp = of(inputsOf(idx))
			byEndpoint[tasks[idx].EndpointID] = fp
		}
		tasks[idx].Fingerprint = fp
	}
}
