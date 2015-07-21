package api

type VolumeMountConfig struct {
	MountPoint string
}

type VolumeCreateConfig struct {
	Name      string
	Size      int64
	BackupURL string
}

type BackupListConfig struct {
	URL          string
	VolumeUUID   string
	SnapshotUUID string
}

type BackupCreateConfig struct {
	URL          string
	SnapshotUUID string
}

type BackupDeleteConfig struct {
	URL string
}
