package synced

import (
	"archive/tar"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildArchive creates a tar archive in-memory (as a temp file) containing
// entries with specific modes, uids and gids. Returns the archive path.
func buildArchive(t *testing.T, entries []tarEntry) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "archive-*.tar")
	require.NoError(t, err)
	defer f.Close()

	tw := tar.NewWriter(f)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Mode:     e.mode,
			Uid:      e.uid,
			Gid:      e.gid,
			Size:     int64(len(e.content)),
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if len(e.content) > 0 {
			_, err := tw.Write([]byte(e.content))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	return f.Name()
}

type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	uid, gid int
	content  string
}

// resolverFor returns a FileHandler mock that always resolves to archivePath.
func resolverFor(archivePath string) FileHandler {
	return &staticResolver{path: archivePath}
}

type staticResolver struct{ path string }

func (s *staticResolver) Resolve(_ string) (string, error) { return s.path, nil }
func (s *staticResolver) Save(_ string, _ string) error    { return nil }

// resolverError is a FileHandler that always fails Resolve.
type resolverError struct{}

func (r *resolverError) Resolve(_ string) (string, error) {
	return "", errors.New("not found")
}
func (r *resolverError) Save(_ string, _ string) error { return nil }

// startServer is a test helper that starts a file server using the given
// FileHandler on a fixed address, and returns its address. The server is shut
// down on test cleanup.
func startServer(t *testing.T, handler FileHandler) string {
	t.Helper()
	const addr = "localhost:50052"
	srv := NewFileServer(t.TempDir(), addr)
	err := srv.Run(handler)
	require.NoError(t, err)
	t.Cleanup(srv.Stop)
	return addr
}

// TestClientDownload_FilePermissions verifies that file modes are restored
// exactly (Bug 2: OpenFile mode is filtered by umask; Chmod must be used).
func TestClientDownload_FilePermissions(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("file ownership assertions require root; skipping uid/gid checks")
	}

	archivePath := buildArchive(t, []tarEntry{
		{name: ".", typeflag: tar.TypeDir, mode: 0755, uid: 0, gid: 0},
		{name: "exec.sh", typeflag: tar.TypeReg, mode: 0755, uid: 0, gid: 0, content: "#!/bin/sh\n"},
		{name: "readonly.txt", typeflag: tar.TypeReg, mode: 0444, uid: 0, gid: 0, content: "ro\n"},
		{name: "private.txt", typeflag: tar.TypeReg, mode: 0600, uid: 0, gid: 0, content: "secret\n"},
	})

	addr := startServer(t, resolverFor(archivePath))
	destDir := t.TempDir()

	err := ClientDownload(context.Background(), addr, "vol-perm", destDir)
	require.NoError(t, err)

	cases := []struct {
		path string
		want os.FileMode
	}{
		{"exec.sh", 0755},
		{"readonly.txt", 0444},
		{"private.txt", 0600},
	}
	for _, tc := range cases {
		info, err := os.Stat(filepath.Join(destDir, tc.path))
		require.NoError(t, err, tc.path)
		assert.Equal(t, tc.want, info.Mode().Perm(), "mode mismatch for %s", tc.path)
	}
}

// TestClientDownload_DirectoryPermissions verifies that directory modes are
// restored exactly (Bug 1: MkdirAll used hardcoded 0755).
func TestClientDownload_DirectoryPermissions(t *testing.T) {
	archivePath := buildArchive(t, []tarEntry{
		{name: ".", typeflag: tar.TypeDir, mode: 0755, uid: 0, gid: 0},
		{name: "secret-dir", typeflag: tar.TypeDir, mode: 0700, uid: 0, gid: 0},
		{name: "secret-dir/file.txt", typeflag: tar.TypeReg, mode: 0600, uid: 0, gid: 0, content: "x"},
		{name: "shared-dir", typeflag: tar.TypeDir, mode: 0750, uid: 0, gid: 0},
	})

	addr := startServer(t, resolverFor(archivePath))
	destDir := t.TempDir()

	err := ClientDownload(context.Background(), addr, "vol-dir-perm", destDir)
	require.NoError(t, err)

	cases := []struct {
		path string
		want os.FileMode
	}{
		{"secret-dir", 0700},
		{"shared-dir", 0750},
		{"secret-dir/file.txt", 0600},
	}
	for _, tc := range cases {
		info, err := os.Stat(filepath.Join(destDir, tc.path))
		require.NoError(t, err, tc.path)
		assert.Equal(t, tc.want, info.Mode().Perm(), "mode mismatch for %s", tc.path)
	}
}

// TestClientDownload_Ownership verifies that uid/gid are restored (Bug 3:
// no os.Lchown was called). Requires root to set arbitrary uid/gid.
func TestClientDownload_Ownership(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ownership restoration requires root (os.Lchown with arbitrary uid/gid)")
	}

	archivePath := buildArchive(t, []tarEntry{
		{name: ".", typeflag: tar.TypeDir, mode: 0755, uid: 0, gid: 0},
		{name: "secret-dir", typeflag: tar.TypeDir, mode: 0700, uid: 5678, gid: 1234},
		{name: "secret-dir/secret.txt", typeflag: tar.TypeReg, mode: 0600, uid: 5678, gid: 1234, content: "secret"},
		{name: "shared.txt", typeflag: tar.TypeReg, mode: 0640, uid: 0, gid: 1234, content: "shared"},
	})

	addr := startServer(t, resolverFor(archivePath))
	destDir := t.TempDir()

	err := ClientDownload(context.Background(), addr, "vol-ownership", destDir)
	require.NoError(t, err)

	type ownerCheck struct {
		path         string
		wantMode     os.FileMode
		wantUID, wantGID int
	}
	cases := []ownerCheck{
		{"secret-dir", 0700, 5678, 1234},
		{"secret-dir/secret.txt", 0600, 5678, 1234},
		{"shared.txt", 0640, 0, 1234},
	}
	for _, tc := range cases {
		full := filepath.Join(destDir, tc.path)
		info, err := os.Stat(full)
		require.NoError(t, err, tc.path)
		assert.Equal(t, tc.wantMode, info.Mode().Perm(), "mode mismatch for %s", tc.path)

		sys := info.Sys()
		require.NotNil(t, sys)
		// Use syscall.Stat_t for uid/gid — available on Linux & macOS.
		if st, ok := sys.(*syscall.Stat_t); ok {
			assert.Equal(t, uint32(tc.wantUID), st.Uid, "uid mismatch for %s", tc.path)
			assert.Equal(t, uint32(tc.wantGID), st.Gid, "gid mismatch for %s", tc.path)
		}
	}
}

// TestClientDownload_PathTraversal verifies the tar-slip guard still works.
func TestClientDownload_PathTraversal(t *testing.T) {
	archivePath := buildArchive(t, []tarEntry{
		{name: "../../etc/passwd", typeflag: tar.TypeReg, mode: 0644, content: "evil"},
	})

	addr := startServer(t, resolverFor(archivePath))
	destDir := t.TempDir()

	err := ClientDownload(context.Background(), addr, "vol-traversal", destDir)
	assert.Error(t, err, "expected error for path traversal attempt")
}
