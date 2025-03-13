// Copyright Contributors to the Open Cluster Management project
package servers

import (
	"context"
	"fmt"

	"k8s.io/apiserver/pkg/endpoints/discovery/aggregated"
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	genericfeatures "k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/apiserver/pkg/util/notfoundhandler"
	"k8s.io/apiserver/pkg/util/webhook"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	"open-cluster-management.io/multicluster-controlplane/pkg/controllers"
	"open-cluster-management.io/multicluster-controlplane/pkg/controllers/ocmcontroller"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/options"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
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
		klog.Fatal(err)
	}
	s := &server{
		aggregator:       aggregator,
		aggregatorConfig: aggregatorConfig,
	}

	s.AddController("multicluster-controlplane-crd", ocmcontroller.InstallCRD)
	s.AddController("multicluster-controlplane-registration-resource", ocmcontroller.InstallHubResource)
	s.AddController("multicluster-controlplane-controllers", ocmcontroller.InstallControllers(options))
	s.AddController("multicluster-controlplane-selfmanagement", ocmcontroller.InstallSelfManagementCluster(options))
	if options.Authentication.DelegatingAuthenticatorConfig != nil {
		s.AddController("multicluster-controlplane-authentication-delegator",
			func(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error {
				options.Authentication.DelegatingAuthenticatorConfig.Start(util.GoContext(stopCh))
				return nil
			})
	}
	return s
}

func (s *server) Start(ctx context.Context) error {
	klog.Info("starting the server...")
	prepared, err := s.aggregator.PrepareRun()
	if err != nil {
		return err
	}
	return prepared.Run(ctx)
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
	kubeAPIServerConfig, serviceResolver, _, err := createKubeAPIServerConfig(o)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kubeapi server config, %v", err)
	}

	// If additional API servers are added, they should be gated.
	apiExtensionsConfig, err := createAPIExtensionsConfig(
		*kubeAPIServerConfig.ControlPlane.Generic,
		kubeAPIServerConfig.ControlPlane.VersionedInformers,
		&o, 1, serviceResolver,
		webhook.NewDefaultAuthenticationInfoResolverWrapper(kubeAPIServerConfig.ControlPlane.ProxyTransport, kubeAPIServerConfig.ControlPlane.Generic.EgressSelector, kubeAPIServerConfig.ControlPlane.Generic.LoopbackClientConfig, kubeAPIServerConfig.ControlPlane.Generic.TracerProvider))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create apiextensions config, %v", err)
	}

	notFoundHandler := notfoundhandler.New(kubeAPIServerConfig.ControlPlane.Generic.Serializer, genericapifilters.NoMuxAndDiscoveryIncompleteKey)
	apiExtensionsServer, err := createAPIExtensionsServer(apiExtensionsConfig,
		genericapiserver.NewEmptyDelegateWithCustomHandler(notFoundHandler))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create apiextensions server, %v", err)
	}

	kubeAPIServer, err := createKubeAPIServer(kubeAPIServerConfig, apiExtensionsServer.GenericAPIServer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kubeapi server, %v", err)
	}

	if utilfeature.DefaultFeatureGate.Enabled(genericfeatures.AggregatedDiscoveryEndpoint) {
		manager := kubeAPIServer.ControlPlane.GenericAPIServer.AggregatedDiscoveryGroupManager
		if manager == nil {
			manager = aggregated.NewResourceManager("apis")
		}
		kubeAPIServer.ControlPlane.GenericAPIServer.AggregatedDiscoveryGroupManager = manager
		kubeAPIServer.ControlPlane.GenericAPIServer.AggregatedLegacyDiscoveryGroupManager = aggregated.NewResourceManager("api")
	}

	// aggregator comes last in the chain
	aggregatorConfig, err := createAggregatorConfig(*kubeAPIServerConfig.ControlPlane.Generic, &o, kubeAPIServerConfig.ControlPlane.VersionedInformers, serviceResolver, kubeAPIServerConfig.ControlPlane.ProxyTransport)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create aggregator config, %v", err)
	}
	aggregatorServer, err := createAggregatorServer(
		aggregatorConfig, kubeAPIServer.ControlPlane.GenericAPIServer, apiExtensionsServer.Informers,
		o.Authentication.ClientCert.ClientCA,
		o.ExtraOptions.ClientKeyFile,
		o.KubeControllerManagerOptions,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create aggregator server, %v", err)
	}

	return aggregatorConfig, aggregatorServer, nil
}
