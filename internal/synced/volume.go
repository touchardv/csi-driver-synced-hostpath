package synced

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pborman/uuid"
	"k8s.io/klog/v2"
)

type Volume struct {
	Capacity   int64
	ID         string
	Name       string
	Published  bool
	Staged     bool
	StagedPath string
}

func archiveFile(dir string, volumeID string) string {
	return filepath.Join(dir, volumeID, "archive.tar")
}

func createVolume(name string, capacity int64, dir string) (*Volume, error) {
	volumeID := uuid.NewUUID().String()
	if err := createEmptyArchive(dir, volumeID); err != nil {
		return nil, err
	}
	return &Volume{
		Capacity: capacity,
		ID:       volumeID,
		Name:     name,
	}, nil
}

func createEmptyArchive(dir string, volumeID string) error {
	klog.V(4).Info("Creating empty archive in: ", dir)
	file := archiveFile(dir, volumeID)
	if err := os.Mkdir(filepath.Dir(file), os.FileMode(0755)); err != nil {
		return fmt.Errorf("could not create archive directory: %w", err)
	}
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("could not create archive file: %w", err)
	}
	defer f.Close()

	tw := tar.NewWriter(f)
	if err := tw.Close(); err != nil {
		return fmt.Errorf("could not close tar writer: %w", err)
	}
	return nil
}

func createArchive(dir string, w io.Writer) error {
	klog.V(4).Info("Creating archive of: ", dir)
	writer := tar.NewWriter(w)
	defer writer.Close()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create the header based on file info
		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}

		// Ensure the name in the tar is relative to the baseDir
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := writer.WriteHeader(header); err != nil {
			return err
		}

		// If it's a directory, we just write the header and move on
		if info.IsDir() {
			return nil
		}

		// If it's a file, open it and copy its contents to the tar writer
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
}

func newStagedVolume(id string, stagedPath string) *Volume {
	return &Volume{
		ID:         id,
		Staged:     true,
		StagedPath: stagedPath,
	}
}
