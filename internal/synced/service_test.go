package synced

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewService(t *testing.T) {
	stateDir, _ := os.MkdirTemp("", "service_test")
	defer os.RemoveAll(stateDir)

	svc := NewService(stateDir, "localhost:50051")
	err := svc.Stop()
	assert.Nil(t, err)
	assert.FileExists(t, filepath.Join(stateDir, "volumes.yaml"))

	svc = NewService(stateDir, "localhost:50051")
	err = svc.Stop()
	assert.Nil(t, err)
	assert.FileExists(t, filepath.Join(stateDir, "volumes.yaml"))
}

func TestCreateDeleteVolume(t *testing.T) {
	stateDir, _ := os.MkdirTemp("", "service_test")
	defer os.RemoveAll(stateDir)
	svc := NewService(stateDir, "localhost:50051")

	id, err := svc.CreateVolume("myvolume", 12340)
	assert.NotNil(t, id)
	assert.Nil(t, err)

	err = svc.DeleteVolume(id)
	assert.Nil(t, err)
}

func TestGetVolume(t *testing.T) {
	stateDir, _ := os.MkdirTemp("", "service_test")
	defer os.RemoveAll(stateDir)
	svc := NewService(stateDir, "localhost:50051")
	id, err := svc.CreateVolume("myvolume", 12340)
	assert.Nil(t, err)

	v, found := svc.GetVolumeByID(id)
	assert.True(t, found)
	assert.Equal(t, int64(12340), v.Capacity)
	assert.Equal(t, id, v.ID)
	assert.Equal(t, "myvolume", v.Name)

	v, found = svc.GetVolumeByID("not-found")
	assert.False(t, found)
	assert.Equal(t, int64(0), v.Capacity)
	assert.Equal(t, "", v.ID)
	assert.Equal(t, "", v.Name)

	v, found = svc.GetVolumeByName("myvolume")
	assert.True(t, found)
	assert.Equal(t, int64(12340), v.Capacity)
	assert.Equal(t, id, v.ID)
	assert.Equal(t, "myvolume", v.Name)

	v, found = svc.GetVolumeByName("wrong-name")
	assert.False(t, found)
	assert.Equal(t, int64(0), v.Capacity)
	assert.Equal(t, "", v.ID)
	assert.Equal(t, "", v.Name)
}

func TestResolve(t *testing.T) {
	stateDir, _ := os.MkdirTemp("", "service_test")
	defer os.RemoveAll(stateDir)
	svc := NewService(stateDir, "localhost:50051")
	id, err := svc.CreateVolume("myvolume", 12340)
	assert.Nil(t, err)

	path, err := svc.Resolve("wrong-ig")
	assert.NotNil(t, err)
	assert.Equal(t, "", path)

	path0, err := svc.Resolve(id)
	assert.Nil(t, err)
	path1, err := svc.Resolve(id)
	assert.Nil(t, err)
	assert.Equal(t, path0, path1)
}

func TestStageUnstage(t *testing.T) {
	stateDir, _ := os.MkdirTemp("", "service_test")
	defer os.RemoveAll(stateDir)

	svc := NewService(stateDir, "localhost:50051")
	id, err := svc.CreateVolume("myvolume", 12340)
	assert.Nil(t, err)

	fileServer := NewFileServer(stateDir)
	fileServer.Run(svc)
	defer fileServer.Stop()

	ctx := context.Background()
	stagingPath := filepath.Join(stateDir, "staging")
	os.Mkdir(stagingPath, 0755)
	err = svc.StageVolume(ctx, id, stagingPath)
	assert.Nil(t, err)

	err = svc.UnstageVolume(id, stagingPath)
	assert.Nil(t, err)
}

func TestSave(t *testing.T) {
	stateDir, _ := os.MkdirTemp("", "service_test")
	defer os.RemoveAll(stateDir)
	svc := NewService(stateDir, "localhost:50051")

	// Test 1: Save when volume is not found
	err := svc.Save("non-existent", "some-file")
	assert.Nil(t, err)

	// Test 2: Save when volume is found
	id, err := svc.CreateVolume("myvolume", 12340)
	assert.Nil(t, err)

	// Create a dummy file to copy
	dummyFile := filepath.Join(stateDir, "dummy")
	err = os.WriteFile(dummyFile, []byte("hello"), 0644)
	assert.Nil(t, err)

	err = svc.Save(id, dummyFile)
	assert.Nil(t, err)

	// Verify that the file was copied to the state archive
	archivePath := filepath.Join(stateDir, id, "archive.tar")
	assert.FileExists(t, archivePath)
	content, err := os.ReadFile(archivePath)
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello"), content)
}
