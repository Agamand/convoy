package util

import (
	"code.google.com/p/go-uuid/uuid"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	imageFile string
}

var _ = Suite(&TestSuite{})

type Device struct {
	Root              string
	DataDevice        string
	MetadataDevice    string
	ThinpoolDevice    string
	ThinpoolSize      uint64
	ThinpoolBlockSize uint32
	Volumes           map[string]Volume
}

type Volume struct {
	DevID int
	Size  uint64
}

const (
	testRoot  = "/tmp/util"
	testImage = "test.img"
	imageSize = 1 << 27
)

func (s *TestSuite) SetUpSuite(c *C) {
	err := exec.Command("mkdir", "-p", testRoot).Run()
	c.Assert(err, IsNil)

	s.imageFile = filepath.Join(testRoot, testImage)
	err = exec.Command("dd", "if=/dev/zero", "of="+s.imageFile, "bs=4096", "count="+strconv.Itoa(imageSize/4096)).Run()
	c.Assert(err, IsNil)
}

func (s *TestSuite) TearDownSuite(c *C) {
	err := exec.Command("rm", "-rf", testRoot).Run()
	c.Assert(err, IsNil)
}

func (s *TestSuite) TestSaveLoadConfig(c *C) {
	dev := Device{
		Root:              "/tmp/rancher-volume/devmapper",
		DataDevice:        "/dev/loop0",
		MetadataDevice:    "/dev/loop1",
		ThinpoolDevice:    "/dev/mapper/rancher-volume-pool",
		ThinpoolSize:      1024 * 1024 * 1024,
		ThinpoolBlockSize: 4096,
	}

	dev.Volumes = make(map[string]Volume)
	err := SaveConfig("/tmp", "cfg", &dev)
	c.Assert(err, IsNil)

	dev.ThinpoolBlockSize = 2048

	volume := Volume{
		DevID: 1,
		Size:  1000000,
	}
	dev.Volumes["123"] = volume

	err = SaveConfig("/tmp", "cfg", &dev)
	c.Assert(err, IsNil)

	devNew := Device{}
	err = LoadConfig("/tmp", "cfg", &devNew)
	c.Assert(err, IsNil)

	c.Assert(dev, DeepEquals, devNew)
}

func (s *TestSuite) TestListConfigIDs(c *C) {
	tmpdir, err := ioutil.TempDir("/tmp", "rancher-volume")
	c.Assert(err, IsNil)
	defer os.RemoveAll(tmpdir)

	prefix := "prefix_"
	suffix := "_suffix.cfg"
	ids := ListConfigIDs(tmpdir, prefix, suffix)
	c.Assert(ids, HasLen, 0)

	counts := 10
	uuids := make(map[string]bool)
	for i := 0; i < counts; i++ {
		id := uuid.New()
		uuids[id] = true
		err := exec.Command("touch", filepath.Join(tmpdir, prefix+id+suffix)).Run()
		c.Assert(err, IsNil)
	}
	uuidList := ListConfigIDs(tmpdir, prefix, suffix)
	c.Assert(uuidList, HasLen, counts)
	for i := 0; i < counts; i++ {
		_, exists := uuids[uuidList[i]]
		c.Assert(exists, Equals, true)
	}
}

func (s *TestSuite) TestLockFile(c *C) {
	file := "/tmp/t.lock"
	err := LockFile(file)
	c.Assert(err, IsNil)

	err = LockFile(file)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "resource temporarily unavailable")

	err = LockFile(file)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "resource temporarily unavailable")

	err = UnlockFile(file)
	c.Assert(err, IsNil)

	err = LockFile(file)
	c.Assert(err, IsNil)

	err = UnlockFile(file)
	c.Assert(err, IsNil)
}

func (s *TestSuite) TestSliceToMap(c *C) {
	legalMap := []string{
		"a=1",
		"b=2",
	}
	m := SliceToMap(legalMap)
	c.Assert(m["a"], Equals, "1")
	c.Assert(m["b"], Equals, "2")

	illegalMap := []string{
		"a=1",
		"bcd",
	}
	m = SliceToMap(illegalMap)
	c.Assert(m, IsNil)
}

func (s *TestSuite) TestChecksum(c *C) {
	checksum, err := GetFileChecksum(s.imageFile)
	c.Assert(err, IsNil)
	c.Assert(checksum, Equals, "0ff7859005e5debb631f55b7dcf4fb3a1293ff937b488d8bf5a8e173d758917ccf9e835403c16db1b33d406b9b40438f88d184d95c81baece136bc68fa0ae5d2")
}

func (s *TestSuite) TestLoopDevice(c *C) {
	dev, err := AttachLoopbackDevice(s.imageFile, true)
	c.Assert(err, IsNil)

	err = DetachLoopbackDevice("/tmp", dev)
	c.Assert(err, Not(IsNil))

	err = DetachLoopbackDevice(s.imageFile, dev)
	c.Assert(err, IsNil)

	_, err = AttachLoopbackDevice("/tmp", true)
	c.Assert(err, Not(IsNil))

	err = DetachLoopbackDevice("/tmp", "/dev/loop0")
	c.Assert(err, Not(IsNil))
}

func (s *TestSuite) TestValidateUUID(c *C) {
	c.Assert(ValidateUUID(""), Equals, false)
	c.Assert(ValidateUUID("123"), Equals, false)
	c.Assert(ValidateUUID("asdf"), Equals, false)
	c.Assert(ValidateUUID("f997529d-904f-4fbc-8ba2-6d296b74470a"), Equals, true)
	c.Assert(ValidateUUID("00000000-0000-0000-0000-000000000000"), Equals, true)
}

func (s *TestSuite) TestValidateName(c *C) {
	c.Assert(ValidateName(""), Equals, false)
	c.Assert(ValidateName("_09123a."), Equals, true)
	c.Assert(ValidateName("ubuntu14.04_v1"), Equals, true)
	c.Assert(ValidateName("123/456.a"), Equals, false)
	c.Assert(ValidateName("a.\t"), Equals, false)
	c.Assert(ValidateName("ubuntu14.04_v1 "), Equals, false)
}

func (s *TestSuite) TestParseSize(c *C) {
	var (
		value int64
		err   error
	)
	value, err = ParseSize("1024")
	c.Assert(value, Equals, int64(1024))
	c.Assert(err, IsNil)

	value, err = ParseSize("100k")
	c.Assert(value, Equals, int64(102400))
	c.Assert(err, IsNil)

	value, err = ParseSize("100m")
	c.Assert(value, Equals, int64(104857600))
	c.Assert(err, IsNil)

	value, err = ParseSize("100g")
	c.Assert(value, Equals, int64(107374182400))
	c.Assert(err, IsNil)

	value, err = ParseSize("100K")
	c.Assert(value, Equals, int64(102400))

	value, err = ParseSize("0")
	c.Assert(value, Equals, int64(0))
	c.Assert(err, ErrorMatches, "Invalid size.*")

	value, err = ParseSize("0k")
	c.Assert(value, Equals, int64(0))
	c.Assert(err, ErrorMatches, "Invalid size.*")

	value, err = ParseSize("")
	c.Assert(value, Equals, int64(0))
	c.Assert(err, ErrorMatches, "strconv.ParseInt: parsing .*: invalid syntax")

	value, err = ParseSize("m")
	c.Assert(value, Equals, int64(0))
	c.Assert(err, ErrorMatches, "strconv.ParseInt: parsing .*: invalid syntax")

	value, err = ParseSize(".m")
	c.Assert(value, Equals, int64(0))
	c.Assert(err, ErrorMatches, "strconv.ParseInt: parsing .*: invalid syntax")
}

func (s *TestSuite) TestIndex(c *C) {
	var err error
	index := NewIndex()
	err = index.Add("key1", "value1")
	c.Assert(err, IsNil)

	err = index.Add("key1", "value2")
	c.Assert(err, ErrorMatches, "BUG: Conflict when updating index.*")

	err = index.Add("", "value")
	c.Assert(err, ErrorMatches, "BUG: Invalid empty index key")

	err = index.Add("key", "")
	c.Assert(err, ErrorMatches, "BUG: Invalid empty index value")

	value := index.Get("key1")
	c.Assert(value, Equals, "value1")

	value = index.Get("keyx")
	c.Assert(value, Equals, "")

	err = index.Delete("")
	c.Assert(err, ErrorMatches, "BUG: Invalid empty index key")

	err = index.Delete("keyx")
	c.Assert(err, ErrorMatches, "BUG: About to remove non-existed key.*")

	err = index.Delete("key1")
	c.Assert(err, IsNil)
}
