// Copyright Contributors to the Open Cluster Management project
/*
Copyright 2014 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// package kubeapiserver does all of the work necessary to create a Kubernetes
// APIServer by binding together the API, master and APIServer infrastructure.
// It can be configured and called directly or via the hyperkube framework.
package servers

import (
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/util/notfoundhandler"
	"k8s.io/apiserver/pkg/util/webhook"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	"open-cluster-management.io/multicluster-controlplane/pkg/controllers"
	"open-cluster-management.io/multicluster-controlplane/pkg/controllers/ocmcontroller"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/options"
)

type Server interface {
	AddController(name string, controller controllers.Controller)
	Start() error
}

type server struct {
	aggregatorConfig *aggregatorapiserver.Config
	aggregator       *aggregatorapiserver.APIAggregator
}

func NewServer(options options.ServerRunOptions) *server {
	aggregatorConfig, aggregator, err := createServerChain(options)
	if err != nil {
		klog.Errorf("create server chain err %v", err)
	}
	s := &server{
		aggregator:       aggregator,
		aggregatorConfig: aggregatorConfig,
	}

	s.AddController("multicluster-controlplane-crd", ocmcontroller.InstallCrd)
	s.AddController("multicluster-controlplane-registration-resource", ocmcontroller.InstallHubResource)
	s.AddController("multicluster-controlplane-controllers", ocmcontroller.InstallControllers)
	return s
}

func (s *server) Start(stopCh <-chan struct{}) error {
	klog.Info("starting the server...")
	prepared, err := s.aggregator.PrepareRun()
	if err != nil {
		return err
	}
	return prepared.Run(stopCh)
}

func (s *server) AddController(name string, controller controllers.Controller) {
	if err := s.aggregator.GenericAPIServer.AddPostStartHook(name, func(context genericapiserver.PostStartHookContext) error {
		return controller(context.StopCh, s.aggregatorConfig)
	}); err != nil {
		klog.Errorf("add controller error %v", err)
	}
}

// CreateServerChain creates the apiservers connected via delegation.
func createServerChain(o options.ServerRunOptions) (*aggregatorapiserver.Config, *aggregatorapiserver.APIAggregator, error) {
	kubeAPIServerConfig, serviceResolver, pluginInitializer, err := createKubeAPIServerConfig(o)
	if err != nil {
		return nil, nil, err
	}

	// If additional API servers are added, they should be gated.
	apiExtensionsConfig, err := createAPIExtensionsConfig(
		*kubeAPIServerConfig.GenericConfig,
		kubeAPIServerConfig.ExtraConfig.VersionedInformers,
		pluginInitializer, &o, 1, serviceResolver,
		webhook.NewDefaultAuthenticationInfoResolverWrapper(kubeAPIServerConfig.ExtraConfig.ProxyTransport, kubeAPIServerConfig.GenericConfig.EgressSelector, kubeAPIServerConfig.GenericConfig.LoopbackClientConfig, kubeAPIServerConfig.GenericConfig.TracerProvider))
	if err != nil {
		return nil, nil, err
	}

	notFoundHandler := notfoundhandler.New(kubeAPIServerConfig.GenericConfig.Serializer, genericapifilters.NoMuxAndDiscoveryIncompleteKey)
	apiExtensionsServer, err := createAPIExtensionsServer(apiExtensionsConfig,
		genericapiserver.NewEmptyDelegateWithCustomHandler(notFoundHandler))
	if err != nil {
		return nil, nil, err
	}

	kubeAPIServer, err := createKubeAPIServer(kubeAPIServerConfig, apiExtensionsServer.GenericAPIServer)
	if err != nil {
		return nil, nil, err
	}

	// aggregator comes last in the chain
	aggregatorConfig, err := createAggregatorConfig(*kubeAPIServerConfig.GenericConfig, &o, kubeAPIServerConfig.ExtraConfig.VersionedInformers, serviceResolver, kubeAPIServerConfig.ExtraConfig.ProxyTransport, pluginInitializer)
	if err != nil {
		return nil, nil, err
	}
	aggregatorServer, err := createAggregatorServer(
		aggregatorConfig, kubeAPIServer.GenericAPIServer, apiExtensionsServer.Informers,
		o.Authentication.ClientCert.ClientCA,
		o.ExtraOptions.ClientKeyFile,
	)
	if err != nil {
		return nil, nil, err
	}

	return aggregatorConfig, aggregatorServer, nil
}
