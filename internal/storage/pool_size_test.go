package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The gateway reports its pool's size as the DSN set it, the cap the bus holds
// its worklist workers under (#1577).
func TestTheGatewayReportsItsPoolSize(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t)+"&pool_max_conns=6")
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if got := gw.PoolSize(); got != 6 {
		t.Fatalf("PoolSize() = %d, want 6", got)
	}
}
