// Copyright Contributors to the Open Cluster Management project

package e2e_test

import (
	"os"
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1 "open-cluster-management.io/api/work/v1"
)

const defaultNamespace = "default"

const timeout = 30 * time.Second

var (
	hubKubeClient    kubernetes.Interface
	spokeKubeClient  kubernetes.Interface
	hubClusterClient clusterclient.Interface
	hubWorkClient    workclient.Interface
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	err := func() error {
		var err error

		hubKubeconfig := os.Getenv("HUBKUBECONFIG")

		hubConfig, err := clientcmd.BuildConfigFromFlags("", hubKubeconfig)
		if err != nil {
			return err
		}

		hubKubeClient, err = kubernetes.NewForConfig(hubConfig)
		if err != nil {
			return err
		}

		hubClusterClient, err = clusterclient.NewForConfig(hubConfig)
		if err != nil {
			return err
		}

		hubWorkClient, err = workclient.NewForConfig(hubConfig)
		if err != nil {
			return err
		}

		spokeKubeconfig := os.Getenv("SPOKEKUBECONFIG")
		spokeConfig, err := clientcmd.BuildConfigFromFlags("", spokeKubeconfig)
		if err != nil {
			return err
		}

		spokeKubeClient, err = kubernetes.NewForConfig(spokeConfig)
		if err != nil {
			return err
		}

		return nil
	}()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

func toManifest(object runtime.Object) workv1.Manifest {
	manifest := workv1.Manifest{}
	manifest.Object = object
	return manifest
}

func newConfigmap(name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: defaultNamespace,
			Name:      name,
		},
		Data: map[string]string{
			"test": "I'm a test configmap",
		},
	}
}
