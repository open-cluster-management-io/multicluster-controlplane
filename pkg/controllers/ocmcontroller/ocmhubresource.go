// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	"open-cluster-management.io/multicluster-controlplane/pkg/controllers/bootstrap"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

func InstallHubResource(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error {
	klog.Info("installing ocm hub resources")
	kubeClient, err := kubernetes.NewForConfig(aggregatorConfig.GenericConfig.LoopbackClientConfig)
	if err != nil {
		return err
	}

	// bootstrap ocm hub resources
	if err := bootstrap.BuildKubeSystemResources(
		util.GoContext(stopCh),
		aggregatorConfig.GenericConfig.Config,
		kubeClient,
	); err != nil {
		klog.Errorf("failed to bootstrap ocm hub controller resources: %v", err)
		// nolint:nilerr
		return nil // don't klog.Fatal. This only happens when context is cancelled.
	}
	klog.Infof("installed ocm hub resources")
	return nil
}
