package synced

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const serverAddr = "localhost:50051"

func TestServerDownload(t *testing.T) {
	dir, _ := os.MkdirTemp("", "server_test")
	defer os.RemoveAll(dir)

	volumeDir := filepath.Join(dir, "content")
	os.Mkdir(volumeDir, 0755)
	os.Create(filepath.Join(volumeDir, "someFile"))
	os.Mkdir(filepath.Join(volumeDir, "someDir"), 0755)
	f, _ := os.Create(filepath.Join(dir, "archive.tar"))
	createArchive(dir, f)

	resolver := new(resolverMock)
	resolver.mock.On("Resolve", "missing").Return(filepath.Join(dir, ""), errors.New("not found"))
	resolver.mock.On("Resolve", "wrong").Return(filepath.Join(dir, "wrong.tar"), nil)
	resolver.mock.On("Resolve", "volume-id").Return(filepath.Join(dir, "archive.tar"), nil)
	fileServer := NewFileServer(dir)
	fileServer.Run(resolver)
	defer fileServer.Stop()

	ctx := context.Background()
	err := ClientDownload(ctx, serverAddr, "missing", dir)
	assert.NotNil(t, err)

	err = ClientDownload(ctx, serverAddr, "wrong", dir)
	assert.NotNil(t, err)

	err = ClientDownload(ctx, serverAddr, "volume-id", dir)
	assert.Nil(t, err)
}

func TestServerUpload(t *testing.T) {
	dir, _ := os.MkdirTemp("", "server_test")
	defer os.RemoveAll(dir)

	resolver := new(resolverMock)
	resolver.mock.On("Resolve", "volume-id").Return(filepath.Join(dir, "archive.tar"), nil)
	resolver.mock.On("Save", "volume-id", mock.AnythingOfType("string")).Return(nil)
	fileServer := NewFileServer(dir)
	fileServer.Run(resolver)
	defer fileServer.Stop()

	err := ClientUpload(serverAddr, dir, "volume-id")
	assert.Nil(t, err)
}

type resolverMock struct {
	mock mock.Mock
}

func (m *resolverMock) Resolve(volumeID string) (string, error) {
	args := m.mock.Called(volumeID)
	return args.Get(0).(string), nil
}

func (m *resolverMock) Save(volumeID string, file string) error {
	m.mock.Called(volumeID, file)
	return nil
}
