# Convoy [![Build Status](http://ci.rancher.io/api/badge/github.com/rancher/convoy/status.svg?branch=master)](http://ci.rancher.io/github.com/rancher/convoy)

# Overview
Convoy is a  Docker volume plugin for a variety of storage back-ends. It's designed to be a simple Docker volume plug-ins that supports vendor-specific extensions such as snapshots, backups and restore. It's written in Go and can be deployed as a standalone binary.

[![Convoy_DEMO](https://asciinema.org/a/9y5nbp3h97vyyxnzuax9f568e.png)](https://asciinema.org/a/9y5nbp3h97vyyxnzuax9f568e?autoplay=1&loop=1&size=medium&speed=2)

# Quick Start Guide
First let's make sure we have Docker 1.8 or above running.
```
docker --version
```
If not, install the latest Docker daemon as follows:
```
curl -sSL https://get.docker.com/ | sh
```
Once we have made sure we have the right Docker daemon running, we can install and configure Convoy volume plugin as follows:
```
wget https://github.com/rancher/convoy/releases/download/v0.2/convoy.tar.gz
tar xvf convoy.tar.gz
sudo cp convoy/* /usr/local/bin/
sudo mkdir -p /etc/docker/plugins/
sudo bash -c 'echo "unix:///var/run/convoy/convoy.sock" > /etc/docker/plugins/convoy.spec'
```
We can use file-backed lookback device to test and demo Convoy driver. Lookback device, however, is known to be unstable and should not be used in production.
```
truncate -s 100G data.vol
truncate -s 1G metadata.vol
sudo losetup /dev/loop5 data.vol
sudo losetup /dev/loop6 metadata.vol
```
Once we have the data and metadata device setup, we can start the Convoy plugin daemon as follows:
```
sudo convoy server --drivers devicemapper --driver-opts dm.datadev=/dev/loop5 --driver-opts dm.metadatadev=/dev/loop6
```
We can create a Docker container with a convoy volume. As a test, we create a file called `/vol1/foo` in the convoy volume: 
```
sudo docker run -v vol1:/vol1 --volume-driver=convoy ubuntu touch /vol1/foo
```
Next we take a snapshot of the convoy volume. We backup the snapshot to a local directory: (Backup to NFS share or S3 objectore is also supported.)
```
sudo convoy snapshot create vol1 --name snap1vol1
sudo mkdir /opt/convoy/
sudo convoy backup create snap1vol1 --dest vfs:///opt/convoy/
```
The `convoy backup` command returns a URL string representing backup dataset. You can use the same URL string to recover the volume to another host:
```
sudo convoy create res1 --backup <backup_url>
```
The following command creates a new container and mounts the recovered convoy volume into that container:
```
sudo docker run -v res1:/res1 --volume-driver=convoy ubuntu ls /res1/foo
```
You should see the recovered file ```/res1/foo```. 

# Installation
Ensure you have Docker 1.8 or above installed.

Download latest version of [convoy](https://github.com/rancher/convoy/releases/download/v0.2/convoy.tar.gz) and unzip it. Put the binaries in a directory in the execution ```$PATH``` of sudo and root users (e.g. /usr/local/bin).
```
wget https://github.com/rancher/convoy/releases/download/v0.2/convoy.tar.gz
tar xvf convoy.tar.gz
sudo cp convoy/* /usr/local/bin/
```
Run the following commands to setup the Convoy volume plugin for Docker:
```
sudo mkdir -p /etc/docker/plugins/
sudo bash -c 'echo "unix:///var/run/convoy/convoy.sock" > /etc/docker/plugins/convoy.spec'
```

# Start Convoy Daemon

You need to pass different arguments to convoy daemon depending on the choice of backend implementation.

## Device Mapper
Assuming you have two devices created, one data device called `/dev/convoy-vg/data` and the other metadata device called `/dev/convoy-vg/metadata`. You run the following command to start the Convoy daemon:
```
sudo convoy server --drivers devicemapper --driver-opts dm.datadev=/dev/convoy-vg/data --driver-opts dm.metadatadev=/dev/convoy-vg/metadata
```
Default Convoy volume size is 100G. You can override it with the `---driver-opts dm.defaultvolumesize` option.

## NFS
First, mount the NFS share to the root directory used to store volumes. Substitute `<vfs_path>` to the appropriate directory of your choice:
```
sudo mkdir <vfs_path>
sudo mount -t nfs <nfs_server>:/path <vfs_path>
```
The NFS-based Convoy daemon can be started as follows:
```
sudo convoy server --drivers vfs --driver-opts vfs.path=<vfs_path>
```
# Volume Commands
## Create a Volume

Volumes can be created using the `convoy create` command:
```
sudo convoy create volume_name
```
Default device mapper volume size is 100G. We can supply the `--size` option to specify a custom device mapper volume size.

We can also create a volume using the `docker run` command. If the volume does not yet exist, a new volume will be greated. Otherwise the existing volume will be used.
```
sudo docker -it test_volume:/test --volume-driver=convoy ubuntu
```

## Delete a Volume
```
sudo docker rm -v <container_name>
```
or
```
sudo convoy delete <volume_name>
```
* For NFS-backed volumes only: The `--reference` option instructs the `convoy delete` command to only delete the reference to the NFS-based volume from the current host and leave the underlying files on NFS server unchanged. This is useful where the same NFS-backed volume is mounted into multiple containers.

## List and Inspect a Volume
```
sudo convoy list
sudo convoy inspect vol1
```

# Take Snapshot of a Volume:
```
sudo convoy snapshot create vol1 --name snap1vol1
```

## Backup a Snapshot
We can backup a snapshot to S3 object store or an NFS mount:
```
sudo convoy backup create snap1vol1 --dest s3://backup-bucket@us-west-2/
```
or
```
sudo convoy backup create snap1vol1 --dest vfs:///opt/backup/
```

The backup operation returns a URL string that uniquely idenfied the backup dataset.
```
s3://backup-bucket@us-west-2/?backup=f98f9ea1-dd6e-4490-8212-6d50df1982ea\u0026volume=e0d386c5-6a24-446c-8111-1077d10356b0
```
* For S3, please make sure you have AWS credential ready either at ```~/.aws/credentials``` or as environment variables, as described [here](http://blogs.aws.amazon.com/security/post/Tx3D6U6WSFGOK2H/A-New-and-Standardized-Way-to-Manage-Credentials-in-the-AWS-SDKs). You may need to put credentials to ```/root/.aws/credentials``` or setup sudo environment variables in order to get S3 credential works.

## Restore a Volume from Backup
```
sudo convoy create res1 --backup <url>
```

## Mount a Restored Volume into a Docker Container
We can use the standard `docker run` command to mount the restored volume into a Docker container:
```
sudo docker run -it -v res1:/res1 --volume-driver convoy ubuntu
```

## Mount an NFS-Backed Volume on Multiple Servers
You can mount an NFS-backed volume on multiple servers. You can use the standard `docker run` command to mount an existing NFS-backed mount into a Docker container. For example, if you have already created an NFS-based volume `vol1` on one host, you can run the following command to mount the existing `vol1` volume into a new container:
```
sudo docker run -it -v vol1:/vol1 --volume-driver=convoy ubuntu
```
# Build

1. Environment: Ensure Go environment, mercurial and `libdevmapper-dev` package are installed.
2. Build and install:
```
go get github.com/rancher/convoy
cd $GOPATH/src/github.com/rancher/convoy
make
sudo make install
```
The last line would install convoy to `/usr/local/bin/`, otherwise executables are
in `bin/` directory.
