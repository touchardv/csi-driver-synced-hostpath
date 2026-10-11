package driver

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
)

func TestNodeUnstageVolume_IdempotencyNotFound(t *testing.T) {
	dir, err := os.MkdirTemp("", "nodeserver_test")
	assert.Nil(t, err)
	defer os.RemoveAll(dir)

	d := NewSyncedHostPathDriver("nodeID", dir, false, "localhost:50051")

	req := &csi.NodeUnstageVolumeRequest{
		VolumeId:          "non-existent-volume",
		StagingTargetPath: filepath.Join(dir, "staging"),
	}

	resp, err := d.NodeUnstageVolume(context.Background(), req)
	assert.Nil(t, err, "NodeUnstageVolume must succeed for non-existent volume (idempotency)")
	assert.NotNil(t, resp)
}

func TestNodeUnstageVolume_IdempotencyNotStaged(t *testing.T) {
	dir, err := os.MkdirTemp("", "nodeserver_test")
	assert.Nil(t, err)
	defer os.RemoveAll(dir)

	d := NewSyncedHostPathDriver("nodeID", dir, false, "localhost:50051")
	volID, err := d.svc.CreateVolume("test-vol", 1024)
	assert.Nil(t, err)

	req := &csi.NodeUnstageVolumeRequest{
		VolumeId:          volID,
		StagingTargetPath: filepath.Join(dir, "staging"),
	}

	resp, err := d.NodeUnstageVolume(context.Background(), req)
	assert.Nil(t, err, "NodeUnstageVolume must succeed when volume is not staged")
	assert.NotNil(t, resp)
}

func TestNodeUnstageVolume_ConcurrentDeduplication(t *testing.T) {
	dir, err := os.MkdirTemp("", "nodeserver_test")
	assert.Nil(t, err)
	defer os.RemoveAll(dir)

	d := NewSyncedHostPathDriver("nodeID", dir, false, "localhost:50051")
	volID := "test-race-volume"

	// Lock the volume to simulate an in-flight unstage operation
	d.volumeLocks.LockKey(volID)

	var wg sync.WaitGroup
	var callResult *csi.NodeUnstageVolumeResponse
	var callErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		// This concurrent call should block until the lock is released
		req := &csi.NodeUnstageVolumeRequest{
			VolumeId:          volID,
			StagingTargetPath: filepath.Join(dir, "staging"),
		}
		callResult, callErr = d.NodeUnstageVolume(context.Background(), req)
	}()

	// Give the goroutine time to attempt acquiring the lock and block
	time.Sleep(50 * time.Millisecond)

	// Release the lock (simulating completion of the in-flight unstage)
	_ = d.volumeLocks.UnlockKey(volID)

	wg.Wait()
	assert.Nil(t, callErr, "Concurrent NodeUnstageVolume should succeed idempotently")
	assert.NotNil(t, callResult)
}
