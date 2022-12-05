// Copyright Contributors to the Open Cluster Management project
package e2e_test

import (
	"context"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = ginkgo.Describe("ManagedCluster", func() {
	var runtimeClientMap map[string]runtimeClient.Client

	ginkgo.BeforeEach(func() {
		runtimeClientMap = make(map[string]runtimeClient.Client, 0)

		for _, controlPlane := range options.ControlPlanes {
			ginkgo.By("Ensure each controlplane with a managed cluster")
			gomega.Expect(len(controlPlane.Name) > 0).Should(gomega.BeTrue())
			gomega.Expect(len(controlPlane.ManagedCluster) == 1).Should(gomega.BeTrue())

			ginkgo.By("Get the client of the controlplane")
			client, err := getRuntimeClient(controlPlane.KubeConfig, controlPlane.Context)
			gomega.Expect(err).Should(gomega.BeNil())
			runtimeClientMap[controlPlane.Name] = client
		}

		gomega.Expect(len(runtimeClientMap) > 0).Should(gomega.BeTrue())
	})

	ginkgo.It("get managed clusters from controlplanes", func() {
		testControlPlane := 0
		for _, controlPlane := range options.ControlPlanes {
			managedCluster := clusterv1.ManagedCluster{}
			managedCluster.SetName(controlPlane.ManagedCluster[0].Name)
			client := runtimeClientMap[controlPlane.Name]

			err := client.Get(context.TODO(), runtimeClient.ObjectKeyFromObject(&managedCluster), &managedCluster)
			gomega.Expect(err).Should(gomega.BeNil())
			gomega.Expect(len(managedCluster.GetUID()) > 0).Should(gomega.BeTrue())
			testControlPlane++
		}
		gomega.Expect(testControlPlane > 0).Should(gomega.BeTrue())
	})
})

func getRuntimeClient(kubeconfig, kubecontext string) (runtimeClient.Client, error) {
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: kubeconfig,
		},
		&clientcmd.ConfigOverrides{
			CurrentContext: kubecontext,
		}).ClientConfig()
	if err != nil {
		return nil, err
	}
	client, err := runtimeClient.New(restConfig, runtimeClient.Options{Scheme: runtimeScheme})
	if err != nil {
		return nil, err
	}
	return client, nil
}
