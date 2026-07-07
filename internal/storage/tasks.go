package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// TaskOwner is a task's resolved ingest binding: the component its interface
// dedicates its datapoints to, plus the interface identity the write records as
// source (type) and instance (name).
type TaskOwner struct {
	Component     string
	InterfaceName string
	InterfaceType string
}

// ResolveTaskOwner binds the component a node's task collects for and, in the
// same query, confines the node to its own tasks. Given a task id and the node
// that published the telemetry (extracted from the NATS subject), it returns the
// task's interface component. ok is false (the datapoint is an orphan the ingest
// consumer drops, never writes) when the task is unknown, belongs to a DIFFERENT
// node (the confinement fence: a node cannot land a datapoint for a component it
// was not placed on), or its interface has no component (a shared interface has
// no pre-bound owner in this checkpoint). err is reserved for a real DB failure,
// so the caller can leave the message unacked for redelivery.
func (p *PG) ResolveTaskOwner(ctx context.Context, taskID, nodeName string) (TaskOwner, bool, error) {
	var (
		owner     TaskOwner
		component *string
		taskNode  *string
	)
	err := p.pool.QueryRow(ctx, `
		select i.component, t.node_name, t.interface_name, i.type
		from task t
		join interface i on i.name = t.interface_name
		where t.id = $1`, taskID).Scan(&component, &taskNode, &owner.InterfaceName, &owner.InterfaceType)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskOwner{}, false, nil
	} else if err != nil {
		return TaskOwner{}, false, fmt.Errorf("storage: resolve task owner %q: %w", taskID, err)
	}
	if taskNode == nil || *taskNode != nodeName {
		return TaskOwner{}, false, nil // confinement: not this node's task
	}
	if component == nil || *component == "" {
		return TaskOwner{}, false, nil // shared interface: no pre-bound owner
	}
	owner.Component = *component
	return owner, true, nil
}
