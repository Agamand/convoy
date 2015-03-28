#!/usr/bin/python

import subprocess
import os
import json

EXT4_FS = "ext4"

class VolumeManager:
    def __init__(self, cmdline, mount_root):
        self.base_cmdline = cmdline
	self.mount_root = mount_root

    def create_volume(self, size):
        data = subprocess.check_output(self.base_cmdline + ["volume", "create",
    	    "--size", str(size)])
        volume = json.loads(data)
        uuid = volume["UUID"]
        assert volume["Size"] == size
        assert volume["Base"] == ""
        return uuid

    def delete_volume(self, uuid):
        subprocess.check_call(self.base_cmdline + ["volume", "delete",
    	    "--uuid", uuid])

    def mount_volume(self, uuid, need_format):
        volume_mount_dir = os.path.join(self.mount_root, uuid)
        if not os.path.exists(volume_mount_dir):
    	    os.makedirs(volume_mount_dir)
        assert os.path.exists(volume_mount_dir)
        cmdline = self.base_cmdline + ["volume", "mount",
    		"--uuid", uuid,
    		"--mountpoint", volume_mount_dir,
    		"--fs", EXT4_FS]
        if need_format:
    	    cmdline = cmdline + ["--format"]

	subprocess.check_call(cmdline)
        return volume_mount_dir

    def umount_volume(self, uuid):
        subprocess.check_call(self.base_cmdline + ["volume", "umount",
    	    "--uuid", uuid])

    def list_volumes(self, uuid = None):
        if uuid is None:
    	    data = subprocess.check_output(self.base_cmdline + \
			    ["volume", "list"])
    	    volumes = json.loads(data)
    	    return volumes

        data = subprocess.check_output(self.base_cmdline + ["volume", "list",
    	    "--uuid", uuid])
        volumes = json.loads(data)
        return volumes

    def create_snapshot(self, volume_uuid):
        data = subprocess.check_output(self.base_cmdline + \
		["snapshot", "create",
    	    	"--volume-uuid", volume_uuid])
        snapshot = json.loads(data)
        assert snapshot["VolumeUUID"] == volume_uuid
        return snapshot["UUID"]

    def delete_snapshot(self, snapshot_uuid, volume_uuid):
        subprocess.check_call(self.base_cmdline + ["snapshot", "delete",
	        "--uuid", snapshot_uuid,
	        "--volume-uuid", volume_uuid])

    def register_vfs_blockstore(self, path):
	data = subprocess.check_output(self.base_cmdline + ["blockstore",
		"register", "--kind", "vfs",
		"--opts", "vfs.path="+path])
	bs = json.loads(data)
	assert bs["Kind"] == "vfs"
	return bs["UUID"]

    def deregister_blockstore(self, uuid):
	subprocess.check_call(self.base_cmdline + ["blockstore", "deregister",
		"--uuid", uuid])

