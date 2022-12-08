// Copyright Contributors to the Open Cluster Management project
package e2e_test

import (
	"context"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/klog"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = ginkgo.Describe("ManagedCluster", ginkgo.Label("cluster"), func() {
	ginkgo.It("get managed clusters from controlPlanes", func() {
		gomega.Eventually(func() bool {
			availableCount := 0
			for _, controlPlane := range options.ControlPlanes {
				managedCluster := clusterv1.ManagedCluster{}
				managedCluster.SetName(controlPlane.ManagedCluster[0].Name)
				client := runtimeClientMap[controlPlane.Name]
				err := client.Get(context.TODO(), runtimeClient.ObjectKeyFromObject(&managedCluster), &managedCluster)
				gomega.Expect(err).Should(gomega.BeNil())
				gomega.Expect(len(managedCluster.GetUID()) > 0).Should(gomega.BeTrue())

				if meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable) {
					klog.V(5).Infof("%s is available on %s", controlPlane.ManagedCluster[0].Name, controlPlane.Name)
					availableCount++
				}
			}
			return availableCount > 0 && availableCount == len(options.ControlPlanes)
		}).WithTimeout(30 * time.Second).Should(gomega.BeTrue())
	})
})
