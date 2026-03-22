//go:build linux

package synced

import (
	"k8s.io/mount-utils"
	"k8s.io/utils/exec"
)

func NewSafeMounter() mount.Interface {
	return &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: exec.New()}
}
