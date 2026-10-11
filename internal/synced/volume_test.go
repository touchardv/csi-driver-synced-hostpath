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

func TestCreateArchive(t *testing.T) {
	dir, err := os.MkdirTemp("", "archive_test")
	assert.Nil(t, err)
	defer os.RemoveAll(dir)

	// Create subdirectories and files
	subDir := filepath.Join(dir, "nested", "sub")
	err = os.MkdirAll(subDir, 0755)
	assert.Nil(t, err)

	filePath := filepath.Join(subDir, "hello.txt")
	err = os.WriteFile(filePath, []byte("world"), 0644)
	assert.Nil(t, err)

	archivePath := filepath.Join(dir, "output.tar")
	outFile, err := os.Create(archivePath)
	assert.Nil(t, err)
	defer outFile.Close()

	err = createArchive(dir, outFile)
	assert.Nil(t, err)

	assert.FileExists(t, archivePath)
	info, err := os.Stat(archivePath)
	assert.Nil(t, err)
	assert.True(t, info.Size() > 0)
}

