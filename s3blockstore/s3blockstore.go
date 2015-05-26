package s3blockstore

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/rancherio/volmgr/blockstore"
	"github.com/rancherio/volmgr/util"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type S3BlockStoreDriver struct {
	ID      string
	Path    string
	Service S3Service
}

const (
	KIND = "s3"

	S3_ACCESS_KEY = "s3.access_key"
	S3_SECRET_KEY = "s3.secret_key"
	S3_REGION     = "s3.region"
	S3_BUCKET     = "s3.bucket"
	S3_PATH       = "s3.path"

	ENV_AWS_ACCESS_KEY = "AWS_ACCESS_KEY_ID"
	ENV_AWS_SECRET_KEY = "AWS_SECRET_ACCESS_KEY"
)

func init() {
	blockstore.RegisterDriver(KIND, initFunc)
}

func initFunc(root, cfgName string, config map[string]string) (blockstore.BlockStoreDriver, error) {
	b := &S3BlockStoreDriver{}
	if cfgName != "" {
		if util.ConfigExists(root, cfgName) {
			err := util.LoadConfig(root, cfgName, b)
			if err != nil {
				return nil, err
			}
			return b, nil
		} else {
			return nil, fmt.Errorf("Wrong configuration file for S3 blockstore driver")
		}
	}

	b.Service.Keys.AccessKey = config[S3_ACCESS_KEY]
	b.Service.Keys.SecretKey = config[S3_SECRET_KEY]
	b.Service.Region = config[S3_REGION]
	b.Service.Bucket = config[S3_BUCKET]
	b.Path = config[S3_PATH]
	if b.Service.Keys.AccessKey == "" || b.Service.Keys.SecretKey == "" ||
		b.Service.Region == "" || b.Service.Bucket == "" || b.Path == "" {
		return nil, fmt.Errorf("Cannot find all required fields: %v %v %v %v %v",
			S3_ACCESS_KEY, S3_SECRET_KEY, S3_REGION, S3_BUCKET, S3_PATH)
	}

	//Test connection
	if _, err := b.List(""); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *S3BlockStoreDriver) Kind() string {
	return KIND
}

func (s *S3BlockStoreDriver) updatePath(path string) string {
	return filepath.Join(s.Path, path)
}

func (s *S3BlockStoreDriver) FinalizeInit(root, cfgName, id string) error {
	s.ID = id
	if err := util.SaveConfig(root, cfgName, s); err != nil {
		return err
	}
	return nil
}

func (s *S3BlockStoreDriver) List(listPath string) ([]string, error) {
	var result []string

	path := s.updatePath(listPath)
	contents, err := s.Service.ListObjects(path)
	if err != nil {
		log.Error("Fail to list s3: ", err)
		return result, err
	}

	size := len(contents)
	if size == 0 {
		return result, nil
	}
	result = make([]string, size)
	for i, obj := range contents {
		result[i] = strings.TrimPrefix(*obj.Key, path)
	}

	return result, nil
}

func (s *S3BlockStoreDriver) FileExists(filePath string) bool {
	return s.FileSize(filePath) >= 0
}

func (s *S3BlockStoreDriver) FileSize(filePath string) int64 {
	path := s.updatePath(filePath)
	contents, err := s.Service.ListObjects(path)
	if err != nil {
		return -1
	}

	if len(contents) == 0 {
		return -1
	}

	//TODO deal with multiple returns
	return *contents[0].Size
}

func (s *S3BlockStoreDriver) Remove(name string) error {
	path := s.updatePath(name)
	return s.Service.DeleteObjects(path)
}

func (s *S3BlockStoreDriver) Read(src string) (io.ReadCloser, error) {
	path := s.updatePath(src)
	rc, err := s.Service.GetObject(path)
	if err != nil {
		return nil, err
	}
	return rc, nil
}

func (s *S3BlockStoreDriver) Write(dst string, rs io.ReadSeeker) error {
	path := s.updatePath(dst)
	return s.Service.PutObject(path, rs)
}

func (s *S3BlockStoreDriver) Upload(src, dst string) error {
	file, err := os.Open(src)
	if err != nil {
		return nil
	}
	defer file.Close()
	path := s.updatePath(dst)
	return s.Service.PutObject(path, file)
}

func (s *S3BlockStoreDriver) Download(src, dst string) error {
	if _, err := os.Stat(dst); err != nil {
		os.Remove(dst)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	path := s.updatePath(src)
	rc, err := s.Service.GetObject(path)
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.Copy(f, rc)
	if err != nil {
		return err
	}
	return nil
}
