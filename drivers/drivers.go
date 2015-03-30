package drivers

import (
	"fmt"
	"github.com/yasker/volmgr/metadata"
	"golang.org/x/sys/unix"
	"os/exec"
)

type InitFunc func(root string, config map[string]string) (Driver, error)

type Driver interface {
	Name() string
	CreateVolume(id, baseID string, size int64) error
	DeleteVolume(id string) error
	GetVolumeDevice(id string) (string, error)
	ListVolume(id string) error
	CreateSnapshot(id, volumeID string) error
	DeleteSnapshot(id, volumeID string) error
	HasSnapshot(id, volumeID string) bool
	CompareSnapshot(id, compareID, volumeID string) (*metadata.Mappings, error)
	OpenSnapshot(id, volumeID string) error
	ReadSnapshot(id, volumeID string, start int64, data []byte) error
	CloseSnapshot(id, volumeID string) error
	Info() error
}

var (
	initializers map[string]InitFunc
)

func init() {
	initializers = make(map[string]InitFunc)
}

func Register(name string, initFunc InitFunc) error {
	if _, exists := initializers[name]; exists {
		return fmt.Errorf("Driver %s has already been registered", name)
	}
	initializers[name] = initFunc
	return nil
}

func GetDriver(name, root string, config map[string]string) (Driver, error) {
	if _, exists := initializers[name]; !exists {
		return nil, fmt.Errorf("Driver %v is not supported!", name)
	}
	return initializers[name](root, config)
}

func Mount(driver Driver, volumeUUID, mountPoint, fstype, option string, needFormat bool) error {
	dev, err := driver.GetVolumeDevice(volumeUUID)
	if err != nil {
		return err
	}
	if fstype != "ext4" {
		return fmt.Errorf("unsupported filesystem ", fstype)
	}
	if needFormat {
		if err := exec.Command("mkfs", "-t", fstype, dev).Run(); err != nil {
			return err
		}
	}
	var flags uintptr = unix.MS_MGC_VAL
	if err := unix.Mount(dev, mountPoint, fstype, flags, option); err != nil {
		return err
	}
	return nil
}

func Unmount(driver Driver, mountPoint string) error {
	return unix.Unmount(mountPoint, unix.MNT_DETACH)
}
