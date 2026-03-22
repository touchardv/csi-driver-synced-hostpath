//go:build darwin

package synced

import "k8s.io/mount-utils"

func NewSafeMounter() mount.Interface {
	return &mount.FakeMounter{}
}
