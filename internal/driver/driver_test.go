package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kubernetes-csi/csi-test/v5/pkg/sanity"
)

func TestDriver(t *testing.T) {
	dir, _ := os.MkdirTemp("", "driver_test")
	defer os.RemoveAll(dir)

	ctx := context.Background()
	d := NewSyncedHostPathDriver("nodeID", filepath.Join(dir, "state"), true, "localhost:50051")
	go func() {
		d.Run(ctx, "/tmp/csi.sock")
	}()
	defer d.Stop()

	config := sanity.NewTestConfig()
	config.Address = "/tmp/csi.sock"
	config.TargetPath = filepath.Join(dir, "csi-mount")
	config.StagingPath = filepath.Join(dir, "csi-staging")

	sanity.Test(t, config)
}
