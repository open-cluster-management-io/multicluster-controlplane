// Copyright Contributors to the Open Cluster Management project
package servers

// refer to https://github.com/kubernetes/kubernetes/blob/{kubernetes-version}/cmd/kube-apiserver/app/server.go

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	cacheddiscovery "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/restmapper"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingapiv1 "k8s.io/api/autoscaling/v1"
	autoscalingapiv2 "k8s.io/api/autoscaling/v2"
	batchapiv1 "k8s.io/api/batch/v1"
	networkingapiv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	policyapiv1 "k8s.io/api/policy/v1"
	schedulingapiv1 "k8s.io/api/scheduling/v1"
	extensionsapiserver "k8s.io/apiextensions-apiserver/pkg/apiserver"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/endpoints/discovery/aggregated"
	openapinamer "k8s.io/apiserver/pkg/endpoints/openapi"
	genericfeatures "k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/egressselector"
	"k8s.io/apiserver/pkg/server/filters"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	utilflowcontrol "k8s.io/apiserver/pkg/util/flowcontrol"
	"k8s.io/apiserver/pkg/util/openapi"
	"k8s.io/apiserver/pkg/util/webhook"
	"k8s.io/client-go/dynamic"
	clientgoinformers "k8s.io/client-go/informers"
	clientgoclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/keyutil"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"
	aggregatorscheme "k8s.io/kube-aggregator/pkg/apiserver/scheme"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/capabilities"
	"k8s.io/kubernetes/pkg/controlplane"
	controlplaneapiserver "k8s.io/kubernetes/pkg/controlplane/apiserver"
	controlplaneadmission "k8s.io/kubernetes/pkg/controlplane/apiserver/admission"
	"k8s.io/kubernetes/pkg/controlplane/reconcilers"
	generatedopenapi "k8s.io/kubernetes/pkg/generated/openapi"
	"k8s.io/kubernetes/pkg/kubeapiserver"
	"k8s.io/kubernetes/pkg/kubeapiserver/authorizer/modes"
	rbacrest "k8s.io/kubernetes/pkg/registry/rbac/rest"
	"k8s.io/kubernetes/pkg/serviceaccount"

	"open-cluster-management.io/multicluster-controlplane/pkg/servers/options"
)

// CreateKubeAPIServer creates and wires a workable kube-apiserver
func createKubeAPIServer(kubeAPIServerConfig *controlplane.Config, delegateAPIServer genericapiserver.DelegationTarget) (*controlplane.Instance, error) {
	kubeAPIServer, err := kubeAPIServerConfig.Complete().New(delegateAPIServer)
	if err != nil {
		return nil, err
	}
	return kubeAPIServer, nil
}

// createKubeAPIServerConfig creates all the resources for running the API server, but runs none of them
func createKubeAPIServerConfig(options options.ServerRunOptions) (
	*controlplane.Config,
	aggregatorapiserver.ServiceResolver,
	[]admission.PluginInitializer,
	error,
) {
	proxyTransport := CreateProxyTransport()

	genericConfig, versionedInformers, serviceResolver, pluginInitializers, storageFactory, err := buildGenericConfig(&options, proxyTransport)
	if err != nil {
		return nil, nil, nil, err
	}

	capabilities.Setup(options.AllowPrivileged, options.MaxConnectionBytesPerSec)

	options.Metrics.Apply()
	serviceaccount.RegisterMetrics()

	config := &controlplane.Config{
		ControlPlane: controlplaneapiserver.Config{
			Generic: genericConfig,
			Extra: controlplaneapiserver.Extra{
				APIResourceConfigSource:     storageFactory.APIResourceConfigSource,
				StorageFactory:              storageFactory,
				EventTTL:                    options.EventTTL,
				EnableLogsSupport:           true,
				ProxyTransport:              proxyTransport,
				ServiceAccountIssuer:        options.ServiceAccountIssuer,
				ServiceAccountMaxExpiration: options.ServiceAccountTokenMaxExpiration,
				ExtendExpiration:            options.Authentication.ServiceAccounts.ExtendExpiration,
				VersionedInformers:          versionedInformers,
			},
		},
		Extra: controlplane.Extra{
			KubeletClientConfig:     options.KubeletConfig,
			APIServerServiceIP:      options.APIServerServiceIP,
			APIServerServicePort:    443,
			ServiceIPRange:          options.PrimaryServiceClusterIPRange,
			SecondaryServiceIPRange: options.SecondaryServiceClusterIPRange,
			EndpointReconcilerType:  reconcilers.Type(options.EndpointReconcilerType),
			MasterCount:             1,
		},
	}

	clientCAProvider, err := options.Authentication.ClientCert.GetClientCAContentProvider()
	if err != nil {
		return nil, nil, nil, err
	}
	config.ControlPlane.ClusterAuthenticationInfo.ClientCA = clientCAProvider

	requestHeaderConfig, err := options.Authentication.RequestHeader.ToAuthenticationRequestHeaderConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	if requestHeaderConfig != nil {
		config.ControlPlane.ClusterAuthenticationInfo.RequestHeaderCA = requestHeaderConfig.CAContentProvider
		config.ControlPlane.ClusterAuthenticationInfo.RequestHeaderAllowedNames = requestHeaderConfig.AllowedClientNames
		config.ControlPlane.ClusterAuthenticationInfo.RequestHeaderExtraHeaderPrefixes = requestHeaderConfig.ExtraHeaderPrefixes
		config.ControlPlane.ClusterAuthenticationInfo.RequestHeaderGroupHeaders = requestHeaderConfig.GroupHeaders
		config.ControlPlane.ClusterAuthenticationInfo.RequestHeaderUsernameHeaders = requestHeaderConfig.UsernameHeaders
	}

	clientgoExternalClient, err := clientgoclientset.NewForConfig(genericConfig.LoopbackClientConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create real client-go external client: %w", err)
	}
	discoveryClient := cacheddiscovery.NewMemCacheClient(clientgoExternalClient.Discovery())
	discoveryRESTMapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)

	admissionPostStartHook := func(context genericapiserver.PostStartHookContext) error {
		discoveryRESTMapper.Reset()
		go wait.Until(discoveryRESTMapper.Reset, 30*time.Second, context.Done())
		return nil
	}

	if err := config.ControlPlane.Generic.AddPostStartHook("start-kube-apiserver-admission-initializer", admissionPostStartHook); err != nil {
		return nil, nil, nil, err
	}

	if config.ControlPlane.Generic.EgressSelector != nil {
		// Use the config.GenericConfig.EgressSelector lookup to find the dialer to connect to the kubelet
		config.Extra.KubeletClientConfig.Lookup = config.ControlPlane.Generic.EgressSelector.Lookup

		// Use the config.GenericConfig.EgressSelector lookup as the transport used by the "proxy" subresources.
		networkContext := egressselector.Cluster.AsNetworkContext()
		dialer, err := config.ControlPlane.Generic.EgressSelector.Lookup(networkContext)
		if err != nil {
			return nil, nil, nil, err
		}
		c := proxyTransport.Clone()
		c.DialContext = dialer
		config.ControlPlane.ProxyTransport = c
	}

	if len(options.Authentication.ServiceAccounts.KeyFiles) > 0 {
		// Load and set the public keys.
		var pubKeys []interface{}
		for _, f := range options.Authentication.ServiceAccounts.KeyFiles {
			keys, err := keyutil.PublicKeysFromFile(f)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to parse key file %q: %w", f, err)
			}
			pubKeys = append(pubKeys, keys...)
		}
		keysGetter, err := serviceaccount.StaticPublicKeysGetter(pubKeys)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to set up public service account keys: %w", err)
		}
		config.ControlPlane.ServiceAccountPublicKeysGetter = keysGetter
	}
	// Plumb the required metadata through ExtraConfig.
	config.ControlPlane.ServiceAccountIssuerURL = options.Authentication.ServiceAccounts.Issuers[0]
	config.ControlPlane.ServiceAccountJWKSURI = options.Authentication.ServiceAccounts.JWKSURI

	return config, serviceResolver, pluginInitializers, nil
}

// BuildGenericConfig takes the master server options and produces the genericapiserver.Config associated with it
func buildGenericConfig(
	options *options.ServerRunOptions,
	proxyTransport *http.Transport,
) (
	genericConfig *genericapiserver.Config,
	versionedInformers clientgoinformers.SharedInformerFactory,
	serviceResolver aggregatorapiserver.ServiceResolver,
	pluginInitializers []admission.PluginInitializer,
	storageFactory *serverstorage.DefaultStorageFactory,
	lastErr error,
) {
	genericConfig = genericapiserver.NewConfig(legacyscheme.Codecs)
	genericConfig.MergedResourceConfig = controlplane.DefaultAPIResourceConfigSource()

	if lastErr = options.GenericServerRunOptions.ApplyTo(genericConfig); lastErr != nil {
		return
	}

	if lastErr = options.SecureServing.ApplyTo(&genericConfig.SecureServing, &genericConfig.LoopbackClientConfig); lastErr != nil {
		return
	}

	kubeClientConfig := genericConfig.LoopbackClientConfig
	clientgoExternalClient, err := clientgoclientset.NewForConfig(kubeClientConfig)
	if err != nil {
		lastErr = fmt.Errorf("failed to create real external clientset: %v", err)
		return
	}
	versionedInformers = clientgoinformers.NewSharedInformerFactory(clientgoExternalClient, 10*time.Minute)

	if lastErr = options.Features.ApplyTo(genericConfig, clientgoExternalClient, versionedInformers); lastErr != nil {
		return
	}
	if lastErr = options.APIEnablement.ApplyTo(genericConfig, getAPIResourceConfig(), legacyscheme.Scheme); lastErr != nil {
		return
	}
	if lastErr = options.EgressSelector.ApplyTo(genericConfig); lastErr != nil {
		return
	}
	if utilfeature.DefaultFeatureGate.Enabled(genericfeatures.APIServerTracing) {
		if lastErr = options.Traces.ApplyTo(genericConfig.EgressSelector, genericConfig); lastErr != nil {
			return
		}
	}

	// wrap the definitions to revert any changes from disabled features
	getOpenAPIDefinitions := openapi.GetOpenAPIDefinitionsWithoutDisabledFeatures(generatedopenapi.GetOpenAPIDefinitions)
	genericConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(
		getOpenAPIDefinitions,
		openapinamer.NewDefinitionNamer(legacyscheme.Scheme, extensionsapiserver.Scheme, aggregatorscheme.Scheme))
	genericConfig.OpenAPIConfig.Info.Title = "Kubernetes"
	genericConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(getOpenAPIDefinitions, openapinamer.NewDefinitionNamer(legacyscheme.Scheme, extensionsapiserver.Scheme, aggregatorscheme.Scheme))
	genericConfig.OpenAPIV3Config.Info.Title = "Kubernetes"

	genericConfig.LongRunningFunc = filters.BasicLongRunningRequestCheck(
		sets.NewString("watch", "proxy"),
		sets.NewString("attach", "exec", "proxy", "log", "portforward"),
	)

	if lastErr = options.Etcd.ApplyTo(genericConfig); lastErr != nil {
		return
	}

	storageFactoryConfig := kubeapiserver.NewStorageFactoryConfig()
	storageFactoryConfig.APIResourceConfig = genericConfig.MergedResourceConfig
	storageFactory, lastErr = storageFactoryConfig.Complete(options.Etcd).New()
	if lastErr != nil {
		return
	}

	if genericConfig.EgressSelector != nil {
		storageFactory.StorageConfig.Transport.EgressLookup = genericConfig.EgressSelector.Lookup
	}
	if utilfeature.DefaultFeatureGate.Enabled(genericfeatures.APIServerTracing) && genericConfig.TracerProvider != nil {
		storageFactory.StorageConfig.Transport.TracerProvider = genericConfig.TracerProvider
	}
	if lastErr = options.Etcd.ApplyWithStorageFactoryTo(storageFactory, genericConfig); lastErr != nil {
		return
	}

	// Use protobufs for self-communication.
	// Since not every generic apiserver has to support protobufs, we
	// cannot default to it in generic apiserver and need to explicitly
	// set it in kube-apiserver.
	genericConfig.LoopbackClientConfig.ContentConfig.ContentType = "application/vnd.kubernetes.protobuf"
	// Disable compression for self-communication, since we are going to be
	// on a fast local network
	genericConfig.LoopbackClientConfig.DisableCompression = true

	// Authentication.ApplyTo requires already applied OpenAPIConfig and EgressSelector if present
	if lastErr = options.Authentication.ApplyTo(&genericConfig.Authentication, genericConfig.SecureServing, genericConfig.EgressSelector,
		genericConfig.OpenAPIConfig, genericConfig.OpenAPIV3Config, clientgoExternalClient, versionedInformers); lastErr != nil {
		return
	}

	ctx := wait.ContextForChannel(genericConfig.DrainedNotify())

	authorizationConfig := options.Authorization.ToAuthorizationConfig(versionedInformers)
	if genericConfig.EgressSelector != nil {
		egressDialer, err := genericConfig.EgressSelector.Lookup(egressselector.ControlPlane.AsNetworkContext())
		if err != nil {
			lastErr = fmt.Errorf("invalid egress controlplane network config: %v", err)
			return
		}
		authorizationConfig.CustomDial = egressDialer
	}
	genericConfig.Authorization.Authorizer, genericConfig.RuleResolver, err = authorizationConfig.New(ctx, genericConfig.APIServerID)
	if err != nil {
		lastErr = fmt.Errorf("invalid authorization config: %v", err)
		return
	}
	if !sets.NewString(options.Authorization.Modes...).Has(modes.ModeRBAC) {
		genericConfig.DisabledPostStartHooks.Insert(rbacrest.PostStartHookName)
	}

	lastErr = options.Audit.ApplyTo(genericConfig)
	if lastErr != nil {
		return
	}

	// setup admission
	genericAdmissionConfig := controlplaneadmission.Config{
		ExternalInformers:    versionedInformers,
		LoopbackClientConfig: genericConfig.LoopbackClientConfig,
	}
	serviceResolver = buildServiceResolver(options.EnableAggregatorRouting, genericConfig.LoopbackClientConfig.Host, versionedInformers)
	genericInitializers, err := genericAdmissionConfig.New(proxyTransport, genericConfig.EgressSelector, serviceResolver, genericConfig.TracerProvider)
	if err != nil {
		lastErr = fmt.Errorf("failed to create admission plugin initializer: %w", err)
		return
	}
	dynamicExternalClient, err := dynamic.NewForConfig(genericConfig.LoopbackClientConfig)
	if err != nil {
		lastErr = fmt.Errorf("failed to create real dynamic external client: %w", err)
		return
	}
	err = options.Admission.ApplyTo(
		genericConfig,
		versionedInformers,
		clientgoExternalClient,
		dynamicExternalClient,
		utilfeature.DefaultFeatureGate,
		genericInitializers...,
	)
	if err != nil {
		lastErr = fmt.Errorf("failed to apply admission: %w", err)
		return
	}

	if utilfeature.DefaultFeatureGate.Enabled(genericfeatures.AggregatedDiscoveryEndpoint) {
		genericConfig.AggregatedDiscoveryGroupManager = aggregated.NewResourceManager("apis")
	}
	return
}

// BuildPriorityAndFairness constructs the guts of the API Priority and Fairness filter
func BuildPriorityAndFairness(serverRunOptions *genericoptions.ServerRunOptions, extclient clientgoclientset.Interface, versionedInformer clientgoinformers.SharedInformerFactory) (utilflowcontrol.Interface, error) {
	if serverRunOptions.MaxRequestsInFlight+serverRunOptions.MaxMutatingRequestsInFlight <= 0 {
		return nil, fmt.Errorf("invalid configuration: MaxRequestsInFlight=%d and MaxMutatingRequestsInFlight=%d; they must add up to something positive", serverRunOptions.MaxRequestsInFlight, serverRunOptions.MaxMutatingRequestsInFlight)
	}
	return utilflowcontrol.New(
		versionedInformer,
		extclient.FlowcontrolV1(),
		serverRunOptions.MaxRequestsInFlight+serverRunOptions.MaxMutatingRequestsInFlight,
	), nil
}

func buildServiceResolver(enabledAggregatorRouting bool, hostname string, informer clientgoinformers.SharedInformerFactory) webhook.ServiceResolver {
	var serviceResolver webhook.ServiceResolver
	if enabledAggregatorRouting {
		serviceResolver = aggregatorapiserver.NewEndpointServiceResolver(
			informer.Core().V1().Services().Lister(),
			informer.Core().V1().Endpoints().Lister(),
		)
	} else {
		serviceResolver = aggregatorapiserver.NewClusterIPServiceResolver(
			informer.Core().V1().Services().Lister(),
		)
	}
	// resolve kubernetes.default.svc locally
	if localHost, err := url.Parse(hostname); err == nil {
		serviceResolver = aggregatorapiserver.NewLoopbackServiceResolver(serviceResolver, localHost)
	}
	return serviceResolver
}

// CreateProxyTransport creates the dialer infrastructure to connect to the nodes.
func CreateProxyTransport() *http.Transport {
	var proxyDialerFn utilnet.DialFunc
	// Proxying to pods and services is IP-based... don't expect to be able to verify the hostname
	proxyTLSClientConfig := &tls.Config{InsecureSkipVerify: true}
	proxyTransport := utilnet.SetTransportDefaults(&http.Transport{
		DialContext:     proxyDialerFn,
		TLSClientConfig: proxyTLSClientConfig,
	})
	return proxyTransport
}

func getAPIResourceConfig() *serverstorage.ResourceConfig {
	resourceConfig := controlplane.DefaultAPIResourceConfigSource()
	resourceConfig.DisableVersions(appsv1.SchemeGroupVersion,
		autoscalingapiv1.SchemeGroupVersion,
		autoscalingapiv2.SchemeGroupVersion,
		batchapiv1.SchemeGroupVersion,
		networkingapiv1.SchemeGroupVersion,
		nodev1.SchemeGroupVersion,
		policyapiv1.SchemeGroupVersion,
		schedulingapiv1.SchemeGroupVersion)
	return resourceConfig
}
