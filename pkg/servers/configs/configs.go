// Copyright Contributors to the Open Cluster Management project
package configs

import (
	"fmt"
	"os"
	"path"

	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

const DefaultAPIServerPort = 9443

const (
	defaultControlPlaneDataDir = "/.ocm"
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
	c, err := loadConfigFromFile(configDir)
	if err != nil {
		return nil, err
	}

	//TODO if c is nil, read configs from others

	if c.Apiserver.ExternalHostname == "" {
		klog.Warningf("The external host name unspecified, trying to find it from runtime environment ...")
		hostname, err := util.GetExternalHost()
		if err != nil {
			return nil, fmt.Errorf("failed to find external host name from runtime environment, %v", err)
		}
		c.Apiserver.ExternalHostname = hostname
	}

	return c, nil
}

func (c *ControlplaneRunConfig) IsCAProvided() bool {
	return c.Apiserver.CAFile != "" && c.Apiserver.CAKeyFile != ""
}

func (c *ControlplaneRunConfig) IsEmbedEtcd() bool {
	return c.Etcd.Mode == "embed"
}

func loadConfigFromFile(configDir string) (*ControlplaneRunConfig, error) {
	configFile := path.Join(configDir, "ocmconfig.yaml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return nil, err
	}

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

	return c, nil
}
