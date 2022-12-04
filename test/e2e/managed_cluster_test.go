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
	var runtimeClient2 runtimeClient.Client

	ginkgo.BeforeEach(func() {
		ginkgo.By("Check the option controlplane")
		gomega.Expect(len(options.ControlPlanes) == 2).Should(gomega.BeTrue())
		gomega.Expect(len(options.ControlPlanes[0].ManagedCluster) == 1).Should(gomega.BeTrue())
		gomega.Expect(len(options.ControlPlanes[1].ManagedCluster) == 1).Should(gomega.BeTrue())

		ginkgo.By("Get the clients of controlplanes")
		var err error
		runtimeClient1, err = getRuntimeClient(options.ControlPlanes[0].KubeConfig, options.ControlPlanes[0].Context)
		gomega.Expect(err).Should(gomega.BeNil())
		runtimeClient2, err = getRuntimeClient(options.ControlPlanes[1].KubeConfig, options.ControlPlanes[1].Context)
		gomega.Expect(err).Should(gomega.BeNil())
	})

	ginkgo.It("get managed cluster from controlplanes", func() {
		ginkgo.By("Check the managed cluster from controlplane1")
		managedCluster1 := clusterv1.ManagedCluster{}
		managedCluster1.SetName(options.ControlPlanes[0].ManagedCluster[0].Name)
		err := runtimeClient1.Get(context.TODO(), runtimeClient.ObjectKeyFromObject(&managedCluster1), &managedCluster1)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(len(managedCluster1.GetUID()) > 0).Should(gomega.BeTrue())

		ginkgo.By("Check the managed cluster from controlplane2")
		managedCluster2 := clusterv1.ManagedCluster{}
		managedCluster2.SetName(options.ControlPlanes[1].ManagedCluster[0].Name)
		err = runtimeClient2.Get(context.TODO(), runtimeClient.ObjectKeyFromObject(&managedCluster2), &managedCluster2)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(len(managedCluster2.GetUID()) > 0).Should(gomega.BeTrue())
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
