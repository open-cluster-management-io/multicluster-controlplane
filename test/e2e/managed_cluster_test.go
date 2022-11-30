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
	var runtimeClient1 runtimeClient.Client

	ginkgo.BeforeEach(func() {
		ginkgo.By("Check the option controlplane")
		gomega.Expect(len(options.ControlPlanes) > 0).Should(gomega.BeTrue())
		gomega.Expect(len(options.ControlPlanes[0].ManagedCluster) > 0).Should(gomega.BeTrue())

		ginkgo.By("Get a controlplane name")

		ginkgo.By("Get the controlplane restConfig")
		restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{
				ExplicitPath: options.ControlPlanes[0].KubeConfig,
			},
			&clientcmd.ConfigOverrides{
				CurrentContext: options.ControlPlanes[0].Context,
			}).ClientConfig()
		gomega.Expect(err).Should(gomega.BeNil())

		ginkgo.By("Get the controlplane runtime client")
		runtimeClient1, err = runtimeClient.New(restConfig, runtimeClient.Options{Scheme: runtimeScheme})
		gomega.Expect(err).Should(gomega.BeNil())
	})

	ginkgo.It("get managed cluster from controlplane", func() {
		managedCluster := clusterv1.ManagedCluster{}
		managedCluster.SetName(options.ControlPlanes[0].ManagedCluster[0].Name)
		err := runtimeClient1.Get(context.TODO(), runtimeClient.ObjectKeyFromObject(&managedCluster), &managedCluster)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(len(managedCluster.GetUID()) > 0).Should(gomega.BeTrue())
	})
})
