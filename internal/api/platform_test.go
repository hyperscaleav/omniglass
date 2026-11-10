package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Every route that can reach a platform refusal answers it as a conflict,
// not the 500 an unmapped sentinel falls through to (#1625).
func TestPlatformRefusalsAnswerConflictOnEveryRoute(t *testing.T) {
	t.Parallel()
	mappers := map[string]func(error) error{
		"system": mapSystemErr, "component": mapComponentErr, "member": mapMemberErr,
		"role": mapRoleErr, "node": mapNodeErr,
	}
	sentinels := []error{
		storage.ErrPlatformSystemReserved, storage.ErrPlatformTypeReserved,
		storage.ErrPlatformComponentReserved, storage.ErrPlatformMemberReserved,
		storage.ErrPlatformComponentNameTaken,
	}
	for name, mapErr := range mappers {
		for _, sentinel := range sentinels {
			var se huma.StatusError
			got := mapErr(fmt.Errorf("wrapped: %w", sentinel))
			if !errors.As(got, &se) || se.GetStatus() != http.StatusConflict {
				t.Errorf("%s mapper on %v = %v, want 409", name, sentinel, got)
			}
		}
	}
}
