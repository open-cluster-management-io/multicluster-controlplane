// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	ocmhubresource "open-cluster-management.io/multicluster-controlplane/config/hub"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

func InstallHubResource(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error {
	klog.Info("installing ocm hub resources")
	dynamicClient, err := dynamic.NewForConfig(aggregatorConfig.GenericConfig.LoopbackClientConfig)
	if err != nil {
		return err
	}
	kubeClient, err := kubernetes.NewForConfig(aggregatorConfig.GenericConfig.LoopbackClientConfig)
	if err != nil {
		return err
	}
	// bootstrap ocm hub resources
	if err := ocmhubresource.Bootstrap(
		util.GoContext(stopCh),
		aggregatorConfig.GenericConfig.Config,
		kubeClient.Discovery(),
		dynamicClient,
		kubeClient,
	); err != nil {
		klog.Errorf("failed to bootstrap ocm hub controller resources: %v", err)
		// nolint:nilerr
		return nil // don't klog.Fatal. This only happens when context is cancelled.
	}
	klog.Infof("installed ocm hub resources")
	return nil
}
