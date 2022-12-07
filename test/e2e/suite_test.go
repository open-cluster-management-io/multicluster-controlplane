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
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	msav1alpha1 "open-cluster-management.io/managed-serviceaccount/api/v1alpha1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	options          Options
	timeout          time.Duration
	file             string
	runtimeClientMap map[string]runtimeClient.Client
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
	initRuntimeClients(addScheme())
})

func addScheme() *runtime.Scheme {
	runtimeScheme := runtime.NewScheme()
	gomega.Expect(kubescheme.AddToScheme(runtimeScheme)).Should(gomega.BeNil())
	gomega.Expect(clusterv1.AddToScheme(runtimeScheme)).Should(gomega.BeNil())
	gomega.Expect(addonv1alpha1.AddToScheme(runtimeScheme)).Should(gomega.BeNil())
	gomega.Expect(msav1alpha1.AddToScheme(runtimeScheme)).Should(gomega.BeNil())
	return runtimeScheme
}

func initRuntimeClients(scheme *runtime.Scheme) {
	runtimeClientMap = make(map[string]runtimeClient.Client, 0)

	for _, controlPlane := range options.ControlPlanes {
		ginkgo.By("Ensure each controlplane with a managed cluster")
		gomega.Expect(len(controlPlane.Name) > 0).Should(gomega.BeTrue())
		gomega.Expect(len(controlPlane.ManagedCluster) == 1).Should(gomega.BeTrue())

		ginkgo.By("Get the client of the controlplane")
		client, err := getRuntimeClient(controlPlane.KubeConfig, scheme)
		gomega.Expect(err).Should(gomega.BeNil())
		runtimeClientMap[controlPlane.Name] = client

		ginkgo.By("Get the client of the managedcluster")
		client, err = getRuntimeClient(controlPlane.ManagedCluster[0].KubeConfig, scheme)
		gomega.Expect(err).Should(gomega.BeNil())
		runtimeClientMap[controlPlane.ManagedCluster[0].Name] = client
	}

	gomega.Expect(len(runtimeClientMap) > 0).Should(gomega.BeTrue())
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

func getRuntimeClient(kubeconfig string, runtimeScheme *runtime.Scheme) (runtimeClient.Client, error) {
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	client, err := runtimeClient.New(restConfig, runtimeClient.Options{Scheme: runtimeScheme})
	if err != nil {
		return nil, err
	}
	return client, nil
}
