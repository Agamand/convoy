package main

import (
	"fmt"
	"github.com/codegangsta/cli"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"
)

func getConfigFileName(root string) string {
	return filepath.Join(root, CONFIGFILE)
}

func getCfgName() string {
	return CONFIGFILE
}

func genRequiredMissingError(name string) error {
	return fmt.Errorf("Cannot find valid required parameter:", name)
}

func getLowerCaseFlag(v interface{}, key string, required bool, err error) (string, error) {
	value := ""
	switch v := v.(type) {
	default:
		return "", fmt.Errorf("Unexpected type for getLowerCaseFlag")
	case *cli.Context:
		value = v.String(key)
	case map[string]string:
		value = v[key]
	case *http.Request:
		if err := v.ParseForm(); err != nil {
			return "", err
		}
		value = v.FormValue(key)
	}
	result := strings.ToLower(value)
	if required && result == "" {
		err = genRequiredMissingError(key)
	}
	return result, err
}

func cmdInfo(c *cli.Context) {
	if err := doInfo(c); err != nil {
		panic(err)
	}
}

func doInfo(c *cli.Context) error {
	rc, _, err := client.call("GET", "/info", nil, nil)
	if err != nil {
		return err
	}
	defer rc.Close()

	b, err := ioutil.ReadAll(rc)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func (s *Server) doInfo(version string, w http.ResponseWriter, r *http.Request, objs map[string]string) error {
	driver := s.StorageDriver
	data, err := driver.Info()
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	if err != nil {
		return err
	}
	return nil
}
