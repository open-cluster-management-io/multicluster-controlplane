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
	"open-cluster-management.io/sdk-go/pkg/helpers"

	"open-cluster-management.io/multicluster-controlplane/pkg/agent"
	"open-cluster-management.io/multicluster-controlplane/pkg/certificate"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/options"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

const SelfManagementClusterLabel = "multicluster-controlplane.open-cluster-management.io/selfmanagement"

type ClusterInfo struct {
	ClusterName string
	URL         string
	CABundle    []byte
}

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

		kubeClient, err := kubernetes.NewForConfig(inClusterConfig)
		if err != nil {
			return err
		}
		apiserverURL, err := helpers.GetAPIServer(kubeClient)
		if err != nil {
			return err
		}
		caBundle, err := helpers.GetCACert(kubeClient)
		if err != nil {
			return err
		}

		selfClusterInfo := ClusterInfo{
			ClusterName: clusterName,
			URL:         apiserverURL,
			CABundle:    caBundle,
		}

		go EnableSelfManagement(ctx, hubRestConfig, options.ControlplaneDataDir, &selfClusterInfo)

		return nil
	}
}

func EnableSelfManagement(ctx context.Context, hubRestConfig *rest.Config, controlplaneCertDir string, selfClusterInfo *ClusterInfo) {
	kubeClient, err := kubernetes.NewForConfig(hubRestConfig)
	if err != nil {
		klog.Fatalf("Failed to kube client, %v", err)
	}

	clusterClient, err := clusterclient.NewForConfig(hubRestConfig)
	if err != nil {
		klog.Fatalf("Failed to cluster client, %v", err)
	}

	if err := createNamespace(ctx, kubeClient, selfClusterInfo.ClusterName); err != nil {
		klog.Fatalf("Failed to create self managed cluster namespace, %v", err)
	}

	// TODO need a controller to maintain the self managed cluster
	if err := waitForSelfManagedCluster(ctx, clusterClient, selfClusterInfo); err != nil {
		klog.Fatalf("Failed to create self managed cluster, %v", err)
	}

	bootstrapKubeConfig := path.Join(controlplaneCertDir, "cert", certificate.InclusterKubeconfigFileName)
	agentHubKubeconfigDir := path.Join(controlplaneCertDir, "agent", "hub-kubeconfig")
	if err := os.MkdirAll(agentHubKubeconfigDir, os.ModePerm); err != nil {
		klog.Fatalf("Failed to create dir %s, %v", agentHubKubeconfigDir, err)
	}

	// TODO also need provide feature gates
	klusterletAgent := agent.NewAgentOptions().
		WithClusterName(selfClusterInfo.ClusterName).
		WithBootstrapKubeconfig(bootstrapKubeConfig).
		WithHubKubeconfigDir(agentHubKubeconfigDir).
		WithWorkloadSourceDriverConfig(agentHubKubeconfigDir + "/kubeconfig")

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

func waitForSelfManagedCluster(ctx context.Context, clusterClient clusterclient.Interface, selfClusterInfo *ClusterInfo) error {
	return wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		klog.Info("Waiting for self managed cluster to be accepted")
		selfCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, selfClusterInfo.ClusterName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			selfManagedCluster, err := clusterClient.ClusterV1().ManagedClusters().Create(
				ctx,
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: selfClusterInfo.ClusterName,
						Labels: map[string]string{
							SelfManagementClusterLabel: "",
						},
					},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient: true,
						ManagedClusterClientConfigs: []clusterv1.ClientConfig{
							{
								URL:      selfClusterInfo.URL,
								CABundle: selfClusterInfo.CABundle,
							},
						},
					},
				},
				metav1.CreateOptions{},
			)

			// The creation of the ManagedCluster CRD may take some time to complete.
			// Therefore, we handle the "not found" error gracefully by ignoring it
			// and allowing the operation to retry until the resource becomes available.
			if err == nil {
				return meta.IsStatusConditionTrue(selfManagedCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted), nil
			} else if err != nil && errors.IsNotFound(err) {
				return false, nil
			} else if err != nil {
				return false, err
			}
		}

		if err != nil {
			return false, err
		}

		return meta.IsStatusConditionTrue(selfCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted), nil
	})
}
