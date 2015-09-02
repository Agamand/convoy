//+build ebstest

package ebs

import (
	"github.com/Sirupsen/logrus"
	"os"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(c *C) {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(os.Stderr)
}

func (s *TestSuite) TestEC2Metadata(c *C) {
	var err error

	svc, err := NewEBSService()
	c.Assert(err, IsNil)

	c.Assert(svc.Region, Not(Equals), "")
	c.Assert(svc.AvailabilityZone, Not(Equals), "")
	c.Assert(svc.InstanceID, Not(Equals), "")
}

func (s *TestSuite) TestBlkDevList(c *C) {
	devList, err := getBlkDevList()
	c.Assert(err, IsNil)
	c.Assert(len(devList), Not(Equals), 0)
	c.Assert(devList["loop0"], Equals, true)
}

func (s *TestSuite) TestVolumeAndSnapshot(c *C) {
	var err error

	svc, err := NewEBSService()
	c.Assert(err, IsNil)

	// should contain the root device only
	devMap, err := svc.getInstanceDevList()
	c.Assert(err, IsNil)
	originDevCounts := len(devMap)
	c.Assert(originDevCounts, Not(Equals), 0)

	log.Debug("Creating volume1")
	volumeID1, err := svc.CreateVolume(GB, "", "")
	c.Assert(err, IsNil)
	c.Assert(volumeID1, Not(Equals), "")

	log.Debug("Attaching volume1")
	dev1, err := svc.AttachVolume(volumeID1, GB)
	c.Assert(err, IsNil)
	c.Assert(strings.HasPrefix(dev1, "/dev/"), Equals, true)
	stat1, err := os.Stat(dev1)
	c.Assert(err, IsNil)
	c.Assert(stat1.Mode()&os.ModeDevice != 0, Equals, true)
	log.Debug("Attached volume1 at ", dev1)

	devMap, err = svc.getInstanceDevList()
	c.Assert(err, IsNil)
	c.Assert(len(devMap), Equals, originDevCounts+1)

	log.Debug("Creating snapshot")
	snapshotID, err := svc.CreateSnapshot(volumeID1, "Test snapshot")
	c.Assert(err, IsNil)
	c.Assert(snapshotID, Not(Equals), "")
	log.Debug("Creating snapshot ", snapshotID)
	err = svc.WaitForSnapshotComplete(snapshotID)
	c.Assert(err, IsNil)

	log.Debug("Creating volume from snapshot")
	volumeID2, err := svc.CreateVolume(2*GB, snapshotID, "gp2")
	c.Assert(err, IsNil)
	c.Assert(volumeID2, Not(Equals), "")

	log.Debug("Deleting snapshot")
	err = svc.DeleteSnapshot(snapshotID)
	c.Assert(err, IsNil)

	log.Debug("Attaching volume2")
	dev2, err := svc.AttachVolume(volumeID2, 2*GB)
	c.Assert(err, IsNil)
	c.Assert(strings.HasPrefix(dev2, "/dev/"), Equals, true)
	stat2, err := os.Stat(dev2)
	c.Assert(err, IsNil)
	c.Assert(stat2.Mode()&os.ModeDevice != 0, Equals, true)
	log.Debug("Attached volume2 at ", dev2)

	devMap, err = svc.getInstanceDevList()
	c.Assert(err, IsNil)
	c.Assert(len(devMap), Equals, originDevCounts+2)

	log.Debug("Detaching volume2")
	err = svc.DetachVolume(volumeID2)
	c.Assert(err, IsNil)

	devMap, err = svc.getInstanceDevList()
	c.Assert(err, IsNil)
	c.Assert(len(devMap), Equals, originDevCounts+1)

	log.Debug("Detaching volume1")
	err = svc.DetachVolume(volumeID1)
	c.Assert(err, IsNil)

	devMap, err = svc.getInstanceDevList()
	c.Assert(err, IsNil)
	c.Assert(len(devMap), Equals, originDevCounts)

	log.Debug("Deleting volume2")
	err = svc.DeleteVolume(volumeID2)
	c.Assert(err, IsNil)

	log.Debug("Deleting volume1")
	err = svc.DeleteVolume(volumeID1)
	c.Assert(err, IsNil)
}
