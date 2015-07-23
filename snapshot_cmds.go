package main

import (
	"github.com/codegangsta/cli"
	"github.com/rancher/rancher-volume/api"
	"github.com/rancher/rancher-volume/util"
	"net/url"
)

var (
	snapshotCreateCmd = cli.Command{
		Name:  "create",
		Usage: "create a snapshot for certain volume: snapshot create <volume>",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "name",
				Usage: "name of snapshot",
			},
		},
		Action: cmdSnapshotCreate,
	}

	snapshotDeleteCmd = cli.Command{
		Name:   "delete",
		Usage:  "delete a snapshot: snapshot delete <snapshot>",
		Action: cmdSnapshotDelete,
	}

	snapshotInspectCmd = cli.Command{
		Name:   "inspect",
		Usage:  "inspect an snapshot: snapshot inspect <snapshot>",
		Action: cmdSnapshotInspect,
	}

	snapshotCmd = cli.Command{
		Name:  "snapshot",
		Usage: "snapshot related operations",
		Subcommands: []cli.Command{
			snapshotCreateCmd,
			snapshotDeleteCmd,
			snapshotInspectCmd,
		},
	}
)

func cmdSnapshotCreate(c *cli.Context) {
	if err := doSnapshotCreate(c); err != nil {
		panic(err)
	}
}

func doSnapshotCreate(c *cli.Context) error {
	var err error

	v := url.Values{}
	volumeUUID, err := getOrRequestUUID(c, "", true)
	snapshotName, err := util.GetName(c, "name", false, err)
	if err != nil {
		return err
	}

	if snapshotName != "" {
		v.Set(api.KEY_NAME, snapshotName)
	}

	request := "/volumes/" + volumeUUID + "/snapshots/create?" + v.Encode()

	return sendRequestAndPrint("POST", request, nil)
}

func cmdSnapshotDelete(c *cli.Context) {
	if err := doSnapshotDelete(c); err != nil {
		panic(err)
	}
}

func doSnapshotDelete(c *cli.Context) error {
	var err error
	uuid, err := getOrRequestUUID(c, "", true)
	if err != nil {
		return err
	}

	request := "/snapshots/" + uuid + "/"
	return sendRequestAndPrint("DELETE", request, nil)
}

func cmdSnapshotInspect(c *cli.Context) {
	if err := doSnapshotInspect(c); err != nil {
		panic(err)
	}
}

func doSnapshotInspect(c *cli.Context) error {
	var err error

	uuid, err := getOrRequestUUID(c, "", true)
	if err != nil {
		return err
	}

	request := "/snapshots/" + uuid + "/"
	return sendRequestAndPrint("GET", request, nil)
}
