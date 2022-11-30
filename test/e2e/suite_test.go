// Copyright Contributors to the Open Cluster Management project

package e2e_test

import (
	"encoding/json"
	"flag"
	"os"
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	options       Options
	timeout       time.Duration
	file          string
	runtimeScheme *runtime.Scheme
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E Suite")
}

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)
	klog.InitFlags(nil)
	flag.StringVar(&file, "options", "", "Location of an options.yaml file to provide input for various tests")
}

var _ = ginkgo.BeforeSuite(func() {
	initVars()
	addScheme()
})

func addScheme() {
	runtimeScheme = runtime.NewScheme()
	gomega.Expect(clusterv1.AddToScheme(runtimeScheme)).Should(gomega.BeNil())
}

func initVars() {
	timeout = time.Second * 30

	data, err := os.ReadFile(file)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	optionsContainer := OptionsContainer{}
	err = yaml.UnmarshalStrict([]byte(data), &optionsContainer)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	options = optionsContainer.Options

	s, _ := json.MarshalIndent(optionsContainer, "", "  ")
	klog.V(6).Infof("OptionsContainer %s", s)
}

type OptionsContainer struct {
	Options Options `yaml:"options"`
}

// Define options available for Tests to consume
type Options struct {
	Hosting       Cluster        `yaml:"hosting"`
	ControlPlanes []ControlPlane `yaml:"controlplanes"`
}

type ControlPlane struct {
	Name           string    `yaml:"name,omitempty"`
	KubeConfig     string    `yaml:"kubeconfig,omitempty"`
	Context        string    `yaml:"context,omitempty"`
	ManagedCluster []Cluster `yaml:"managedclusters,omitempty"`
}

type Cluster struct {
	Name       string `yaml:"name,omitempty"`
	Context    string `yaml:"context,omitempty"`
	KubeConfig string `yaml:"kubeconfig,omitempty"`
}
