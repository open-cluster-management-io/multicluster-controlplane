// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	ocmhubresource "open-cluster-management.io/multicluster-controlplane/config/hub"
)

func InstallHubResource(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error {
	klog.Info("installing ocm hub resources")
	apiextensionsClient, err := apiextensionsclient.NewForConfig(aggregatorConfig.GenericConfig.LoopbackClientConfig)
	if err != nil {
		return err
	}
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
		GoContext(stopCh),
		aggregatorConfig.GenericConfig.Config,
		apiextensionsClient.Discovery(),
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
