// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"context"
	"os"
	"path"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/multicluster-controlplane/pkg/agent"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/options"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

const SelfManagementClusterLabel = "multicluster-controlplane.open-cluster-management.io/selfmanagement"

func InstallSelfManagementCluster(options options.ServerRunOptions) func(<-chan struct{}, *aggregatorapiserver.Config) error {
	return func(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error {
		inClusterConfig, err := rest.InClusterConfig()
		if err != nil {
			klog.Warning("Current runtime environment is not in a cluster, ignore --self-management flag.")
			return nil
		}

		if !options.EnableSelfManagement {
			// TODO if there is a self management cluster, cleanup its resources
			return nil
		}

		ctx := util.GoContext(stopCh)
		hubRestConfig := aggregatorConfig.GenericConfig.LoopbackClientConfig
		hubRestConfig.ContentType = "application/json"

		clusterName := options.SelfManagementClusterName
		if len(clusterName) == 0 {
			clusterName, err = util.GenerateSelfManagedClusterName(ctx, inClusterConfig)
			if err != nil {
				return err
			}
		}

		go EnableSelfManagement(ctx, hubRestConfig, options.ControlplaneDataDir, clusterName)

		return nil
	}
}

func EnableSelfManagement(ctx context.Context, hubRestConfig *rest.Config, controlplaneCertDir, selfClusterName string) {
	kubeClient, err := kubernetes.NewForConfig(hubRestConfig)
	if err != nil {
		klog.Fatalf("Failed to kube client, %v", err)
	}

	clusterClient, err := clusterclient.NewForConfig(hubRestConfig)
	if err != nil {
		klog.Fatalf("Failed to cluster client, %v", err)
	}

	if err := createNamespace(ctx, kubeClient, selfClusterName); err != nil {
		klog.Fatalf("Failed to create self managed cluster namespace, %v", err)
	}

	// TODO need a controller to maintain the self managed cluster
	if err := waitForSelfManagedCluster(ctx, clusterClient, selfClusterName); err != nil {
		klog.Fatalf("Failed to create self managed cluster, %v", err)
	}

	bootstrapKubeConfig := path.Join(controlplaneCertDir, "cert", "kube-aggregator.kubeconfig")
	agentHubKubeconfigDir := path.Join(controlplaneCertDir, "agent", "hub-kubeconfig")
	if err := os.MkdirAll(agentHubKubeconfigDir, os.ModePerm); err != nil {
		klog.Fatalf("Failed to create dir %s, %v", agentHubKubeconfigDir, err)
	}

	// TODO also need provide feature gates
	klusterletAgent := agent.NewAgentOptions().
		WithClusterName(selfClusterName).
		WithBootstrapKubeconfig(bootstrapKubeConfig).
		WithHubKubeconfigDir(agentHubKubeconfigDir)

	if err := klusterletAgent.RunAgent(ctx); err != nil {
		klog.Fatalf("failed to start agents, %v", err)
	}
}

func createNamespace(ctx context.Context, kubeClient kubernetes.Interface, ns string) error {
	_, err := kubeClient.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := kubeClient.CoreV1().Namespaces().Create(
			ctx,
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			},
			metav1.CreateOptions{},
		)
		return err
	}

	return err
}

func waitForSelfManagedCluster(ctx context.Context, clusterClient clusterclient.Interface, selfClusterName string) error {
	return wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		selfCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, selfClusterName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			_, err := clusterClient.ClusterV1().ManagedClusters().Create(
				ctx,
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: selfClusterName,
						Labels: map[string]string{
							SelfManagementClusterLabel: "",
						},
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient: true,
					},
				},
				metav1.CreateOptions{},
			)

			return false, err
		}

		if err != nil {
			return false, err
		}

		return meta.IsStatusConditionTrue(selfCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted), nil
	})
}
