package synced

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.yaml.in/yaml/v2"
	"k8s.io/klog/v2"
)

type Service interface {
	CreateVolume(name string, capacity int64) (string, error)

	DeleteVolume(id string) error

	// Get the volume info by ID.
	// If found then returns the volume info and true.
	GetVolumeByID(id string) (Volume, bool)

	// Get a volume info by name.
	// If found then returns the volume info and true.
	GetVolumeByName(name string) (Volume, bool)

	StageVolume(ctx context.Context, id string, stagingTargetPath string) error

	PublishVolume(id string, stagingTargetPath string, targetPath string, readOnly bool) error

	UnpublishVolume(id string, targetPath string) error

	UnstageVolume(id string, stagingTargetPath string) error

	Stop() error

	FileHandler
}

type service struct {
	fileServerAddr string
	mutex          sync.Mutex
	stateDir       string
	volumes        map[string]*Volume
}

func NewService(stateDir string, fileServerAddr string) Service {
	ensureExistLocalDir(stateDir)

	volumes := map[string]*Volume{}
	stateFile := filepath.Join(stateDir, "volumes.yaml")
	if existLocalFile(stateFile) {
		klog.Infof("Reading service state from %s", stateFile)
		b, err := os.ReadFile(stateFile)
		if err != nil {
			klog.Fatalf("failed to read service state file: %v", err)
		}
		err = yaml.Unmarshal(b, volumes)
		if err != nil {
			klog.Fatalf("failed to decode service state file content: %v", err)
		}
	} else {
		klog.Info("No service state found")
	}

	return &service{
		fileServerAddr: fileServerAddr,
		stateDir:       stateDir,
		volumes:        volumes,
	}
}

func (s *service) Resolve(volumeID string) (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if v, found := s.volumes[volumeID]; found {
		return archiveFile(s.stateDir, v.ID), nil
	}
	return "", errors.New("volume not found")
}

func (s *service) Save(volumeID string, file string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if v, found := s.volumes[volumeID]; found {
		return copyFile(file, archiveFile(s.stateDir, v.ID))
	}
	return errors.New("volume not found")
}

func (s *service) Stop() error {
	return s.save()
}

func (s *service) save() error {
	klog.V(2).Info("Saving service state")
	b, err := yaml.Marshal(s.volumes)
	if err != nil {
		klog.Errorf("failed to encode state file content: %v", err)
		return err
	}
	stateFile := filepath.Join(s.stateDir, "volumes.yaml")
	if err = os.WriteFile(stateFile, b, 0644); err != nil {
		klog.Errorf("failed to save state file: %v", err)
		return err
	}
	return nil
}

func (s *service) CreateVolume(name string, capacity int64) (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	v, err := createVolume(name, capacity, s.stateDir)
	if err != nil {
		return "", err
	}
	s.volumes[v.ID] = v
	return v.ID, s.save()
}

func (s *service) DeleteVolume(volumeID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	archivesPath := filepath.Join(s.stateDir, volumeID)
	if err := os.RemoveAll(archivesPath); err != nil {
		return err
	}
	delete(s.volumes, volumeID)
	return s.save()
}

func (s *service) GetVolumeByID(id string) (Volume, bool) {
	v, found := s.volumes[id]
	if found {
		return *v, true
	}
	return Volume{}, false
}

func (s *service) GetVolumeByName(name string) (Volume, bool) {
	for _, v := range s.volumes {
		if v.Name == name {
			return *v, true
		}
	}
	return Volume{}, false
}

func (s *service) StageVolume(ctx context.Context, id string, stagingTargetPath string) error {
	if err := ClientDownload(ctx, s.fileServerAddr, id, stagingTargetPath); err != nil {
		klog.Infof("failed to download volume backup: %v", err)
		return err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.volumes[id] = newStagedVolume(id, stagingTargetPath)
	return s.save()
}

func (s *service) PublishVolume(volumeID string, stagingTargetPath string, targetPath string, readOnly bool) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	mounter := NewSafeMounter()
	isMount, err := mounter.IsMountPoint(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.Mkdir(targetPath, 0750); err != nil {
				return fmt.Errorf("create target path: %w", err)
			}
			isMount = false
		} else {
			return fmt.Errorf("check target path: %w", err)
		}
	}
	if isMount {
		return nil
	}

	mountOptions := []string{"bind"}
	if readOnly {
		mountOptions = append(mountOptions, "ro")
	}
	if err := mounter.Mount(stagingTargetPath, targetPath, "", mountOptions); err != nil {
		var errList strings.Builder
		errList.WriteString(err.Error())
		return fmt.Errorf("failed to mount device: %s at %s: %s", stagingTargetPath, targetPath, errList.String())
	}

	v, _ := s.volumes[volumeID]
	v.Published = true
	return s.save()
}

func (s *service) UnpublishVolume(volumeID string, targetPath string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	mounter := NewSafeMounter()
	if isMount, err := mounter.IsMountPoint(targetPath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to check mount path: %w", err)
		}
	} else if isMount {
		if err = mounter.Unmount(targetPath); err != nil {
			klog.Errorf("failed to unmount: %s\nError: %v", targetPath, err)
			return err
		}
	}
	if err := os.RemoveAll(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove target path: %w", err)
	}

	v, _ := s.volumes[volumeID]
	v.Published = false
	return s.save()
}

func (s *service) UnstageVolume(volumeID string, stagingTargetPath string) error {
	if err := ClientUpload(s.fileServerAddr, stagingTargetPath, volumeID); err != nil {
		klog.Infof("failed to upload volume backup: %v", err)
		return err
	}
	if err := os.RemoveAll(stagingTargetPath); err != nil {
		klog.Infof("failed to cleanup unstaged volume: %v", err)
		return err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.volumes, volumeID)
	return s.save()
}
