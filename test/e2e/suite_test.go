// Copyright Contributors to the Open Cluster Management project

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1 "open-cluster-management.io/api/work/v1"
)

const defaultNamespace = "default"

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

// TODO remove this after registration starts supporting auto approve
func approveCSR(clusterName string) error {
	return wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
		csrs, err := hubKubeClient.CertificatesV1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name=%s", clusterName),
		})
		if err != nil {
			return false, err
		}

		if len(csrs.Items) == 0 {
			return false, nil
		}

		for _, csr := range csrs.Items {
			if isCSRInTerminalState(&csr.Status) {
				continue
			}

			copied := csr.DeepCopy()
			copied.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
				Type:           certificatesv1.CertificateApproved,
				Status:         corev1.ConditionTrue,
				Reason:         "AutoApprovedByE2ETest",
				Message:        "Auto approved by e2e test",
				LastUpdateTime: metav1.Now(),
			})
			_, err := hubKubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(context.TODO(), copied.Name, copied, metav1.UpdateOptions{})
			if err != nil {
				return false, err
			}
		}

		return true, nil
	})
}

func isCSRInTerminalState(status *certificatesv1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}

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
