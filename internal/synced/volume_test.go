package synced

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateVolume(t *testing.T) {
	dir, _ := os.MkdirTemp("", "volume_test")
	defer os.RemoveAll(dir)

	v, err := createVolume("foo", 1024, dir)
	assert.Nil(t, err)
	assert.NotNil(t, v)
	assert.FileExists(t, filepath.Join(dir, v.ID, "archive.tar"))
}

func TestNewStagedVolume(t *testing.T) {
	v := newStagedVolume("some-id", "/staged/path")
	assert.NotNil(t, v)
	assert.Equal(t, "some-id", v.ID)
	assert.True(t, v.Staged)
	assert.Equal(t, "/staged/path", v.StagedPath)
}
