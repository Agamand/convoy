package main

import (
	"code.google.com/p/go-uuid/uuid"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/yasker/volmgr/api"
	"github.com/yasker/volmgr/blockstores"
	"github.com/yasker/volmgr/drivers"
	"github.com/yasker/volmgr/utils"
	"path/filepath"
)

func getDriverRoot(root, driverName string) string {
	return filepath.Join(root, driverName) + "/"
}

func getConfigFileName(root string) string {
	return filepath.Join(root, CONFIGFILE)
}

func doInitialize(root, driverName string, driverOpts map[string]string) error {
	log.Debug("Config root is ", root)

	driverRoot := getDriverRoot(root, driverName)
	utils.MkdirIfNotExists(driverRoot)
	log.Debug("Driver root is ", driverRoot)

	_, err := drivers.GetDriver(driverName, driverRoot, driverOpts)
	if err != nil {
		return err
	}

	configFileName := getConfigFileName(root)
	config := Config{
		Root:    root,
		Driver:  driverName,
		Volumes: make(map[string]Volume),
	}
	err = utils.SaveConfig(configFileName, &config)
	return err
}

func doInfo(config *Config, driver drivers.Driver) error {
	err := driver.Info()
	return err
}

func doVolumeCreate(config *Config, driver drivers.Driver, size int64) error {
	uuid := uuid.New()
	base := "" //Doesn't support base for now
	if err := driver.CreateVolume(uuid, base, size); err != nil {
		return err
	}
	log.Debug("Created volume using ", config.Driver)
	config.Volumes[uuid] = Volume{
		Base:       base,
		Size:       size,
		MountPoint: "",
		FileSystem: "",
		Snapshots:  make(map[string]bool),
	}
	if err := utils.SaveConfig(getConfigFileName(config.Root), config); err != nil {
		return err
	}
	api.ResponseOutput(api.VolumeResponse{
		UUID: uuid,
		Base: base,
		Size: size,
	})
	return nil
}

func doVolumeDelete(config *Config, driver drivers.Driver, uuid string) error {
	if err := driver.DeleteVolume(uuid); err != nil {
		return err
	}
	log.Debug("Deleted volume using ", config.Driver)
	delete(config.Volumes, uuid)
	return utils.SaveConfig(getConfigFileName(config.Root), config)
}

func doVolumeUpdate(config *Config, driver drivers.Driver, uuid string, size int64) error {
	return fmt.Errorf("Doesn't support change size of volume yet")
}

func doVolumeList(config *Config, driver drivers.Driver, id string) error {
	err := driver.ListVolume(id)
	return err
}

func doVolumeMount(config *Config, driver drivers.Driver, volumeUUID, mountPoint, fs, option string, needFormat bool) error {
	volume, exists := config.Volumes[volumeUUID]
	if !exists {
		return fmt.Errorf("volume %v doesn't exist", volumeUUID)
	}
	if err := drivers.Mount(driver, volumeUUID, mountPoint, fs, option, needFormat); err != nil {
		return err
	}
	log.Debugf("Mount %v to %v", volumeUUID, mountPoint)
	volume.MountPoint = mountPoint
	volume.FileSystem = fs
	config.Volumes[volumeUUID] = volume
	return utils.SaveConfig(getConfigFileName(config.Root), config)
}

func doVolumeUnmount(config *Config, driver drivers.Driver, volumeUUID string) error {
	volume, exists := config.Volumes[volumeUUID]
	if !exists {
		return fmt.Errorf("volume %v doesn't exist", volumeUUID)
	}
	if err := drivers.Unmount(driver, volume.MountPoint); err != nil {
		return err
	}
	log.Debugf("Unmount %v from %v", volumeUUID, volume.MountPoint)
	volume.MountPoint = ""
	config.Volumes[volumeUUID] = volume
	return utils.SaveConfig(getConfigFileName(config.Root), config)
}

func doSnapshotCreate(config *Config, driver drivers.Driver, volumeUUID string) error {
	if _, exists := config.Volumes[volumeUUID]; !exists {
		return fmt.Errorf("volume %v doesn't exist", volumeUUID)
	}
	uuid := uuid.New()
	if err := driver.CreateSnapshot(uuid, volumeUUID); err != nil {
		return err
	}
	log.Debugf("Created snapshot %v of volume %v using %v\n", uuid, volumeUUID, config.Driver)

	config.Volumes[volumeUUID].Snapshots[uuid] = true
	if err := utils.SaveConfig(getConfigFileName(config.Root), config); err != nil {
		return err
	}
	api.ResponseOutput(api.SnapshotResponse{
		UUID:       uuid,
		VolumeUUID: volumeUUID,
	})
	return nil
}

func doSnapshotDelete(config *Config, driver drivers.Driver, uuid, volumeUUID string) error {
	if _, exists := config.Volumes[volumeUUID]; !exists {
		return fmt.Errorf("volume %v doesn't exist", volumeUUID)
	}
	if _, exists := config.Volumes[volumeUUID].Snapshots[uuid]; !exists {
		return fmt.Errorf("snapshot %v of volume %v doesn't exist", uuid, volumeUUID)
	}
	if err := driver.DeleteSnapshot(uuid, volumeUUID); err != nil {
		return err
	}
	log.Debugf("Deleted snapshot %v of volume %v using %v\n", uuid, volumeUUID, config.Driver)

	delete(config.Volumes[volumeUUID].Snapshots, uuid)
	return utils.SaveConfig(getConfigFileName(config.Root), config)
}

const (
	BLOCKSTORE_PATH = "blockstore"
)

func getBlockStoreRoot(root string) string {
	return filepath.Join(root, BLOCKSTORE_PATH) + "/"
}

func doBlockStoreRegister(config *Config, kind string, opts map[string]string) error {
	path := getBlockStoreRoot(config.Root)
	err := utils.MkdirIfNotExists(path)
	if err != nil {
		return err
	}
	id, blockSize, err := blockstores.Register(path, kind, opts)
	if err != nil {
		return err
	}

	api.ResponseOutput(api.BlockStoreResponse{
		UUID:      id,
		Kind:      kind,
		BlockSize: blockSize,
	})
	return nil
}

func doBlockStoreDeregister(config *Config, id string) error {
	return blockstores.Deregister(getBlockStoreRoot(config.Root), id)
}

func doBlockStoreAdd(config *Config, blockstoreId, volumeId string) error {
	volume, exists := config.Volumes[volumeId]
	if !exists {
		return fmt.Errorf("volume %v doesn't exist", volumeId)
	}

	return blockstores.AddVolume(getBlockStoreRoot(config.Root), blockstoreId, volumeId, volume.Base, volume.Size)
}

func doBlockStoreRemove(config *Config, blockstoreId, volumeId string) error {
	if _, exists := config.Volumes[volumeId]; !exists {
		return fmt.Errorf("volume %v doesn't exist", volumeId)
	}

	return blockstores.RemoveVolume(getBlockStoreRoot(config.Root), blockstoreId, volumeId)
}

func doSnapshotBackup(config *Config, driver drivers.Driver, snapshotId, volumeId, blockstoreId string) error {
	if _, exists := config.Volumes[volumeId]; !exists {
		return fmt.Errorf("volume %v doesn't exist", volumeId)
	}
	if _, exists := config.Volumes[volumeId].Snapshots[snapshotId]; !exists {
		return fmt.Errorf("snapshot %v of volume %v doesn't exist", snapshotId, volumeId)
	}

	return blockstores.BackupSnapshot(getBlockStoreRoot(config.Root), snapshotId, volumeId, blockstoreId, driver)
}

func doSnapshotRestore(config *Config, driver drivers.Driver, snapshotId, originVolumeId, targetVolumeId, blockstoreId string) error {
	originVol, exists := config.Volumes[originVolumeId]
	if !exists {
		return fmt.Errorf("volume %v doesn't exist", originVolumeId)
	}
	if _, exists := config.Volumes[originVolumeId].Snapshots[snapshotId]; !exists {
		return fmt.Errorf("snapshot %v of volume %v doesn't exist", snapshotId, originVolumeId)
	}
	targetVol, exists := config.Volumes[targetVolumeId]
	if !exists {
		return fmt.Errorf("volume %v doesn't exist", targetVolumeId)
	}
	if originVol.Size != targetVol.Size || originVol.Base != targetVol.Base {
		return fmt.Errorf("target volume %v doesn't match original volume %v's size or base",
			targetVolumeId, originVolumeId)
	}

	return blockstores.RestoreSnapshot(getBlockStoreRoot(config.Root), snapshotId, originVolumeId,
		targetVolumeId, blockstoreId, driver)
}

func doSnapshotRemove(config *Config, snapshotId, volumeId, blockstoreId string) error {
	if _, exists := config.Volumes[volumeId]; !exists {
		return fmt.Errorf("volume %v doesn't exist", volumeId)
	}
	if _, exists := config.Volumes[volumeId].Snapshots[snapshotId]; !exists {
		return fmt.Errorf("snapshot %v of volume %v doesn't exist", snapshotId, volumeId)
	}

	return blockstores.RemoveSnapshot(getBlockStoreRoot(config.Root), snapshotId, volumeId, blockstoreId)
}
