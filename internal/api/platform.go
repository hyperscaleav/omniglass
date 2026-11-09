package api

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// mapPlatformErr answers the Gateway's platform refusals (#1625) as 409s,
// whichever resource's route reached them: the platform system and a node's
// platform component are Omniglass's own rows, which an operator may relabel
// and may not reshape.
func mapPlatformErr(err error) (error, bool) {
	switch {
	case errors.Is(err, storage.ErrPlatformSystemReserved):
		return huma.Error409Conflict("the platform system is reserved to Omniglass: it may be relabeled, not renamed, retyped, moved, deleted or given a child system"), true
	case errors.Is(err, storage.ErrPlatformTypeReserved):
		return huma.Error409Conflict("the platform system type is reserved to the platform system"), true
	case errors.Is(err, storage.ErrPlatformComponentReserved):
		return huma.Error409Conflict("a node's platform component is reserved to its node: it may be relabeled, not renamed, reclassified, moved, deleted, given a child or added to another system"), true
	case errors.Is(err, storage.ErrPlatformMemberReserved):
		return huma.Error409Conflict("the platform system holds only node platform components"), true
	case errors.Is(err, storage.ErrPlatformComponentNameTaken):
		return huma.Error409Conflict("an unplaced component already holds this node's name, which the node's platform component takes"), true
	}
	return nil, false
}
