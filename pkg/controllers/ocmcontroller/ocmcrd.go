// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"context"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	ocmcrds "open-cluster-management.io/multicluster-controlplane/config/crds"
)

func InstallCrd(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error {
	klog.Info("installing ocm crds")
	apiextensionsClient, err := apiextensionsclient.NewForConfig(aggregatorConfig.GenericConfig.LoopbackClientConfig)
	if err != nil {
		return err
	}
	if err := ocmcrds.Bootstrap(GoContext(stopCh), apiextensionsClient); err != nil {
		klog.Errorf("failed to bootstrap OCM CRDs: %v", err)
		// nolint:nilerr
		return nil // don't klog.Fatal. This only happens when context is cancelled.
	}
	klog.Info("installed ocm crds")
	return nil
}

func GoContext(stopCh <-chan struct{}) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func(done <-chan struct{}) {
		<-done
		cancel()
	}(stopCh)
	return ctx
}
