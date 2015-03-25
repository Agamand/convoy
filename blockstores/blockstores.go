package blockstores

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/yasker/volmgr/drivers"
	"github.com/yasker/volmgr/metadata"
	"github.com/yasker/volmgr/utils"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	BLOCKSTORE_BASE        = "rancher-blockstore"
	VOLUME_DIRECTORY       = "volume"
	VOLUME_CONFIG_FILE     = "volume.cfg"
	VOLUME_SEPARATE_LAYER1 = 2
	VOLUME_SEPARATE_LAYER2 = 4
	SNAPSHOTS_DIRECTORY    = "snapshots"
	SNAPSHOT_CONFIG_PREFIX = "snapshot_"
	BLOCKS_DIRECTORY       = "blocks"
	BLOCK_SEPARATE_LAYER1  = 2
	BLOCK_SEPARATE_LAYER2  = 4
	DEFAULT_BLOCK_SIZE     = 2097152
)

type InitFunc func(configFile, id string, config map[string]string) (BlockStoreDriver, error)

type BlockStoreDriver interface {
	Kind() string
	FileExists(path, fileName string) bool
	FileSize(path, fileName string) int64
	MkDirAll(dirName string) error
	RemoveAll(name string) error
	Read(srcPath, srcFileName string, data []byte) error
	Write(data []byte, dstPath, dstFileName string) error
	List(path string) ([]string, error)
	CopyToPath(srcFileName string, path string) error
}

type Volume struct {
	Size           uint64
	Base           string
	LastSnapshotId string
}

type BlockStore struct {
	Kind      string
	BlockSize int64
}

type BlockMapping struct {
	Offset        int64
	BlockChecksum string
}

type SnapshotMap struct {
	Id     string
	Blocks []BlockMapping
}

var (
	initializers map[string]InitFunc
)

func init() {
	initializers = make(map[string]InitFunc)
}

func RegisterDriver(kind string, initFunc InitFunc) error {
	if _, exists := initializers[kind]; exists {
		return fmt.Errorf("%s has already been registered", kind)
	}
	initializers[kind] = initFunc
	return nil
}

func GetBlockStoreDriver(kind, configFile, id string, config map[string]string) (BlockStoreDriver, error) {
	if _, exists := initializers[kind]; !exists {
		return nil, fmt.Errorf("Driver %v is not supported!", kind)
	}
	return initializers[kind](configFile, id, config)
}

func getDriverConfigFilename(root, kind, id string) string {
	return filepath.Join(root, id+"-"+kind+".cfg")
}

func getConfigFilename(root, id string) string {
	return filepath.Join(root, id+".cfg")
}

func loadBlockStoreConfig(path, name string, driver BlockStoreDriver, v interface{}) error {
	size := driver.FileSize(path, name)
	if size < 0 {
		return fmt.Errorf("cannot find %v/%v in blockstore", path, name)
	}
	data := make([]byte, size)
	if err := driver.Read(path, name, data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return err
	}
	return nil
}

func saveBlockStoreConfig(path, name string, driver BlockStoreDriver, v interface{}) error {
	j, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := driver.Write(j, path, name); err != nil {
		return err
	}
	return nil
}

func loadVolumeConfig(volumeId string, driver BlockStoreDriver) (*Volume, error) {
	v := Volume{}
	volumePath := getVolumePath(volumeId)
	volumeCfg := VOLUME_CONFIG_FILE
	if err := loadBlockStoreConfig(volumePath, volumeCfg, driver, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func saveVolumeConfig(volumeId string, driver BlockStoreDriver, v *Volume) error {
	volumePath := getVolumePath(volumeId)
	volumeCfg := VOLUME_CONFIG_FILE
	if err := saveBlockStoreConfig(volumePath, volumeCfg, driver, v); err != nil {
		return err
	}
	return nil
}

func Register(root, kind, id string, config map[string]string) error {
	configFile := getDriverConfigFilename(root, kind, id)
	if _, err := os.Stat(configFile); err == nil {
		return fmt.Errorf("BlockStore %v is already registered", id)
	}
	driver, err := GetBlockStoreDriver(kind, configFile, id, config)
	if err != nil {
		return err
	}
	log.Debug("Created ", configFile)

	basePath := filepath.Join(BLOCKSTORE_BASE, VOLUME_DIRECTORY)
	err = driver.MkDirAll(basePath)
	if err != nil {
		removeDriverConfigFile(root, kind, id)
		return err
	}
	log.Debug("Created base directory of blockstore at ", basePath)

	bs := &BlockStore{
		Kind:      kind,
		BlockSize: DEFAULT_BLOCK_SIZE,
	}
	configFile = getConfigFilename(root, id)
	if err := utils.SaveConfig(configFile, bs); err != nil {
		return err
	}
	log.Debug("Created ", configFile)
	return nil
}

func removeDriverConfigFile(root, kind, id string) error {
	configFile := getDriverConfigFilename(root, kind, id)
	if err := exec.Command("rm", "-f", configFile).Run(); err != nil {
		return err
	}
	log.Debug("Removed ", configFile)
	return nil
}

func removeConfigFile(root, id string) error {
	configFile := getConfigFilename(root, id)
	if err := exec.Command("rm", "-f", configFile).Run(); err != nil {
		return err
	}
	log.Debug("Removed ", configFile)
	return nil
}

func Deregister(root, kind, id string) error {
	err := removeDriverConfigFile(root, kind, id)
	if err != nil {
		return err
	}
	err = removeConfigFile(root, id)
	if err != nil {
		return err
	}
	return nil
}

func AddVolume(root, id, volumeId, base string, size uint64) error {
	configFile := getConfigFilename(root, id)
	b := &BlockStore{}
	err := utils.LoadConfig(configFile, b)
	if err != nil {
		return err
	}

	driverConfigFile := getDriverConfigFilename(root, b.Kind, id)
	driver, err := GetBlockStoreDriver(b.Kind, driverConfigFile, id, nil)
	if err != nil {
		return err
	}

	volumePath := getVolumePath(volumeId)
	volumeCfg := VOLUME_CONFIG_FILE
	if driver.FileExists(volumePath, volumeCfg) {
		return fmt.Errorf("volume %v already exists in blockstore %v", volumeId, id)
	}

	if err := driver.MkDirAll(volumePath); err != nil {
		return err
	}
	if err := driver.MkDirAll(getSnapshotsPath(volumeId)); err != nil {
		return err
	}
	if err := driver.MkDirAll(getBlocksPath(volumeId)); err != nil {
		return err
	}
	log.Debug("Created volume directory")
	volume := Volume{
		Size:           size,
		Base:           base,
		LastSnapshotId: "",
	}

	if err := saveBlockStoreConfig(volumePath, volumeCfg, driver, &volume); err != nil {
		return err
	}
	log.Debug("Created volume configuration file in blockstore done: ", filepath.Join(volumePath, volumeCfg))

	return nil
}

func RemoveVolume(root, id, volumeId string) error {
	configFile := getConfigFilename(root, id)
	b := &BlockStore{}
	err := utils.LoadConfig(configFile, b)
	if err != nil {
		return err
	}

	driverConfigFile := getDriverConfigFilename(root, b.Kind, id)
	driver, err := GetBlockStoreDriver(b.Kind, driverConfigFile, id, nil)
	if err != nil {
		return err
	}

	volumePath := getVolumePath(volumeId)
	volumeCfg := VOLUME_CONFIG_FILE
	if !driver.FileExists(volumePath, volumeCfg) {
		return fmt.Errorf("volume %v doesn't exist in blockstore %v", volumeId, id)
	}

	volumeDir := getVolumePath(volumeId)
	err = driver.RemoveAll(volumeDir)
	if err != nil {
		return err
	}
	log.Debug("Removed volume directory in blockstore: ", volumeDir)

	return nil
}

func getVolumePath(volumeId string) string {
	volumeLayer1 := volumeId[0:VOLUME_SEPARATE_LAYER1]
	volumeLayer2 := volumeId[VOLUME_SEPARATE_LAYER1:VOLUME_SEPARATE_LAYER2]
	return filepath.Join(BLOCKSTORE_BASE, VOLUME_DIRECTORY, volumeLayer1, volumeLayer2, volumeId)
}

func getSnapshotsPath(volumeId string) string {
	return filepath.Join(getVolumePath(volumeId), SNAPSHOTS_DIRECTORY)
}

func getBlocksPath(volumeId string) string {
	return filepath.Join(getVolumePath(volumeId), BLOCKS_DIRECTORY)
}

func getBlockPathAndFileName(volumeId, checksum string) (string, string) {
	blockSubDirLayer1 := checksum[0:BLOCK_SEPARATE_LAYER1]
	blockSubDirLayer2 := checksum[BLOCK_SEPARATE_LAYER1:BLOCK_SEPARATE_LAYER2]
	path := filepath.Join(getBlocksPath(volumeId), blockSubDirLayer1, blockSubDirLayer2)
	fileName := checksum + ".blk"

	return path, fileName
}

func getSnapshotConfigName(id string) string {
	return SNAPSHOT_CONFIG_PREFIX + id + ".cfg"
}

func BackupSnapshot(root, snapshotId, volumeId, blockstoreId string, sDriver drivers.Driver) error {
	configFile := getConfigFilename(root, blockstoreId)
	b := &BlockStore{}
	err := utils.LoadConfig(configFile, b)
	if err != nil {
		return err
	}
	driverConfigFile := getDriverConfigFilename(root, b.Kind, blockstoreId)
	bsDriver, err := GetBlockStoreDriver(b.Kind, driverConfigFile, blockstoreId, nil)
	if err != nil {
		return err
	}

	volume, err := loadVolumeConfig(volumeId, bsDriver)
	if err != nil {
		return err
	}

	lastSnapshotId := volume.LastSnapshotId
	var lastSnapshotMap *SnapshotMap
	//We'd better check last snapshot config early, ensure it would go through
	if lastSnapshotId != "" && lastSnapshotId != snapshotId {
		log.Debug("Loading last snapshot", lastSnapshotId)
		lastSnapshotMap, err = loadSnapshotMap(lastSnapshotId, volumeId, bsDriver)
		if err != nil {
			return err
		}
		log.Debug("Loaded last snapshot", lastSnapshotId)
	} else {
		//Generate full snapshot if the snapshot has been backed up last time
		lastSnapshotId = ""
		log.Debug("Would create full snapshot metadata")
	}

	log.Debug("Generating snapshot metadata of", snapshotId)
	delta := metadata.Mappings{}
	if err = sDriver.CompareSnapshot(snapshotId, lastSnapshotId, volumeId, &delta); err != nil {
		return err
	}
	if delta.BlockSize != b.BlockSize {
		return fmt.Errorf("Currently doesn't support different block sizes between blockstore and driver")
	}
	log.Debug("Generated snapshot metadata of", snapshotId)

	log.Debug("Creating snapshot changed blocks of ", snapshotId)
	snapshotDeltaMap := &SnapshotMap{
		Blocks: []BlockMapping{},
	}
	if err := sDriver.OpenSnapshot(snapshotId, volumeId); err != nil {
		return err
	}
	defer sDriver.CloseSnapshot(snapshotId, volumeId)
	for _, d := range delta.Mappings {
		block := make([]byte, b.BlockSize)
		for i := int64(0); i < d.Size/delta.BlockSize; i++ {
			offset := d.Offset + i*delta.BlockSize
			err := sDriver.ReadSnapshot(snapshotId, volumeId, offset, block)
			if err != nil {
				return err
			}
			checksum := utils.GetChecksum(block)
			path, fileName := getBlockPathAndFileName(volumeId, checksum)
			if bsDriver.FileSize(path, fileName) >= 0 {
				blockMapping := BlockMapping{
					Offset:        offset,
					BlockChecksum: checksum,
				}
				snapshotDeltaMap.Blocks = append(snapshotDeltaMap.Blocks, blockMapping)
				log.Debugf("Found existed block match at %v/%v", path, fileName)
				continue
			}
			log.Debugf("Creating new block file at %v/%v", path, fileName)
			if err := bsDriver.MkDirAll(path); err != nil {
				return err
			}
			if err := bsDriver.Write(block, path, fileName); err != nil {
				return err
			}
			log.Debugf("Created new block file at %v/%v", path, fileName)

			blockMapping := BlockMapping{
				Offset:        offset,
				BlockChecksum: checksum,
			}
			snapshotDeltaMap.Blocks = append(snapshotDeltaMap.Blocks, blockMapping)
		}
	}
	log.Debug("Created snapshot changed blocks of", snapshotId)

	snapshotMap := mergeSnapshotMap(snapshotId, snapshotDeltaMap, lastSnapshotMap)

	if err := saveSnapshotMap(snapshotId, volumeId, bsDriver, snapshotMap); err != nil {
		return err
	}
	log.Debug("Created snapshot config of", snapshotId)
	volume.LastSnapshotId = snapshotId
	if err := saveVolumeConfig(volumeId, bsDriver, volume); err != nil {
		return err
	}

	return nil
}

func loadSnapshotMap(snapshotId, volumeId string, bsDriver BlockStoreDriver) (*SnapshotMap, error) {
	snapshotMap := SnapshotMap{}
	path := getSnapshotsPath(volumeId)
	fileName := getSnapshotConfigName(snapshotId)

	if err := loadBlockStoreConfig(path, fileName, bsDriver, &snapshotMap); err != nil {
		return nil, err
	}
	return &snapshotMap, nil
}

func saveSnapshotMap(snapshotId, volumeId string, bsDriver BlockStoreDriver, snapshotMap *SnapshotMap) error {
	path := getSnapshotsPath(volumeId)
	fileName := getSnapshotConfigName(snapshotId)
	if bsDriver.FileExists(path, fileName) {
		file := filepath.Join(path, fileName)
		log.Warnf("Snapshot configuration file %v already exists, would remove it\n", file)
		if err := bsDriver.RemoveAll(file); err != nil {
			return err
		}
	}
	if err := saveBlockStoreConfig(path, fileName, bsDriver, snapshotMap); err != nil {
		return err
	}
	return nil
}

func mergeSnapshotMap(snapshotId string, deltaMap, lastMap *SnapshotMap) *SnapshotMap {
	if lastMap == nil {
		deltaMap.Id = snapshotId
		return deltaMap
	}
	sMap := &SnapshotMap{
		Id:     snapshotId,
		Blocks: []BlockMapping{},
	}
	var d, l int
	for d, l = 0, 0; d < len(deltaMap.Blocks) && l < len(lastMap.Blocks); {
		dB := deltaMap.Blocks[d]
		lB := lastMap.Blocks[l]
		if dB.Offset == lB.Offset {
			sMap.Blocks = append(sMap.Blocks, dB)
			d++
			l++
		} else if dB.Offset < lB.Offset {
			sMap.Blocks = append(sMap.Blocks, dB)
			d++
		} else {
			//dB.Offset > lB.offset
			sMap.Blocks = append(sMap.Blocks, lB)
			l++
		}
	}

	if d == len(deltaMap.Blocks) {
		sMap.Blocks = append(sMap.Blocks, lastMap.Blocks[l:]...)
	} else {
		sMap.Blocks = append(sMap.Blocks, deltaMap.Blocks[d:]...)
	}

	return sMap
}

func RestoreSnapshot(root, srcSnapshotId, srcVolumeId, dstVolumeId, blockstoreId string, sDriver drivers.Driver) error {
	configFile := getConfigFilename(root, blockstoreId)
	b := &BlockStore{}
	err := utils.LoadConfig(configFile, b)
	if err != nil {
		return err
	}
	driverConfigFile := getDriverConfigFilename(root, b.Kind, blockstoreId)
	bsDriver, err := GetBlockStoreDriver(b.Kind, driverConfigFile, blockstoreId, nil)
	if err != nil {
		return err
	}

	if _, err := loadVolumeConfig(srcVolumeId, bsDriver); err != nil {
		return fmt.Errorf("volume %v doesn't exist in blockstore %v", srcVolumeId, blockstoreId, err)
	}

	volDevName, err := sDriver.GetVolumeDevice(dstVolumeId)
	if err != nil {
		return err
	}
	volDev, err := os.Create(volDevName)
	if err != nil {
		return err
	}
	defer volDev.Close()

	snapshotMap, err := loadSnapshotMap(srcSnapshotId, srcVolumeId, bsDriver)
	if err != nil {
		return err
	}

	for _, block := range snapshotMap.Blocks {
		data := make([]byte, b.BlockSize)
		path, file := getBlockPathAndFileName(srcVolumeId, block.BlockChecksum)
		err := bsDriver.Read(path, file, data)
		if err != nil {
			return err
		}
		if _, err := volDev.WriteAt(data, block.Offset); err != nil {
			return err
		}
	}

	return nil
}

func RemoveSnapshot(root, snapshotId, volumeId, blockstoreId string) error {
	configFile := getConfigFilename(root, blockstoreId)
	b := &BlockStore{}
	err := utils.LoadConfig(configFile, b)
	if err != nil {
		return err
	}
	driverConfigFile := getDriverConfigFilename(root, b.Kind, blockstoreId)
	bsDriver, err := GetBlockStoreDriver(b.Kind, driverConfigFile, blockstoreId, nil)
	if err != nil {
		return err
	}

	v, err := loadVolumeConfig(volumeId, bsDriver)
	if err != nil {
		return fmt.Errorf("cannot find volume %v in blockstore %v", volumeId, blockstoreId, err)
	}

	snapshotMap, err := loadSnapshotMap(snapshotId, volumeId, bsDriver)
	if err != nil {
		return err
	}
	discardBlockSet := make(map[string]bool)
	for _, blk := range snapshotMap.Blocks {
		discardBlockSet[blk.BlockChecksum] = true
	}
	discardBlockCounts := len(discardBlockSet)

	snapshotPath := getSnapshotsPath(volumeId)
	snapshotFile := getSnapshotConfigName(snapshotId)
	discardFile := filepath.Join(snapshotPath, snapshotFile)
	if err := bsDriver.RemoveAll(discardFile); err != nil {
		return err
	}
	log.Debugf("Removed snapshot config file %v on blockstore", discardFile)

	if snapshotId == v.LastSnapshotId {
		v.LastSnapshotId = ""
		if err := saveVolumeConfig(volumeId, bsDriver, v); err != nil {
			return err
		}
	}

	log.Debug("GC started")
	snapshotsList, err := getSnapshotsList(volumeId, bsDriver)
	if err != nil {
		return err
	}
	for _, snapshotId := range snapshotsList {
		snapshotMap, err := loadSnapshotMap(snapshotId, volumeId, bsDriver)
		if err != nil {
			return err
		}
		for _, blk := range snapshotMap.Blocks {
			if _, exists := discardBlockSet[blk.BlockChecksum]; exists {
				delete(discardBlockSet, blk.BlockChecksum)
				discardBlockCounts--
				if discardBlockCounts == 0 {
					break
				}
			}
		}
		if discardBlockCounts == 0 {
			break
		}
	}

	for blk, _ := range discardBlockSet {
		path, file := getBlockPathAndFileName(volumeId, blk)
		if err := bsDriver.RemoveAll(filepath.Join(path, file)); err != nil {
			return err
		}
		log.Debugf("Removed unused block %v for volume %v", blk, volumeId)
	}

	log.Debug("GC completed")

	return nil
}

func getSnapshotsList(volumeId string, driver BlockStoreDriver) ([]string, error) {
	fileList, err := driver.List(getSnapshotsPath(volumeId))
	if err != nil {
		return nil, err
	}

	var result []string
	for _, f := range fileList {
		parts := strings.Split(f, "_")
		if len(parts) != 2 {
			return nil, fmt.Errorf("incorrect filename format:", f)
		}
		parts = strings.Split(parts[1], ".")
		if len(parts) != 2 {
			return nil, fmt.Errorf("incorrect filename format:", f)
		}
		result = append(result, parts[0])
	}
	return result, nil
}
