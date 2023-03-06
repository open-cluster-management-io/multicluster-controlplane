// Copyright Contributors to the Open Cluster Management project
package util

import (
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"k8s.io/klog/v2"
)

const (
	DefaultConfigFile                        = "ocmconfig.yaml"
	DefaultMulticlusterControlplaneConfigDir = ".ocmconfig"

	ConfigDirectory           = "configDirectory"
	DeployToOCP               = "deployToOCP"
	ApiserverExternalHostname = "apiserver.externalHostname"
	ApiserverPort             = "apiserver.port"
	ApiserverCAFile           = "apiserver.caFile"
	ApiserverCAKeyFile        = "apiserver.caKeyFile"
	EtcdMode                  = "etcd.mode"
	EtcdPrefix                = "etcd.prefix"
	EtcdServers               = "etcd.servers"
	EtcdCAFile                = "etcd.caFile"
	EtcdCertFile              = "etcd.certFile"
	EtcdKeyFile               = "etcd.keyFile"
)

func NewDefaultControlplaneRunConfig() *ControlplaneRunConfig {
	return &ControlplaneRunConfig{
		ConfigFile: DefaultConfigFile,
	}
}

type ControlplaneRunConfig struct {
	ConfigFile      string
	ConfigDirectory string          `yaml:"configDirectory"`
	DeployToOCP     bool            `yaml:"deployToOCP"`
	Apiserver       ApiserverConfig `yaml:"apiserver"`
	Etcd            EtcdConfig      `yaml:"etcd"`
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

func (c *ControlplaneRunConfig) LoadConfig() error {

	config := viper.New()

	var configFields = []string{
		ConfigDirectory,
		DeployToOCP,
		ApiserverExternalHostname,
		ApiserverPort,
		ApiserverCAFile,
		ApiserverCAKeyFile,
		EtcdMode,
		EtcdPrefix,
		EtcdServers,
		EtcdCAFile,
		EtcdCertFile,
		EtcdKeyFile,
	}

	var defaultValuesMap = map[string]interface{}{
		ConfigDirectory:           ".ocmconfig",
		DeployToOCP:               false,
		ApiserverExternalHostname: "",
		ApiserverPort:             9443,
		ApiserverCAFile:           "",
		ApiserverCAKeyFile:        "",
		EtcdMode:                  "embed",
		EtcdPrefix:                "/registry",
		EtcdServers:               []string{"http://127.0.0.1:2379"},
		EtcdCAFile:                "",
		EtcdCertFile:              "",
		EtcdKeyFile:               "",
	}

	for _, key := range configFields {
		config.SetDefault(key, defaultValuesMap[key])
	}

	config.SetConfigFile(c.ConfigFile)

	if err := config.ReadInConfig(); err != nil {
		return err
	}

	if err := config.Unmarshal(&c); err != nil {
		return err
	}

	// in deploy mode, certs directory would be read-only because of kustomize mount, copy root-ca and key to config directory
	if c.DeployToOCP && c.IsCAProvided() {
		certDir := filepath.Join(c.ConfigDirectory, "cert")
		if _, err := os.Stat(certDir); err != nil {
			if os.IsNotExist(err) {
				err := os.Mkdir(certDir, 0777)
				if err != nil {
					return err
				}
			}
			return err
		}

		caSrc := c.Apiserver.CAFile
		c.Apiserver.CAFile = filepath.Join(certDir, "root-ca.crt")
		if err := copyFile(caSrc, c.Apiserver.CAFile); err != nil {
			return err
		}

		keySrc := c.Apiserver.CAKeyFile
		c.Apiserver.CAKeyFile = filepath.Join(certDir, "root-ca.key")
		if err := copyFile(keySrc, c.Apiserver.CAKeyFile); err != nil {
			return err
		}
	}
	klog.Infof("controlplane config: %+v\n", c)
	return nil
}

func (c *ControlplaneRunConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.ConfigFile, "config-file", DefaultConfigFile, "To specify the config file")
}

func (c *ControlplaneRunConfig) IsCAProvided() bool {
	return c.Apiserver.CAFile != "" && c.Apiserver.CAKeyFile != ""
}

func (c *ControlplaneRunConfig) IsEmbedEtcd() bool {
	return c.Etcd.Mode == "embed"
}

func (c *ControlplaneRunConfig) IsDeployToOCP() bool {
	return c.DeployToOCP
}

func copyFile(src, dst string) error {
	fin, err := os.Open(src)
	if err != nil {
		return err
	}
	defer fin.Close()

	fout, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer fout.Close()

	_, err = io.Copy(fout, fin)
	if err != nil {
		return err
	}

	return nil
}
