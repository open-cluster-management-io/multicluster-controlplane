// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"context"
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/multicluster-controlplane/pkg/agent"
)

const (
	// TODO consider to put this in the api repo as a common const
	defaultOCMAgentNamespace = "open-cluster-management-agent"
	selfManagedClusterName   = "local-cluster"
)

func InstallAgent(controlplaneCertDir string) func(<-chan struct{}, *aggregatorapiserver.Config) error {
	return func(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error {
		go func() {
			restConfig := aggregatorConfig.GenericConfig.LoopbackClientConfig
			restConfig.ContentType = "application/json"

			kubeClient, err := kubernetes.NewForConfig(restConfig)
			if err != nil {
				klog.Fatalf("failed to kube client, %v", err)
			}

			clusterClient, err := clusterclient.NewForConfig(restConfig)
			if err != nil {
				klog.Fatalf("failed to cluster client, %v", err)
			}

			ctx := GoContext(stopCh)

			agentNamespace := getAgentNamespace()
			if err := createNamespace(ctx, kubeClient, agentNamespace); err != nil {
				klog.Fatalf("failed to create ocm agent namespace, %v", err)
			}

			if err := createNamespace(ctx, kubeClient, selfManagedClusterName); err != nil {
				klog.Fatalf("failed to create self managed cluster namespace, %v", err)
			}

			if err := createSelfManagedCluster(ctx, clusterClient); err != nil {
				klog.Fatalf("failed to create self managed cluster, %v", err)
			}

			bootstrapKubeConfig := path.Join(controlplaneCertDir, "kube-aggregator.kubeconfig")
			// TODO also need with feature gates
			klusterletAgent := agent.NewAgentOptions().
				WithClusterName(selfManagedClusterName).
				WithSpokeKubeconfig(restConfig).
				WithBootstrapKubeconfig(bootstrapKubeConfig).
				WithHubKubeconfigDir("/tmp")

			if err := klusterletAgent.RunAgent(ctx); err != nil {
				klog.Fatalf("failed to start agents, %v", err)
			}
		}()
		return nil
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

func createSelfManagedCluster(ctx context.Context, clusterClient clusterclient.Interface) error {
	_, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, selfManagedClusterName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := clusterClient.ClusterV1().ManagedClusters().Create(
			ctx,
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: selfManagedClusterName,
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			},
			metav1.CreateOptions{},
		)
		return err
	}

	return err
}

func getAgentNamespace() string {
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return defaultOCMAgentNamespace
	}

	return string(nsBytes)
}
