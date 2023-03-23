// Copyright Contributors to the Open Cluster Management project

package cluster

import (
	"context"
	"fmt"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	"open-cluster-management.io/multicluster-controlplane/test/performance/utils"
)

type clusterCleanupOptions struct {
	HubKubeconfig string

	hubKubeClient    kubernetes.Interface
	hubClusterClient clusterclient.Interface
}

func NewClusterCleanupOptions() *clusterCleanupOptions {
	return &clusterCleanupOptions{}
}

func (o *clusterCleanupOptions) Complete() error {
	if o.HubKubeconfig == "" {
		return fmt.Errorf("flag `--controlplane-kubeconfig` is requried")
	}

	hubConfig, err := clientcmd.BuildConfigFromFlags("", o.HubKubeconfig)
	if err != nil {
		return fmt.Errorf("failed to build hub kubeconfig with %s, %v", o.HubKubeconfig, err)
	}

	o.hubKubeClient, err = kubernetes.NewForConfig(hubConfig)
	if err != nil {
		return fmt.Errorf("failed to build hub kube client with %s, %v", o.HubKubeconfig, err)
	}

	o.hubClusterClient, err = clusterclient.NewForConfig(hubConfig)
	if err != nil {
		return fmt.Errorf("failed to build hub cluster client with %s, %v", o.HubKubeconfig, err)
	}

	return nil
}

func (o *clusterCleanupOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.HubKubeconfig, "controlplane-kubeconfig", o.HubKubeconfig, "The kubeconfig of multicluster controlplane")
}

func (o *clusterCleanupOptions) Run() error {
	ctx := context.Background()
	clusters, err := o.hubClusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", performanceTestLabel),
	})
	if err != nil {
		return err
	}

	for _, cluster := range clusters.Items {
		if err := o.hubClusterClient.ClusterV1().ManagedClusters().Delete(ctx, cluster.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}

		utils.PrintMsg(fmt.Sprintf("Cluster %q is deleted", cluster.Name))

		if err := o.hubKubeClient.CoreV1().Namespaces().Delete(ctx, cluster.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}

		utils.PrintMsg(fmt.Sprintf("Cluster namespace %q is deleted", cluster.Name))

	}
	return nil
}
