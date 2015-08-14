#!/usr/bin/python

import subprocess
import os
import json
import pytest
import uuid
import time
import sys
import threading
import shutil

from rancher_volume import VolumeManager

TEST_ROOT = "/tmp/rancher-volume_test/"
CFG_ROOT = os.path.join(TEST_ROOT, "rancher-volume")
MOUNT_ROOT = os.path.join(TEST_ROOT, "mount")
PID_FILE = os.path.join(TEST_ROOT, "rancher-volume.pid")
LOG_FILE= os.path.join(TEST_ROOT, "rancher-volume.log")
TEST_SNAPSHOT_FILE = "snapshot.test"

DM = "devicemapper"
DM_ROOT = os.path.join(CFG_ROOT, DM)

TEST_THREAD_COUNT = 100
TEST_LOOP_COUNT = 100

VFS_DEST = "vfs://" + TEST_ROOT

VFS = "vfs"
VFS_ROOT = os.path.join(CFG_ROOT, VFS)
VFS_VOLUME_PATH = os.path.join(TEST_ROOT, "vfs-volumes")

ENV_TEST_AWS_ACCESS_KEY = "RANCHER_TEST_AWS_ACCESS_KEY_ID"
ENV_TEST_AWS_SECRET_KEY = "RANCHER_TEST_AWS_SECRET_ACCESS_KEY"
ENV_TEST_AWS_REGION     = "RANCHER_TEST_AWS_REGION"
ENV_TEST_AWS_BUCKET     = "RANCHER_TEST_AWS_BUCKET"
S3_PATH = "test/volume/"

DD_BLOCK_SIZE = 4096
POOL_NAME = "rancher_volume_test_pool"
RANCHER_VOLUME_BINARY = os.path.abspath("../../bin/rancher-volume")

DATA_FILE = "data.vol"
METADATA_FILE = "metadata.vol"
DATA_DEVICE_SIZE = 2147483648
METADATA_DEVICE_SIZE = 52428800
DM_DIR = "/dev/mapper"
DM_BLOCK_SIZE = 2097152
EMPTY_FILE_SIZE = 104857600

DEFAULT_VOLUME_SIZE = "107374182400"
VOLUME_SIZE_500M_Bytes = "524288000"
VOLUME_SIZE_500M = "500M"
VOLUME_SIZE_100M = "104857600"
VOLUME_SIZE_6M = "6M"

RANDOM_VALID_UUID = "0bd0bc5f-f3ad-4e1b-9283-98adb3ef38f4"

data_dev = ""
metadata_dev = ""

mount_cleanup_list = []
dm_cleanup_list = []

def create_empty_file(filepath, size):
    subprocess.check_call(["truncate", "-s", str(size), filepath])
    assert os.path.exists(filepath)

def attach_loopback_dev(filepath):
    dev = subprocess.check_output(["losetup", "-v", "-f",
            filepath]).strip().split(" ")[3]
    assert dev.startswith("/dev/loop")
    return dev

def detach_loopback_dev(dev):
    subprocess.check_output(["losetup", "-d", dev])

def format_dev(dev):
    subprocess.check_call(["mkfs", "-t", "ext4", dev])

def mount_dev(dev, mountpoint):
    subprocess.check_call(["mount", dev, mountpoint])
    mount_cleanup_list.append(mountpoint)

def umount_dev(mountpoint):
    subprocess.check_call(["umount", mountpoint])
    mount_cleanup_list.remove(mountpoint)

def setup_module():
    if os.path.exists(TEST_ROOT):
	subprocess.check_call(["rm", "-rf", TEST_ROOT])

    os.makedirs(TEST_ROOT)
    assert os.path.exists(TEST_ROOT)

    os.makedirs(MOUNT_ROOT)
    assert os.path.exists(MOUNT_ROOT)

    data_file = os.path.join(TEST_ROOT, DATA_FILE)
    create_empty_file(data_file, DATA_DEVICE_SIZE)
    global data_dev
    data_dev = attach_loopback_dev(data_file)

    metadata_file = os.path.join(TEST_ROOT, METADATA_FILE)
    create_empty_file(metadata_file, METADATA_DEVICE_SIZE)
    global metadata_dev
    metadata_dev = attach_loopback_dev(metadata_file)

    global v
    v = VolumeManager(RANCHER_VOLUME_BINARY, TEST_ROOT)
    v.start_server(PID_FILE, ["server",
        "--root", CFG_ROOT,
        "--log", LOG_FILE,
        "--drivers=" + DM,
        "--driver-opts", "dm.datadev=" + data_dev,
	"--driver-opts", "dm.metadatadev=" + metadata_dev,
	"--driver-opts", "dm.thinpoolname=" + POOL_NAME,
        "--drivers=" + VFS,
        "--driver-opts", "vfs.path=" + VFS_VOLUME_PATH])
    dm_cleanup_list.append(POOL_NAME)
    wait_for_daemon()

def detach_all_lodev(keyword):
    output = subprocess.check_output(["losetup", "-a"])
    lines = output.splitlines()
    for line in lines:
        if line.find(keyword) != -1:
            detach_loopback_dev(line.split(":")[0].strip())

def teardown_module():
    code = v.stop_server(PID_FILE)
    if code != 0:
        print "Something wrong when tearing down, continuing with code ", code

    while mount_cleanup_list:
	code = subprocess.call(["umount", mount_cleanup_list.pop()])
        if code != 0:
            print "Something wrong when tearing down, continuing with code", code

    while dm_cleanup_list:
	code = subprocess.call(["dmsetup", "remove", "--retry", dm_cleanup_list.pop()])
        if code != 0:
            print "Something wrong when tearing down, continuing with code ", code

    code = subprocess.call(["losetup", "-d", data_dev, metadata_dev])
    if code != 0:
        print "Something wrong when tearing down, continuing with code", code

    detach_all_lodev(TEST_ROOT)

def wait_for_daemon():
    while True:
        try:
                data = v.server_info()
                break
        except subprocess.CalledProcessError:
                print "Fail to communicate with daemon"
                if v.check_server(PID_FILE) != 0:
                    print "Server failed to start"
                    teardown_module()
                    assert False
                time.sleep(1)

    info = json.loads(data)
    success = True
    try:
        success = bool(success and DM in info["General"]["DriverList"])
        success = bool(success and VFS in info["General"]["DriverList"])
        success = bool(success and info["General"]["Root"] == CFG_ROOT)
        success = bool(success and info["General"]["DefaultDriver"] == DM)
        success = bool(success and info[DM]["Driver"] == "devicemapper")
        success = bool(success and info[DM]["Root"] == DM_ROOT)
        success = bool(success and info[DM]["DataDevice"] == data_dev)
        success = bool(success and info[DM]["MetadataDevice"] == metadata_dev)
        success = bool(success and info[DM]["ThinpoolDevice"] == os.path.join(DM_DIR, POOL_NAME))
        success = bool(success and info[DM]["ThinpoolSize"] == str(DATA_DEVICE_SIZE))
        success = bool(success and info[DM]["ThinpoolBlockSize"] == str(DM_BLOCK_SIZE))
        success = bool(success and info[VFS]["Root"] == VFS_ROOT)
        success = bool(success and info[VFS]["Path"] == VFS_VOLUME_PATH)
    except:
        success = False

    if not success:
        teardown_module()
        assert False

@pytest.yield_fixture(autouse=True)
def check_test():
    yield
    filenames = os.listdir(CFG_ROOT)
    for filename in filenames:
        assert not filename.startswith('volume')

def create_volume(size = "", name = "", backup = "", driver = ""):
    uuid = v.create_volume(size, name, backup, driver)
    dm_cleanup_list.append(uuid)
    return uuid

def delete_volume(uuid, name = ""):
    if name == "":
        v.delete_volume(uuid)
    else:
        v.delete_volume(name)
    dm_cleanup_list.remove(uuid)

def mount_volume_with_path(uuid):
    mount_dir = v.mount_volume_with_path(uuid)
    mount_cleanup_list.append(mount_dir)
    return mount_dir

def mount_volume(uuid):
    mount_dir = v.mount_volume(uuid)
    mount_cleanup_list.append(mount_dir)
    return mount_dir

def umount_volume(uuid, mount_dir):
    v.umount_volume(uuid)
    mount_cleanup_list.remove(mount_dir)

def test_volume_crud():
    volume_crud_test(DM)
    volume_crud_test(VFS, False)

def volume_crud_test(drv, sizeTest = True):
    uuid1 = create_volume(driver=drv)
    uuid2 = create_volume(driver=drv)

    if sizeTest:
        uuid3 = create_volume(VOLUME_SIZE_500M, driver=drv)
        uuid4 = create_volume(VOLUME_SIZE_100M, driver=drv)
        delete_volume(uuid4)
        delete_volume(uuid3)

    delete_volume(uuid2, uuid2[:6])
    delete_volume(uuid1)

def test_volume_name():
    volume_name_test(DM)
    volume_name_test(VFS)

def volume_name_test(drv):
    vol_name1 = "vol1"
    vol_name2 = "vol2"
    vol_uuid = create_volume(name=vol_name1, driver=drv)
    vols = v.list_volumes()
    assert vols[vol_uuid]["Name"] == vol_name1
    assert vols[vol_uuid]["Driver"] == drv
    assert vols[vol_uuid]["CreatedTime"] != ""

    with pytest.raises(subprocess.CalledProcessError):
        new_uuid = create_volume(name=vol_name1, driver=drv)

    with pytest.raises(subprocess.CalledProcessError):
        new_uuid = create_volume(driver="randomdriver")

    delete_volume(vol_uuid, vol_name1)
    vols = v.list_volumes()
    assert vol_uuid not in vols

    vol_uuid1 = create_volume(name=vol_name1, driver=drv)
    vol_uuid2 = create_volume(name=vol_name2, driver=drv)
    assert vol_uuid1 != vol_uuid

    vols = v.list_volumes()
    assert vols[vol_uuid1]["Name"] == vol_name1
    assert vols[vol_uuid2]["Name"] == vol_name2
    assert vols[vol_uuid1]["CreatedTime"] != ""
    assert vols[vol_uuid2]["CreatedTime"] != ""
    delete_volume(vol_uuid1, vol_name1)
    delete_volume(vol_uuid2, vol_name2)

def mount_volume_and_create_file(uuid, filename):
    # with format
    volume_mount_dir = mount_volume(uuid)

    test_file = os.path.join(volume_mount_dir,filename)
    with open(test_file, "w") as f:
	subprocess.check_call(["echo", "This is volume test file"], stdout=f)
    assert os.path.exists(test_file)

    umount_volume(uuid, volume_mount_dir)
    # Doesn't work with current VFS implmentation, since it won't really mount
    #assert not os.path.exists(test_file)

def test_volume_mount():
    volume_mount_test(DM)
    # skip the vfs mount test because we only pass the original volume path as
    # mount path, not really done any mount work now

def volume_mount_test(drv):
    uuid = create_volume(driver=drv)

    # with format
    filename = "test"
    mount_volume_and_create_file(uuid, filename)

    # without format
    volume_mount_dir = mount_volume_with_path(uuid)
    test_file = os.path.join(volume_mount_dir, filename)
    assert os.path.exists(test_file)

    umount_volume(uuid, volume_mount_dir)
    assert not os.path.exists(test_file)

    # auto mount
    volume_mount_dir = mount_volume(uuid)
    test_file = os.path.join(volume_mount_dir, filename)
    assert os.path.exists(test_file)

    umount_volume(uuid, volume_mount_dir)
    assert not os.path.exists(test_file)

    delete_volume(uuid)

def test_volume_list():
    volume_list_driver_test(DM)
    volume_list_driver_test(VFS, False)

def volume_list_driver_test(drv, check_size = True):
    volumes = v.list_volumes()
    assert len(volumes) == 0

    uuid1 = create_volume(driver=drv)
    uuid2 = create_volume(driver=drv)
    if check_size:
	uuid3 = create_volume(VOLUME_SIZE_500M, driver=drv)
	uuid4 = create_volume(VOLUME_SIZE_100M, driver=drv)

    volume = v.inspect_volume(uuid1)
    assert volume["UUID"] == uuid1
    if check_size:
	assert volume["Size"] == int(DEFAULT_VOLUME_SIZE)
    volume = v.inspect_volume(uuid2)
    assert volume["UUID"] == uuid2
    if check_size:
	assert volume["Size"] == int(DEFAULT_VOLUME_SIZE)

    if check_size:
        volumes = v.list_volumes()
        assert volumes[uuid1]["Size"] == int(DEFAULT_VOLUME_SIZE)
        assert volumes[uuid2]["Size"] == int(DEFAULT_VOLUME_SIZE)
        assert volumes[uuid3]["Size"] == int(VOLUME_SIZE_500M_Bytes)
        assert volumes[uuid4]["Size"] == int(VOLUME_SIZE_100M)

	delete_volume(uuid4)
	delete_volume(uuid3)

    delete_volume(uuid2)
    delete_volume(uuid1)

def test_snapshot_crud():
    snapshot_crud_test(DM)
    snapshot_crud_test(VFS)

def snapshot_crud_test(driver):
    volume_uuid = create_volume(VOLUME_SIZE_500M, name="vol1", driver=driver)

    snapshot_uuid = v.create_snapshot(volume_uuid)
    v.delete_snapshot(snapshot_uuid)

    delete_volume(volume_uuid)

    # delete snapshot automatically with volume
    volume_uuid = create_volume(VOLUME_SIZE_500M, name="vol1", driver=driver)
    snap1 = v.create_snapshot(volume_uuid)
    snap2 = v.create_snapshot(volume_uuid)
    snap3 = v.create_snapshot(volume_uuid)

    v.delete_snapshot(snap1)
    v.delete_snapshot(snap2[:6])
    delete_volume(volume_uuid)

    volume_uuid = create_volume(VOLUME_SIZE_500M, driver=driver)
    snap1 = v.create_snapshot(volume_uuid, "snap1")
    snap2 = v.create_snapshot(volume_uuid, "snap2")
    snap3 = v.create_snapshot(volume_uuid, "snap3")
    v.delete_snapshot("snap1")
    v.delete_snapshot("snap2")
    delete_volume(volume_uuid)

def test_snapshot_list():
    snapshot_list_test(DM)
    snapshot_list_test(VFS, False)

def snapshot_list_test(driver, check_size = True):
    volume1_uuid = create_volume(VOLUME_SIZE_100M, name = "volume1", driver=driver)
    volume2_uuid = create_volume(VOLUME_SIZE_500M, driver=driver)

    with pytest.raises(subprocess.CalledProcessError):
        snapshot = v.inspect_snapshot(str(uuid.uuid1()))

    with pytest.raises(subprocess.CalledProcessError):
        volume = v.inspect_snapshot(str(uuid.uuid1()))

    snap0_vol1_uuid = v.create_snapshot(volume1_uuid, "snap0_vol1")

    snapshot = v.inspect_snapshot("snap0_vol1")
    assert snapshot["UUID"] == snap0_vol1_uuid
    assert snapshot["VolumeUUID"] == volume1_uuid
    assert snapshot["VolumeName"] == "volume1"
    if check_size:
        assert str(snapshot["Size"]) == VOLUME_SIZE_100M
    assert snapshot["Name"] == "snap0_vol1"

    snap1_vol1_uuid = v.create_snapshot(volume1_uuid)
    snap2_vol1_uuid = v.create_snapshot(volume1_uuid)
    snap1_vol2_uuid = v.create_snapshot(volume2_uuid, "snap1_vol2")
    snap2_vol2_uuid = v.create_snapshot(volume2_uuid, "snap2_vol2")
    snap3_vol2_uuid = v.create_snapshot(volume2_uuid, "snap3_vol2")

    volume = v.inspect_volume(volume2_uuid)
    assert snap1_vol2_uuid in volume["Snapshots"]
    assert volume["Snapshots"][snap1_vol2_uuid]["Name"] == "snap1_vol2"
    assert volume["Snapshots"][snap1_vol2_uuid]["CreatedTime"] != ""
    assert snap2_vol2_uuid in volume["Snapshots"]
    assert volume["Snapshots"][snap2_vol2_uuid]["Name"] == "snap2_vol2"
    assert volume["Snapshots"][snap2_vol2_uuid]["CreatedTime"] != ""
    assert snap3_vol2_uuid in volume["Snapshots"]
    assert volume["Snapshots"][snap3_vol2_uuid]["Name"] == "snap3_vol2"
    assert volume["Snapshots"][snap3_vol2_uuid]["CreatedTime"] != ""

    volumes = v.list_volumes()
    assert snap0_vol1_uuid in volumes[volume1_uuid]["Snapshots"]
    assert snap1_vol1_uuid in volumes[volume1_uuid]["Snapshots"]
    assert snap2_vol1_uuid in volumes[volume1_uuid]["Snapshots"]
    assert snap1_vol2_uuid in volumes[volume2_uuid]["Snapshots"]
    assert snap2_vol2_uuid in volumes[volume2_uuid]["Snapshots"]
    assert snap3_vol2_uuid in volumes[volume2_uuid]["Snapshots"]

    v.delete_snapshot(snap0_vol1_uuid)

    with pytest.raises(subprocess.CalledProcessError):
        snapshot = v.inspect_snapshot(snap0_vol1_uuid)

    v.delete_snapshot(snap1_vol1_uuid)
    v.delete_snapshot(snap2_vol1_uuid)
    v.delete_snapshot(snap1_vol2_uuid)
    v.delete_snapshot(snap2_vol2_uuid)
    v.delete_snapshot(snap3_vol2_uuid)

    delete_volume(volume2_uuid)
    delete_volume(volume1_uuid)

def create_delete_volume():
    uuid = v.create_volume(size = VOLUME_SIZE_6M)
    snap = v.create_snapshot(uuid)
    v.delete_snapshot(snap)
    v.delete_volume(uuid)

def test_create_volume_in_parallel():
    threads = []
    for i in range(TEST_THREAD_COUNT):
        threads.append(threading.Thread(target = create_delete_volume))
        threads[i].start()

    for i in range(TEST_THREAD_COUNT):
        threads[i].join()

def test_create_volume_in_sequence():
    for i in range(TEST_LOOP_COUNT):
	create_delete_volume()

def compress_volume(volume_uuid):
    mountpoint = mount_volume(volume_uuid)
    zipfile = os.path.join(TEST_ROOT, volume_uuid)
    shutil.make_archive(zipfile, "zip", mountpoint)
    umount_volume(volume_uuid, mountpoint)
    return zipfile + ".zip"

def get_volume_checksum(volume_uuid, driver):
    f = ""
    if driver == DM:
        f = os.path.join(DM_DIR, volume_uuid)
    elif driver == VFS:
        f = compress_volume(volume_uuid)
    else:
        assert "Shouldn't reach here" == ""
    output = subprocess.check_output(["sha512sum", f]).decode()

    if driver == "VFS" and f != "":
        os.remove(f)
    return output.split(" ")[0]

def check_restore(origin_vol, restored_vol, driver):
    volume_checksum = get_volume_checksum(origin_vol, driver)
    restore_checksum = get_volume_checksum(restored_vol, driver)
    assert volume_checksum == restore_checksum

def test_backup_create_restore_only():
    process_restore_with_original_removed(VFS_DEST, VFS)
    process_restore_with_original_removed(VFS_DEST, DM)

def process_restore_with_original_removed(dest, driver):
    volume1_uuid = create_volume(size = VOLUME_SIZE_500M, driver = driver)
    mount_volume_and_create_file(volume1_uuid, "test-vol1-v1")
    snap1_vol1_uuid = v.create_snapshot(volume1_uuid)
    bak = v.create_backup(snap1_vol1_uuid, dest)
    volume1_checksum = get_volume_checksum(volume1_uuid, driver)
    delete_volume(volume1_uuid)

    if driver == DM:
        #cannot specify size with backup
        with pytest.raises(subprocess.CalledProcessError):
	    res_volume1_uuid = create_volume(VOLUME_SIZE_500M, "res-vol1", bak,
                    driver = driver)

    res_volume1_uuid = create_volume(name = "res-vol1", backup = bak, driver =
            driver)
    res_volume1_checksum = get_volume_checksum(res_volume1_uuid, driver)
    assert res_volume1_checksum == volume1_checksum
    delete_volume(res_volume1_uuid)

    v.delete_backup(bak)

def test_duplicate_backup():
    process_duplicate_backup_test(VFS_DEST, VFS)
    process_duplicate_backup_test(VFS_DEST, DM)

def process_duplicate_backup_test(dest, driver):
    volume_uuid = create_volume(size = VOLUME_SIZE_500M, driver = driver)
    mount_volume_and_create_file(volume_uuid, "volume_snap_test")
    snap_uuid = v.create_snapshot(volume_uuid)
    volume_checksum = get_volume_checksum(volume_uuid, driver)

    bak1 = v.create_backup(snap_uuid, dest)
    bak2 = v.create_backup(snap_uuid, dest)

    res2 = create_volume(backup = bak2, driver = driver)
    res2_checksum = get_volume_checksum(res2, driver = driver)
    assert res2_checksum == volume_checksum

    v.delete_backup(bak2)

    res1 = create_volume(backup = bak1, driver = driver)
    res1_checksum = get_volume_checksum(res1, driver = driver)
    assert res1_checksum == volume_checksum

    v.delete_backup(bak1)
    delete_volume(res2)
    delete_volume(res1)
    delete_volume(volume_uuid)

def test_vfs_objectstore():
    vfs_objectstore_test(VFS)
    vfs_objectstore_test(DM)

def vfs_objectstore_test(driver):
    process_objectstore_test(VFS_DEST, driver)

def get_s3_dest(path = ""):
    region = os.environ[ENV_TEST_AWS_REGION]
    bucket = os.environ[ENV_TEST_AWS_BUCKET]

    return "s3://" + bucket + "@" + region + "/" + path

@pytest.mark.s3
def test_s3_objectstore():
    s3_objectstore_test(VFS)
    s3_objectstore_test(DM)

def s3_objectstore_test(driver):
    process_objectstore_test(get_s3_dest(), driver)
    process_objectstore_test(get_s3_dest(S3_PATH), driver)

def process_objectstore_test(dest, driver):
    #make sure objectstore is empty
    backups = v.list_backup(dest)
    assert len(backups) == 0

    #add volume to objectstore
    volume1_uuid = create_volume(VOLUME_SIZE_500M, "volume1", driver=driver)
    volume1 = v.inspect_volume("volume1")
    volume2_uuid = create_volume(VOLUME_SIZE_100M, "volume2", driver=driver)

    with pytest.raises(subprocess.CalledProcessError):
        backups = v.list_backup(dest, volume1_uuid)

    #first snapshots
    snap1_vol1_uuid = v.create_snapshot(volume1_uuid, "snap1_vol1")
    snap1_vol1 = v.inspect_snapshot("snap1_vol1")
    snap1_vol1_bak = v.create_backup("snap1_vol1", dest)

    backups = v.list_backup(dest, volume1_uuid)
    assert len(backups) == 1
    backup = backups[snap1_vol1_bak]
    assert backup["DriverName"] == driver
    assert backup["VolumeUUID"] == volume1["UUID"]
    assert backup["VolumeName"] == volume1["Name"]
    assert backup["VolumeSize"] == str(volume1["Size"])
    assert backup["VolumeCreatedAt"] == volume1["CreatedTime"]
    assert backup["SnapshotUUID"] == snap1_vol1["UUID"]
    assert backup["SnapshotName"] == snap1_vol1["Name"]
    assert backup["SnapshotCreatedAt"] == snap1_vol1["CreatedTime"]
    assert backup["CreatedTime"] != ""

    backup = v.inspect_backup(snap1_vol1_bak)
    assert backup["DriverName"] == driver
    assert backup["VolumeUUID"] == volume1["UUID"]
    assert backup["VolumeName"] == volume1["Name"]
    assert backup["VolumeSize"] == str(volume1["Size"])
    assert backup["VolumeCreatedAt"] == volume1["CreatedTime"]
    assert backup["SnapshotUUID"] == snap1_vol1["UUID"]
    assert backup["SnapshotName"] == snap1_vol1["Name"]
    assert backup["SnapshotCreatedAt"] == snap1_vol1["CreatedTime"]
    assert backup["CreatedTime"] != ""

    snap1_vol2_uuid = v.create_snapshot(volume2_uuid, "snap1_vol2")
    snap1_vol2_bak = v.create_backup("snap1_vol2", dest)

    #list snapshots
    backups = v.list_backup(dest, volume2_uuid)
    assert len(backups) == 1

    backup = v.inspect_backup(snap1_vol2_bak)
    assert backup["VolumeUUID"] == volume2_uuid
    assert backup["SnapshotUUID"] == snap1_vol2_uuid

    #second snapshots
    mount_volume_and_create_file(volume1_uuid, "test-vol1-v1")
    snap2_vol1_uuid = v.create_snapshot(volume1_uuid)
    snap2_vol1_bak = v.create_backup(snap2_vol1_uuid, dest)

    mount_volume_and_create_file(volume2_uuid, "test-vol2-v2")
    snap2_vol2_uuid = v.create_snapshot(volume2_uuid)
    snap2_vol2_bak = v.create_backup(snap2_vol2_uuid, dest)

    #list snapshots again
    backups = v.list_backup(dest)
    assert len(backups) == 4
    assert backups[snap1_vol1_bak]["DriverName"] == driver
    assert backups[snap1_vol1_bak]["VolumeUUID"] == volume1_uuid
    assert backups[snap1_vol1_bak]["SnapshotUUID"] == snap1_vol1_uuid
    assert backups[snap2_vol1_bak]["DriverName"] == driver
    assert backups[snap2_vol1_bak]["VolumeUUID"] == volume1_uuid
    assert backups[snap2_vol1_bak]["SnapshotUUID"] == snap2_vol1_uuid
    assert backups[snap1_vol2_bak]["DriverName"] == driver
    assert backups[snap1_vol2_bak]["VolumeUUID"] == volume2_uuid
    assert backups[snap1_vol2_bak]["SnapshotUUID"] == snap1_vol2_uuid
    assert backups[snap2_vol2_bak]["DriverName"] == driver
    assert backups[snap2_vol2_bak]["VolumeUUID"] == volume2_uuid
    assert backups[snap2_vol2_bak]["SnapshotUUID"] == snap2_vol2_uuid

    backups = v.list_backup(dest, volume1_uuid)
    assert len(backups) == 2
    assert backups[snap1_vol1_bak]["VolumeUUID"] == volume1_uuid
    assert backups[snap1_vol1_bak]["SnapshotUUID"] == snap1_vol1_uuid
    assert backups[snap2_vol1_bak]["VolumeUUID"] == volume1_uuid
    assert backups[snap2_vol1_bak]["SnapshotUUID"] == snap2_vol1_uuid

    backups = v.list_backup(dest, volume2_uuid)
    assert len(backups) == 2
    assert backups[snap1_vol2_bak]["VolumeUUID"] == volume2_uuid
    assert backups[snap1_vol2_bak]["SnapshotUUID"] == snap1_vol2_uuid
    assert backups[snap2_vol2_bak]["VolumeUUID"] == volume2_uuid
    assert backups[snap2_vol2_bak]["SnapshotUUID"] == snap2_vol2_uuid

    #restore snapshot
    res_volume1_uuid = create_volume(name = "res-vol1", backup = snap2_vol1_bak,
            driver=driver)
    check_restore(volume1_uuid, res_volume1_uuid, driver)

    res_volume2_uuid = create_volume(backup = snap2_vol2_bak, driver=driver)
    check_restore(volume2_uuid, res_volume2_uuid, driver)

    #remove snapshots from objectstore
    v.delete_backup(snap2_vol1_bak)
    v.delete_backup(snap2_vol2_bak)

    #list snapshots again
    backups = v.list_backup(dest)
    assert len(backups) == 2
    assert backups[snap1_vol1_bak]["DriverName"] == driver
    assert backups[snap1_vol1_bak]["VolumeUUID"] == volume1_uuid
    assert backups[snap1_vol1_bak]["SnapshotUUID"] == snap1_vol1_uuid
    assert backups[snap1_vol2_bak]["DriverName"] == driver
    assert backups[snap1_vol2_bak]["VolumeUUID"] == volume2_uuid
    assert backups[snap1_vol2_bak]["SnapshotUUID"] == snap1_vol2_uuid

    backups = v.list_backup(dest, volume1_uuid)
    assert len(backups) == 1
    backup = backups[snap1_vol1_bak]
    assert backup["DriverName"] == driver
    assert backup["VolumeUUID"] == volume1_uuid
    assert backup["SnapshotUUID"] == snap1_vol1_uuid

    backup = v.inspect_backup(snap1_vol1_bak)
    assert backup["DriverName"] == driver
    assert backup["VolumeUUID"] == volume1_uuid
    assert backup["SnapshotUUID"] == snap1_vol1_uuid

    backups = v.list_backup(dest, volume2_uuid)
    assert len(backups) == 1
    backup = backups[snap1_vol2_bak]
    assert backup["DriverName"] == driver
    assert backup["VolumeUUID"] == volume2_uuid
    assert backup["SnapshotUUID"] == snap1_vol2_uuid

    backup = v.inspect_backup(snap1_vol2_bak)
    assert backup["DriverName"] == driver
    assert backup["VolumeUUID"] == volume2_uuid
    assert backup["SnapshotUUID"] == snap1_vol2_uuid

    #remove snapshots from objectstore
    v.delete_backup(snap1_vol2_bak)
    v.delete_backup(snap1_vol1_bak)

    v.delete_snapshot(snap1_vol1_uuid)
    v.delete_snapshot(snap2_vol1_uuid)
    v.delete_snapshot(snap1_vol2_uuid)
    v.delete_snapshot(snap2_vol2_uuid)

    delete_volume(volume1_uuid)
    delete_volume(volume2_uuid)
    delete_volume(res_volume1_uuid)
    delete_volume(res_volume2_uuid)

def test_cross_restore_error_checking():
    vfs_vol_uuid = create_volume(driver=VFS)
    vfs_snap_uuid = v.create_snapshot(vfs_vol_uuid)
    vfs_backup = v.create_backup(vfs_snap_uuid, VFS_DEST)

    dm_vol_uuid = create_volume(size = VOLUME_SIZE_100M, driver=DM)
    dm_snap_uuid = v.create_snapshot(dm_vol_uuid)
    dm_backup = v.create_backup(dm_snap_uuid, VFS_DEST)

    with pytest.raises(subprocess.CalledProcessError):
        create_volume(driver=VFS, backup=dm_backup)

    with pytest.raises(subprocess.CalledProcessError):
        create_volume(driver=DM, backup=vfs_backup)

    vfs_res = create_volume(driver=VFS, backup=vfs_backup)
    dm_res = create_volume(driver=DM, backup=dm_backup)

    delete_volume(vfs_vol_uuid)
    delete_volume(vfs_res)
    delete_volume(dm_vol_uuid)
    delete_volume(dm_res)
