// +build linux

package devmapper

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/pkg/devicemapper"
	"github.com/rancherio/volmgr/api"
	"github.com/rancherio/volmgr/drivers"
	"github.com/rancherio/volmgr/metadata"
	"github.com/rancherio/volmgr/util"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	. "github.com/rancherio/volmgr/logging"
)

var (
	log = logrus.WithFields(logrus.Fields{"pkg": "devmapper"})
)

const (
	DRIVER_NAME           = "devicemapper"
	DEFAULT_THINPOOL_NAME = "rancher-volume-pool"
	DEFAULT_BLOCK_SIZE    = "4096"
	DM_DIR                = "/dev/mapper/"

	DM_DATA_DEV            = "dm.datadev"
	DM_METADATA_DEV        = "dm.metadatadev"
	DM_THINPOOL_NAME       = "dm.thinpoolname"
	DM_THINPOOL_BLOCK_SIZE = "dm.thinpoolblocksize"

	// as defined in device mapper thin provisioning
	BLOCK_SIZE_MIN        = 128
	BLOCK_SIZE_MAX        = 2097152
	BLOCK_SIZE_MULTIPLIER = 128

	SECTOR_SIZE = 512

	VOLUME_CFG_PREFIX     = "volume_"
	IMAGE_CFG_PREFIX      = "image_"
	DEVMAPPER_CFG_POSTFIX = "_" + DRIVER_NAME + ".json"

	DM_LOG_FIELD_VOLUME_DEVID   = "dm_volume_devid"
	DM_LOG_FIELD_SNAPSHOT_DEVID = "dm_snapshot_devid"
)

type Driver struct {
	root       string
	configName string
	Device
}

type Volume struct {
	UUID      string
	DevID     int
	Size      int64
	Base      string
	Snapshots map[string]Snapshot
}

type Snapshot struct {
	DevID     int
	Activated bool
}

type Image struct {
	UUID      string
	FilePath  string
	Size      int64
	Device    string
	VolumeRef map[string]bool
}

type Device struct {
	Root              string
	DataDevice        string
	MetadataDevice    string
	ThinpoolDevice    string
	ThinpoolSize      int64
	ThinpoolBlockSize int64
	LastDevID         int
}

func init() {
	drivers.Register(DRIVER_NAME, Init)
}

func getVolumeCfgName(uuid string) (string, error) {
	if uuid == "" {
		return "", fmt.Errorf("Invalid volume UUID specified: %v", uuid)
	}
	return VOLUME_CFG_PREFIX + uuid + DEVMAPPER_CFG_POSTFIX, nil
}

func (device *Device) loadVolume(uuid string) *Volume {
	cfgName, err := getVolumeCfgName(uuid)
	if err != nil {
		return nil
	}
	if !util.ConfigExists(device.Root, cfgName) {
		return nil
	}
	volume := &Volume{}
	if err := util.LoadConfig(device.Root, cfgName, volume); err != nil {
		log.Error("Failed to load volume json ", cfgName)
		return nil
	}
	return volume
}

func (device *Device) saveVolume(volume *Volume) error {
	uuid := volume.UUID
	cfgName, err := getVolumeCfgName(uuid)
	if err != nil {
		return err
	}
	return util.SaveConfig(device.Root, cfgName, volume)
}

func (device *Device) deleteVolume(uuid string) error {
	cfgName, err := getVolumeCfgName(uuid)
	if err != nil {
		return err
	}
	return util.RemoveConfig(device.Root, cfgName)
}

func (device *Device) listVolumeIDs() []string {
	return util.ListConfigIDs(device.Root, VOLUME_CFG_PREFIX, DEVMAPPER_CFG_POSTFIX)
}

func verifyConfig(config map[string]string) (*Device, error) {
	dv := Device{
		DataDevice:     config[DM_DATA_DEV],
		MetadataDevice: config[DM_METADATA_DEV],
	}

	if dv.DataDevice == "" || dv.MetadataDevice == "" {
		return nil, fmt.Errorf("data device or metadata device unspecified")
	}

	if _, exists := config[DM_THINPOOL_NAME]; !exists {
		config[DM_THINPOOL_NAME] = DEFAULT_THINPOOL_NAME
	}
	dv.ThinpoolDevice = filepath.Join(DM_DIR, config[DM_THINPOOL_NAME])

	if _, exists := config[DM_THINPOOL_BLOCK_SIZE]; !exists {
		config[DM_THINPOOL_BLOCK_SIZE] = DEFAULT_BLOCK_SIZE
	}

	blockSizeString := config[DM_THINPOOL_BLOCK_SIZE]
	blockSize, err := strconv.ParseInt(blockSizeString, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("Illegal block size specified")
	}
	if blockSize < BLOCK_SIZE_MIN || blockSize > BLOCK_SIZE_MAX || blockSize%BLOCK_SIZE_MULTIPLIER != 0 {
		return nil, fmt.Errorf("Block size must between %v and %v, and must be a multiple of %v",
			BLOCK_SIZE_MIN, BLOCK_SIZE_MAX, BLOCK_SIZE_MULTIPLIER)
	}

	dv.ThinpoolBlockSize = blockSize

	return &dv, nil
}

func (d *Driver) activatePool() error {
	dev := d.Device
	if _, err := os.Stat(dev.ThinpoolDevice); err == nil {
		log.Debug("Found created pool, skip pool reinit")
		return nil
	}

	dataDev, err := os.Open(dev.DataDevice)
	if err != nil {
		return err
	}
	defer dataDev.Close()

	metadataDev, err := os.Open(dev.MetadataDevice)
	if err != nil {
		return err
	}
	defer metadataDev.Close()

	if err := createPool(filepath.Base(dev.ThinpoolDevice), dataDev, metadataDev, uint32(dev.ThinpoolBlockSize)); err != nil {
		return err
	}
	log.Debug("Reinitialized the existing pool ", dev.ThinpoolDevice)

	volumeIDs := dev.listVolumeIDs()
	for _, id := range volumeIDs {
		volume := dev.loadVolume(id)
		if volume == nil {
			return fmt.Errorf("Cannot find volume %v", id)
		}
		if err := devicemapper.ActivateDevice(dev.ThinpoolDevice, id, volume.DevID, uint64(volume.Size)); err != nil {
			return err
		}
		log.WithFields(logrus.Fields{
			LOG_FIELD_EVENT:  LOG_EVENT_ACTIVATE,
			LOG_FIELD_VOLUME: id,
		}).Debug("Reactivated volume device")
	}
	return nil
}

func Init(root, cfgName string, config map[string]string) (drivers.Driver, error) {
	if util.ConfigExists(root, cfgName) {
		dev := Device{}
		err := util.LoadConfig(root, cfgName, &dev)
		d := &Driver{}
		if err != nil {
			return d, err
		}
		d.Device = dev
		d.configName = cfgName
		d.root = root
		if err := d.activatePool(); err != nil {
			return d, err
		}
		return d, nil
	}

	dev, err := verifyConfig(config)
	if err != nil {
		return nil, err
	}

	dev.Root = root

	dataDev, err := os.Open(dev.DataDevice)
	if err != nil {
		return nil, err
	}
	defer dataDev.Close()

	metadataDev, err := os.Open(dev.MetadataDevice)
	if err != nil {
		return nil, err
	}
	defer metadataDev.Close()

	thinpSize, err := devicemapper.GetBlockDeviceSize(dataDev)
	if err != nil {
		return nil, err
	}
	dev.ThinpoolSize = int64(thinpSize)
	dev.LastDevID = 1

	err = createPool(filepath.Base(dev.ThinpoolDevice), dataDev, metadataDev, uint32(dev.ThinpoolBlockSize))
	if err != nil {
		return nil, err
	}

	err = util.SaveConfig(root, cfgName, &dev)
	if err != nil {
		return nil, err
	}
	d := &Driver{
		root:       root,
		configName: cfgName,
		Device:     *dev,
	}
	log.Debug("Init done")
	return d, nil
}

func createPool(poolName string, dataDev, metadataDev *os.File, blockSize uint32) error {
	err := devicemapper.CreatePool(poolName, dataDev, metadataDev, blockSize)
	if err != nil {
		return err
	}
	log.Debugln("Created pool /dev/mapper/" + poolName)

	return nil
}

func (d *Driver) Name() string {
	return DRIVER_NAME
}

func (d *Driver) CreateVolume(id, baseID string, size int64) error {
	if size%(d.ThinpoolBlockSize*SECTOR_SIZE) != 0 {
		return fmt.Errorf("Size must be multiple of block size")

	}
	volume := d.loadVolume(id)
	if volume != nil {
		return fmt.Errorf("Already has volume with uuid %v", id)
	}

	var image *Image
	if baseID != "" {
		image = d.loadImage(baseID)
		if image == nil {
			return fmt.Errorf("Cannot find activated base image %v", baseID)
		}
		if _, err := os.Stat(image.Device); err != nil {
			return fmt.Errorf("Base image device %v doesn't exist", image.Device)
		}
		if size != image.Size {
			return fmt.Errorf("Volume has different size(%v) than base image(%v)",
				size, image.Size)
		}
	}

	devID := d.LastDevID
	log.WithFields(logrus.Fields{
		LOG_FIELD_REASON:          LOG_REASON_START,
		LOG_FIELD_EVENT:           LOG_EVENT_CREATE,
		LOG_FIELD_VOLUME:          id,
		LOG_FIELD_IMAGE:           baseID,
		DM_LOG_FIELD_VOLUME_DEVID: devID,
	}).Debugf("Creating volume")
	err := devicemapper.CreateDevice(d.ThinpoolDevice, devID)
	if err != nil {
		return err
	}

	log.WithFields(logrus.Fields{
		LOG_FIELD_REASON:          LOG_REASON_START,
		LOG_FIELD_EVENT:           LOG_EVENT_ACTIVATE,
		LOG_FIELD_VOLUME:          id,
		LOG_FIELD_IMAGE:           baseID,
		DM_LOG_FIELD_VOLUME_DEVID: devID,
	}).Debugf("Activating device for volume")
	if image == nil {
		err = devicemapper.ActivateDevice(d.ThinpoolDevice, id, devID, uint64(size))
	} else {
		err = devicemapper.ActivateDeviceWithExternal(d.ThinpoolDevice, id, devID, uint64(size), image.Device)
	}
	if err != nil {
		log.WithFields(logrus.Fields{
			LOG_FIELD_REASON:          LOG_REASON_ROLLBACK,
			LOG_FIELD_EVENT:           LOG_EVENT_REMOVE,
			LOG_FIELD_VOLUME:          id,
			LOG_FIELD_IMAGE:           baseID,
			DM_LOG_FIELD_VOLUME_DEVID: devID,
		}).Debugf("Removing device for volume due to fail to activate")
		if err := devicemapper.DeleteDevice(d.ThinpoolDevice, devID); err != nil {
			log.WithFields(logrus.Fields{
				LOG_FIELD_REASON:          LOG_REASON_FAILURE,
				LOG_FIELD_EVENT:           LOG_EVENT_REMOVE,
				LOG_FIELD_VOLUME:          id,
				LOG_FIELD_IMAGE:           baseID,
				DM_LOG_FIELD_VOLUME_DEVID: devID,
			}).Debugf("Failed to remove device")
		}
		return err
	}

	volume = &Volume{
		UUID:      id,
		DevID:     devID,
		Size:      size,
		Snapshots: make(map[string]Snapshot),
	}
	if image != nil {
		volume.Base = image.UUID
		image.VolumeRef[id] = true
		if err := d.saveImage(image); err != nil {
			return err
		}
	}
	if err := d.saveVolume(volume); err != nil {
		return err
	}
	d.LastDevID++

	if err := util.SaveConfig(d.root, d.configName, d.Device); err != nil {
		return err
	}
	log.WithFields(logrus.Fields{
		LOG_FIELD_REASON: LOG_REASON_COMPLETE,
		LOG_FIELD_EVENT:  LOG_EVENT_CREATE,
		LOG_FIELD_VOLUME: id,
	}).Debug("Created volume")

	return nil
}

func (d *Driver) DeleteVolume(id string) error {
	var err error
	volume := d.loadVolume(id)
	if volume == nil {
		return fmt.Errorf("cannot find volume %v", id)
	}
	if len(volume.Snapshots) != 0 {
		return fmt.Errorf("Volume %v still contains snapshots, delete snapshots first", id)
	}

	if err = devicemapper.RemoveDevice(id); err != nil {
		return err
	}

	log.WithFields(logrus.Fields{
		LOG_FIELD_REASON:          LOG_REASON_START,
		LOG_FIELD_EVENT:           LOG_EVENT_REMOVE,
		LOG_FIELD_VOLUME:          id,
		DM_LOG_FIELD_VOLUME_DEVID: volume.DevID,
	}).Debugf("Deleting device")
	err = devicemapper.DeleteDevice(d.ThinpoolDevice, volume.DevID)
	if err != nil {
		return err
	}

	if volume.Base != "" {
		image := d.loadImage(volume.Base)
		if image == nil {
			return fmt.Errorf("Cannot find volume's base image %v", volume.Base)
		}
		if _, exists := image.VolumeRef[volume.UUID]; !exists {
			return fmt.Errorf("Volume's base image %v doesn't refer volume %v", volume.Base, volume.UUID)
		}
		delete(image.VolumeRef, volume.UUID)
	}

	if err := d.deleteVolume(id); err != nil {
		return err
	}

	log.WithFields(logrus.Fields{
		LOG_FIELD_REASON: LOG_REASON_COMPLETE,
		LOG_FIELD_EVENT:  LOG_EVENT_REMOVE,
		LOG_FIELD_VOLUME: id,
	}).Debugf("Deleted device")
	return nil
}

func getVolumeSnapshotInfo(uuid string, volume *Volume, snapshotID string) *api.DeviceMapperVolume {
	result := api.DeviceMapperVolume{
		DevID:     volume.DevID,
		Size:      volume.Size,
		Snapshots: make(map[string]api.DeviceMapperSnapshot),
	}
	if s, exists := volume.Snapshots[snapshotID]; exists {
		result.Snapshots[snapshotID] = api.DeviceMapperSnapshot{
			DevID: s.DevID,
		}
	}
	return &result
}

func getVolumeInfo(uuid string, volume *Volume) *api.DeviceMapperVolume {
	result := api.DeviceMapperVolume{
		DevID:     volume.DevID,
		Size:      volume.Size,
		Snapshots: make(map[string]api.DeviceMapperSnapshot),
	}
	for uuid, snapshot := range volume.Snapshots {
		s := api.DeviceMapperSnapshot{
			DevID: snapshot.DevID,
		}
		result.Snapshots[uuid] = s
	}
	return &result
}

func (d *Driver) ListVolume(id, snapshotID string) error {
	volumes := api.DeviceMapperVolumes{
		Volumes: make(map[string]api.DeviceMapperVolume),
	}
	if id != "" {
		volume := d.loadVolume(id)
		if volume == nil {
			return fmt.Errorf("volume %v doesn't exists", id)
		}
		if snapshotID != "" {
			volumes.Volumes[id] = *getVolumeSnapshotInfo(id, volume, snapshotID)
		} else {
			volumes.Volumes[id] = *getVolumeInfo(id, volume)
		}

	} else {
		volumeIDs := d.listVolumeIDs()
		for _, uuid := range volumeIDs {
			volume := d.loadVolume(uuid)
			if volume == nil {
				return fmt.Errorf("Volume list changed, for volume %v", uuid)
			}
			volumes.Volumes[uuid] = *getVolumeInfo(uuid, volume)
		}
	}
	api.ResponseOutput(volumes)
	return nil
}

func (d *Driver) CreateSnapshot(id, volumeID string) error {
	volume := d.loadVolume(volumeID)
	if volume == nil {
		return fmt.Errorf("Cannot find volume with uuid %v", volumeID)
	}
	devID := d.LastDevID

	snapshot, exists := volume.Snapshots[id]
	if exists {
		return fmt.Errorf("Already has snapshot with uuid %v", id)
	}

	log.WithFields(logrus.Fields{
		LOG_FIELD_REASON:            LOG_REASON_START,
		LOG_FIELD_EVENT:             LOG_EVENT_CREATE,
		LOG_FIELD_VOLUME:            volumeID,
		LOG_FIELD_SNAPSHOT:          id,
		DM_LOG_FIELD_VOLUME_DEVID:   volume.DevID,
		DM_LOG_FIELD_SNAPSHOT_DEVID: devID,
	}).Debugf("Creating snapshot")
	err := devicemapper.CreateSnapDevice(d.ThinpoolDevice, devID, volumeID, volume.DevID)
	if err != nil {
		return err
	}
	log.Debugf("Created snapshot device")

	snapshot = Snapshot{
		DevID:     devID,
		Activated: false,
	}
	volume.Snapshots[id] = snapshot
	d.LastDevID++

	if err := d.saveVolume(volume); err != nil {
		return err
	}
	if err := util.SaveConfig(d.root, d.configName, d.Device); err != nil {
		return err
	}
	log.WithFields(logrus.Fields{
		LOG_FIELD_REASON:   LOG_REASON_COMPLETE,
		LOG_FIELD_EVENT:    LOG_EVENT_CREATE,
		LOG_FIELD_VOLUME:   volumeID,
		LOG_FIELD_SNAPSHOT: id,
	}).Debugf("Created snapshot")
	return nil
}

func (d *Driver) DeleteSnapshot(id, volumeID string) error {
	snapshot, volume, err := d.getSnapshotAndVolume(id, volumeID)
	if err != nil {
		return err
	}

	log.WithFields(logrus.Fields{
		LOG_FIELD_REASON:   LOG_REASON_START,
		LOG_FIELD_EVENT:    LOG_EVENT_REMOVE,
		LOG_FIELD_SNAPSHOT: id,
		LOG_FIELD_VOLUME:   volumeID,
	}).Debugf("Deleting snapshot for volume")
	err = devicemapper.DeleteDevice(d.ThinpoolDevice, snapshot.DevID)
	if err != nil {
		return err
	}
	log.Debug("Deleted snapshot device")
	delete(volume.Snapshots, id)

	if err = d.saveVolume(volume); err != nil {
		return err
	}
	log.WithFields(logrus.Fields{
		LOG_FIELD_REASON:   LOG_REASON_COMPLETE,
		LOG_FIELD_EVENT:    LOG_EVENT_REMOVE,
		LOG_FIELD_SNAPSHOT: id,
		LOG_FIELD_VOLUME:   volumeID,
	}).Debugf("Deleted snapshot for volume")
	return nil
}

func (d *Driver) CompareSnapshot(id, compareID, volumeID string) (*metadata.Mappings, error) {
	includeSame := false
	if compareID == "" || compareID == id {
		compareID = id
		includeSame = true
	}
	snap1, _, err := d.getSnapshotAndVolume(id, volumeID)
	if err != nil {
		return nil, err
	}
	snap2, _, err := d.getSnapshotAndVolume(compareID, volumeID)
	if err != nil {
		return nil, err
	}

	dev := d.MetadataDevice
	out, err := exec.Command("pdata_tools", "thin_delta",
		"--snap1", strconv.Itoa(snap1.DevID), "--snap2", strconv.Itoa(snap2.DevID), dev).Output()
	if err != nil {
		return nil, err
	}
	mapping, err := metadata.DeviceMapperThinDeltaParser(out, d.ThinpoolBlockSize*SECTOR_SIZE, includeSame)
	if err != nil {
		return nil, err
	}
	return mapping, err
}

func (d *Driver) Info() error {
	// from sector count to byte
	blockSize := d.ThinpoolBlockSize * 512

	dmInfo := api.DeviceMapperInfo{
		Driver:            d.Name(),
		Root:              d.Root,
		DataDevice:        d.DataDevice,
		MetadataDevice:    d.MetadataDevice,
		ThinpoolDevice:    d.ThinpoolDevice,
		ThinpoolSize:      d.ThinpoolSize,
		ThinpoolBlockSize: blockSize,
	}

	api.ResponseOutput(dmInfo)

	return nil
}

func (d *Driver) getSnapshotAndVolume(snapshotID, volumeID string) (*Snapshot, *Volume, error) {
	volume := d.loadVolume(volumeID)
	if volume == nil {
		return nil, nil, fmt.Errorf("cannot find volume %v", volumeID)
	}
	snap, exists := volume.Snapshots[snapshotID]
	if !exists {
		return nil, nil, fmt.Errorf("cannot find snapshot %v of volume %v", snapshotID, volumeID)
	}
	return &snap, volume, nil
}

func (d *Driver) OpenSnapshot(id, volumeID string) error {
	snapshot, volume, err := d.getSnapshotAndVolume(id, volumeID)
	if err != nil {
		return err
	}

	if err = devicemapper.ActivateDevice(d.ThinpoolDevice, id, snapshot.DevID, uint64(volume.Size)); err != nil {
		return err
	}
	snapshot.Activated = true

	return d.saveVolume(volume)
}

func (d *Driver) CloseSnapshot(id, volumeID string) error {
	snapshot, volume, err := d.getSnapshotAndVolume(id, volumeID)
	if err != nil {
		return err
	}

	if err := devicemapper.RemoveDevice(id); err != nil {
		return err
	}
	snapshot.Activated = false

	return d.saveVolume(volume)
}

func (d *Driver) ReadSnapshot(id, volumeID string, offset int64, data []byte) error {
	_, _, err := d.getSnapshotAndVolume(id, volumeID)
	if err != nil {
		return err
	}

	dev := filepath.Join(DM_DIR, id)
	devFile, err := os.Open(dev)
	if err != nil {
		return err
	}
	defer devFile.Close()

	if _, err = devFile.ReadAt(data, offset); err != nil {
		return err
	}

	return nil
}

func (d *Driver) GetVolumeDevice(id string) (string, error) {
	volume := d.loadVolume(id)
	if volume == nil {
		return "", fmt.Errorf("Volume with uuid %v doesn't exist", id)
	}

	return filepath.Join(DM_DIR, id), nil
}

func (d *Driver) HasSnapshot(id, volumeID string) bool {
	_, _, err := d.getSnapshotAndVolume(id, volumeID)
	if err != nil {
		return false
	}
	return true
}

func getImageCfgName(uuid string) (string, error) {
	if uuid == "" {
		return "", fmt.Errorf("Invalid image UUID specified: %v", uuid)
	}
	return IMAGE_CFG_PREFIX + uuid + DEVMAPPER_CFG_POSTFIX, nil
}

func (device *Device) loadImage(uuid string) *Image {
	if uuid == "" {
		return nil
	}
	cfgName, err := getImageCfgName(uuid)
	if err != nil {
		return nil
	}
	if !util.ConfigExists(device.Root, cfgName) {
		return nil
	}
	image := &Image{}
	if err := util.LoadConfig(device.Root, cfgName, image); err != nil {
		log.Error("Failed to load volume json ", cfgName)
		return nil
	}
	return image
}

func (device *Device) saveImage(image *Image) error {
	uuid := image.UUID
	cfgName, err := getImageCfgName(uuid)
	if err != nil {
		return err
	}
	return util.SaveConfig(device.Root, cfgName, image)
}

func (device *Device) deleteImage(uuid string) error {
	cfgName, err := getImageCfgName(uuid)
	if err != nil {
		return err
	}
	return util.RemoveConfig(device.Root, cfgName)
}

func (d *Driver) ActivateImage(imageUUID, imageFile string) error {
	image := d.loadImage(imageUUID)
	if image != nil {
		return fmt.Errorf("Image %v already activated by driver %v", imageUUID, DRIVER_NAME)
	}
	st, err := os.Stat(imageFile)
	if err != nil || st.IsDir() {
		return err
	}
	log.Debug("Found ", imageFile)
	loopDev, err := util.AttachLoopbackDevice(imageFile, true)
	if err != nil {
		return err
	}
	log.Debugf("Attached %v to %v", imageFile, loopDev)
	image = &Image{
		UUID:      imageUUID,
		Size:      st.Size(),
		FilePath:  imageFile,
		Device:    loopDev,
		VolumeRef: make(map[string]bool),
	}
	if err := d.saveImage(image); err != nil {
		return err
	}
	log.Debug("Activated image ", imageUUID)
	return nil
}

func (d *Driver) DeactivateImage(imageUUID string) error {
	image := d.loadImage(imageUUID)
	if image == nil {
		return fmt.Errorf("Cannot find image %v by driver %v", imageUUID, DRIVER_NAME)
	}
	for volumeUUID := range image.VolumeRef {
		if volume := d.loadVolume(volumeUUID); volume != nil {
			return fmt.Errorf("Volume %v hasn't been removed yet", volume)
		}
	}
	if err := util.DetachLoopbackDevice(image.FilePath, image.Device); err != nil {
		return err
	}
	log.Debugf("Detached %v from %v", image.FilePath, image.Device)
	if err := d.deleteImage(imageUUID); err != nil {
		return err
	}
	log.Debug("Deactivated image ", imageUUID)
	return nil
}
