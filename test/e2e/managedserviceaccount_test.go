// Copyright Contributors to the Open Cluster Management project
package e2e_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("managed service account tests", func() {
	Context("create a simple managed service account", func() {
		It("should be able to create a managed service account", func() {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "authentication.open-cluster-management.io/v1beta1",
					"kind":       "ManagedServiceAccount",
					"metadata": map[string]interface{}{
						"name":      "my-sample",
						"namespace": loopbackClusterName,
					},
					"spec": map[string]interface{}{
						"rotation": map[string]interface{}{},
					},
				},
			}
			resource := schema.GroupVersionResource{
				Group:    "authentication.open-cluster-management.io",
				Version:  "v1beta1",
				Resource: "managedserviceaccounts",
			}
			Eventually(func() error {
				_, err := hubDynamicClient.Resource(resource).Namespace(loopbackClusterName).
					Create(context.TODO(), obj, metav1.CreateOptions{})
				return err
			}, 2*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

			Eventually(func() error {
				_, err := hubKubeClient.CoreV1().Secrets(loopbackClusterName).Get(context.TODO(), "my-sample", metav1.GetOptions{})
				return err
			}, 1*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
		})
	})
})
