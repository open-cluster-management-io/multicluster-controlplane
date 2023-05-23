// Copyright Contributors to the Open Cluster Management project
package configs

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

const DefaultAPIServerPort = 9443

const (
	defaultControlPlaneDataDir = "/.ocm"
	defaultControlPlaneCADir   = "/.ocm/cert/controlplane-ca"
	defaultETCDMode            = "embed"
	defaultETCDPrefix          = "/registry"
)

type ControlplaneRunConfig struct {
	DataDirectory string          `yaml:"dataDirectory"`
	Apiserver     ApiserverConfig `yaml:"apiserver"`
	Etcd          EtcdConfig      `yaml:"etcd"`
}

type ApiserverConfig struct {
	ExternalHostname string `yaml:"externalHostname"`
	Port             int    `yaml:"port"`
	CAFile           string `yaml:"caFile"`
	CAKeyFile        string `yaml:"caKeyFile"`
}

type EtcdConfig struct {
	Mode     string   `yaml:"mode"`
	Servers  []string `yaml:"servers"`
	CAFile   string   `yaml:"caFile"`
	CertFile string   `yaml:"certFile"`
	KeyFile  string   `yaml:"keyFile"`
	Prefix   string   `yaml:"prefix"`
}

func LoadConfig(configDir string) (*ControlplaneRunConfig, error) {
	configFile := path.Join(configDir, "ocmconfig.yaml")
	configFileData, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	c := &ControlplaneRunConfig{}
	if err := yaml.Unmarshal(configFileData, c); err != nil {
		return nil, err
	}

	if c.DataDirectory == "" {
		c.DataDirectory = defaultControlPlaneDataDir
	}

	if c.Etcd.Mode == "" {
		c.Etcd.Mode = defaultETCDMode
	}

	if c.Etcd.Prefix == "" {
		c.Etcd.Prefix = defaultETCDPrefix
	}

	if len(c.Etcd.Servers) == 0 {
		c.Etcd.Servers = []string{"http://127.0.0.1:2379"}
	}

	if c.Apiserver.ExternalHostname == "" {
		klog.Infof("The external host name unspecified, trying to find it from runtime environment ...")
		hostname, err := util.GetExternalHost()
		if err != nil {
			return nil, fmt.Errorf("failed to find external host name from runtime environment, %v", err)
		}
		c.Apiserver.ExternalHostname = hostname
	}

	if !c.IsCAProvided() {
		klog.Infof("The server ca unspecified, trying to find it from runtime environment ...")
		loaded, err := util.LoadServingSigner(defaultControlPlaneCADir)
		if err != nil {
			return nil, fmt.Errorf("failed to load server ca from runtime enviroment, %v", err)
		}

		if loaded {
			c.Apiserver.CAFile = filepath.Join(defaultControlPlaneCADir, "ca.crt")
			c.Apiserver.CAKeyFile = filepath.Join(defaultControlPlaneCADir, "ca.key")
		}
	}

	return c, nil
}

func (c *ControlplaneRunConfig) IsCAProvided() bool {
	return c.Apiserver.CAFile != "" && c.Apiserver.CAKeyFile != ""
}

func (c *ControlplaneRunConfig) IsEmbedEtcd() bool {
	return c.Etcd.Mode == "embed"
}
