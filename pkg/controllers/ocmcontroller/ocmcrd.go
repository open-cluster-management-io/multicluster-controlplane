// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	"open-cluster-management.io/multicluster-controlplane/pkg/controllers/bootstrap"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

func InstallCRD(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error {
	klog.Info("installing ocm crds")
	apiextensionsClient, err := apiextensionsclient.NewForConfig(aggregatorConfig.GenericConfig.LoopbackClientConfig)
	if err != nil {
		return err
	}
	if err := bootstrap.InstallBaseCRDs(util.GoContext(stopCh), apiextensionsClient); err != nil {
		klog.Errorf("failed to bootstrap OCM CRDs: %v", err)
		// nolint:nilerr
		return nil // don't klog.Fatal. This only happens when context is cancelled.
	}
	klog.Info("installed ocm crds")
	return nil
}
