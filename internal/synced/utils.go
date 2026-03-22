package synced

import (
	"io"
	"os"

	"k8s.io/klog/v2"
)

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return destFile.Sync()
}

func ensureExistLocalDir(dir string) {
	dirInfo, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(dir, os.FileMode(0755))
			if err != nil {
				klog.Fatalf("failed to create directory: %v", err)
			}
		} else {
			klog.Fatalf("failed to check directory: %v", err)
		}
	} else {
		if !dirInfo.IsDir() {
			klog.Fatalf("can not use %s as directory", dir)
		}
	}
}

func existLocalFile(file string) bool {
	fileInfo, err := os.Stat(file)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		} else {
			klog.Fatalf("failed to check file: %v", err)
		}
	}
	if fileInfo.IsDir() {
		klog.Fatalf("can not use directory %s as file", file)
	}
	return true
}
