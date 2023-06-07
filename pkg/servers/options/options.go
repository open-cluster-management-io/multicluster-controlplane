// Copyright Contributors to the Open Cluster Management project
/*
Copyright 2017 The Kubernetes Authors.

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

package options

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	// add the kubernetes feature gates
	_ "k8s.io/kubernetes/pkg/features"

	apiextensionsapiserver "k8s.io/apiextensions-apiserver/pkg/apiserver"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/metrics"
	"k8s.io/klog/v2"
	aggregatorscheme "k8s.io/kube-aggregator/pkg/apiserver/scheme"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/controlplane"
	"k8s.io/kubernetes/pkg/controlplane/reconcilers"
	"k8s.io/kubernetes/pkg/kubeapiserver"
	kubeauthenticator "k8s.io/kubernetes/pkg/kubeapiserver/authenticator"
	kubeletclient "k8s.io/kubernetes/pkg/kubelet/client"
	"k8s.io/kubernetes/pkg/serviceaccount"
	netutils "k8s.io/utils/net"

	kubectrmgroptions "open-cluster-management.io/multicluster-controlplane/pkg/controllers/kubecontroller/options"
	controlplanefeatures "open-cluster-management.io/multicluster-controlplane/pkg/features"

	"open-cluster-management.io/multicluster-controlplane/pkg/certificate"
	"open-cluster-management.io/multicluster-controlplane/pkg/etcd"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/configs"
)

// ServerRunOptions runs a kubernetes api server.
type ServerRunOptions struct {
	GenericServerRunOptions *genericoptions.ServerRunOptions
	Etcd                    *genericoptions.EtcdOptions
	SecureServing           *genericoptions.SecureServingOptionsWithLoopback
	Audit                   *genericoptions.AuditOptions
	Features                *genericoptions.FeatureOptions
	Traces                  *genericoptions.TracingOptions
	APIEnablement           *genericoptions.APIEnablementOptions
	EgressSelector          *genericoptions.EgressSelectorOptions

	Admission      *AdmissionOptions
	Authentication *BuiltInAuthenticationOptions
	Authorization  *BuiltInAuthorizationOptions

	ServiceClusterIPRanges string // ServiceClusterIPRange is mapped to input provided by user
	// PrimaryServiceClusterIPRange and SecondaryServiceClusterIPRange are the results
	// of parsing ServiceClusterIPRange into actual values
	PrimaryServiceClusterIPRange   net.IPNet
	APIServerServiceIP             net.IP // APIServerServiceIP is the first valid IP from PrimaryServiceClusterIPRange
	SecondaryServiceClusterIPRange net.IPNet

	Metrics                           *metrics.Options
	Logs                              *logs.Options
	EventTTL                          time.Duration
	IdentityLeaseDurationSeconds      int
	IdentityLeaseRenewIntervalSeconds int
	EndpointReconcilerType            string

	EnableAggregatorRouting  bool
	AllowPrivileged          bool
	MaxConnectionBytesPerSec int64

	ServiceAccountSigningKeyFile     string
	ServiceAccountIssuer             serviceaccount.TokenGenerator
	ServiceAccountTokenMaxExpiration time.Duration

	KubeletConfig kubeletclient.KubeletClientConfig
	ExtraOptions  *ExtraOptions

	KubeControllerManagerOptions *kubectrmgroptions.KubeControllerManagerOptions

	// ControlplaneConfigDir contains minimum requried configurations for server
	ControlplaneConfigDir string
	// ControlplaneDataDir is used for saving controlplane data
	ControlplaneDataDir string

	// EnableSelfManagement register the current cluster self as a managed cluster
	EnableSelfManagement bool
	// SelfManagementClusterName is the name of self management cluster, by default, it's local-cluster
	SelfManagementClusterName string

	// ClusterAutoApprovalUsers is a bootstrap user list whose cluster registration requests can be automatically approved
	ClusterAutoApprovalUsers []string

	// EnableDelegatingAuthentication delegate the authentication with controlplane hosing cluster
	EnableDelegatingAuthentication bool
}

type ExtraOptions struct {
	EmbeddedEtcd  *EmbeddedEtcd
	ClientKeyFile string
}

// NewOptions creates a new Options with default parameters
func NewServerRunOptions() *ServerRunOptions {
	etcdOptions := genericoptions.NewEtcdOptions(storagebackend.NewDefaultConfig("/registry", nil))
	etcdOptions.DefaultStorageMediaType = "application/vnd.kubernetes.protobuf" // Overwrite default storage data format.

	secureServingOptions := genericoptions.SecureServingOptions{
		BindAddress: netutils.ParseIPSloppy("0.0.0.0"),
		BindPort:    6443,
		Required:    true,
		ServerCert: genericoptions.GeneratableKeyCert{
			PairName:      "apiserver",
			CertDirectory: "/var/run/kubernetes",
		},
	}

	kubeControllerManagerOptions, err := kubectrmgroptions.NewKubeControllerManagerOptions()
	if err != nil {
		klog.Fatalf("unable to initialize kube controller manager options: %v", err)
	}

	// set default flag values

	// --enable-priority-and-fairness="false"
	genericServerRunOptions := genericoptions.NewServerRunOptions()
	genericServerRunOptions.EnablePriorityAndFairness = false

	// --storage-backend="etcd3"
	etcdOptions.StorageConfig.Type = "etcd3"

	// --bind-address="0.0.0.0"
	secureServing := secureServingOptions.WithLoopback()
	secureServing.BindAddress = netutils.ParseIPSloppy("0.0.0.0")

	// --profiling=false
	features := genericoptions.NewFeatureOptions()
	features.EnableProfiling = false

	// --enable-admission-plugins
	// --disable-admission-plugins=""
	admission := NewAdmissionOptions()
	admission.GenericAdmission.EnablePlugins = []string{
		"NamespaceLifecycle",
		"ServiceAccount",
		"MutatingAdmissionWebhook",
		"ValidatingAdmissionWebhook",
		"ResourceQuota",
		"ManagedClusterMutating",
		"ManagedClusterValidating",
		"ManagedClusterSetBindingValidating",
	}
	admission.GenericAdmission.DisablePlugins = []string{}

	// --api-audiences=""
	// --enable-bootstrap-token-auth
	// --service-account-issuer="https://kubernetes.default.svc"
	// --service-account-lookup=true
	authentication := NewBuiltInAuthenticationOptions().WithAll()
	authentication.APIAudiences = []string{}
	authentication.BootstrapToken.Enable = true
	authentication.ServiceAccounts.Issuers = []string{"https://kubernetes.default.svc"}
	authentication.ServiceAccounts.Lookup = true

	// --authorization-mode=RBAC
	authorization := NewBuiltInAuthorizationOptions()
	authorization.Modes = []string{"RBAC"}

	return &ServerRunOptions{
		GenericServerRunOptions: genericServerRunOptions,
		Etcd:                    etcdOptions,
		SecureServing:           secureServing,
		Audit:                   genericoptions.NewAuditOptions(),
		Features:                genericoptions.NewFeatureOptions(),
		Traces:                  genericoptions.NewTracingOptions(),
		APIEnablement:           genericoptions.NewAPIEnablementOptions(),
		EgressSelector:          genericoptions.NewEgressSelectorOptions(),

		Admission:      admission,
		Authentication: authentication,
		Authorization:  authorization,

		Metrics:                           metrics.NewOptions(),
		Logs:                              logs.NewOptions(),
		EventTTL:                          1 * time.Hour,
		IdentityLeaseDurationSeconds:      3600,
		IdentityLeaseRenewIntervalSeconds: 10,
		EndpointReconcilerType:            string(reconcilers.LeaseEndpointReconcilerType),

		// this is fake config, just to let server start
		KubeletConfig: kubeletclient.KubeletClientConfig{
			Port:         10250,
			ReadOnlyPort: 10255,
			PreferredAddressTypes: []string{
				"Hostname",
				"InternalDNS",
				"InternalIP",
				"ExternalDNS",
				"ExternalIP",
			},
			HTTPTimeout: time.Duration(5) * time.Second,
		},

		ExtraOptions: &ExtraOptions{
			EmbeddedEtcd: NewEmbeddedEtcd(),
		},

		KubeControllerManagerOptions: kubeControllerManagerOptions,

		ServiceClusterIPRanges: "10.0.0.0/24",

		ControlplaneConfigDir: "/controlplane_config",
	}
}

func (options *ServerRunOptions) AddFlags(fs *pflag.FlagSet) {
	controlplanefeatures.DefaultControlplaneMutableFeatureGate.AddFlag(fs)
	fs.StringVar(&options.ControlplaneConfigDir, "controlplane-config-dir", options.ControlplaneConfigDir,
		"Path to the file directory contains minimum requried configurations for controlplane server.")
	fs.BoolVar(&options.EnableSelfManagement, "self-management", options.EnableSelfManagement,
		"Register the current controlplane as a self managed cluster.")
	fs.StringVar(&options.SelfManagementClusterName, "self-management-cluster-name", options.SelfManagementClusterName,
		"Name of the self managed cluster name.")
	fs.StringArrayVar(&options.ClusterAutoApprovalUsers, "cluster-auto-approval-users", options.ClusterAutoApprovalUsers,
		"A bootstrap user list whose cluster registration requests can be automatically approved.")
	fs.BoolVar(&options.EnableDelegatingAuthentication, "delegating-authentication", options.EnableDelegatingAuthentication,
		"Delegate authentication to the controlplane hosting cluster.")
}

// Complete set default Options.
// Should be called after kube-apiserver flags parsed.
func (s *ServerRunOptions) Complete(stopCh <-chan struct{}) error {
	for name := range utilfeature.DefaultMutableFeatureGate.GetAll() {
		klog.Infof("kube-apiserver feature %s is %v", name, utilfeature.DefaultMutableFeatureGate.Enabled(name))
	}

	// Load configurations from config file
	config, err := configs.LoadConfig(s.ControlplaneConfigDir)
	if err != nil {
		return err
	}

	if err := s.InitServerRunOptions(config); err != nil {
		return err
	}

	// GenericServerRunOptions
	if err := s.GenericServerRunOptions.DefaultAdvertiseAddress(s.SecureServing.SecureServingOptions); err != nil {
		return err
	}

	// ServiceClusterIPRange
	serviceClusterIPRangeList := []string{}
	if s.ServiceClusterIPRanges != "" {
		serviceClusterIPRangeList = strings.Split(s.ServiceClusterIPRanges, ",")
	}
	if len(serviceClusterIPRangeList) == 0 {
		var primaryServiceClusterCIDR net.IPNet
		var err error
		if s.PrimaryServiceClusterIPRange, s.APIServerServiceIP, err = controlplane.ServiceIPRange(primaryServiceClusterCIDR); err != nil {
			return fmt.Errorf("error determining service IP ranges: %v", err)
		}
		s.SecondaryServiceClusterIPRange = net.IPNet{}
	}
	_, primaryServiceClusterCIDR, err := netutils.ParseCIDRSloppy(serviceClusterIPRangeList[0])
	if err != nil {
		return fmt.Errorf("service-cluster-ip-range[0] is not a valid cidr")
	}
	if s.PrimaryServiceClusterIPRange, s.APIServerServiceIP, err = controlplane.ServiceIPRange(*primaryServiceClusterCIDR); err != nil {
		return fmt.Errorf("error determining service IP ranges for primary service cidr: %v", err)
	}
	// user provided at least two entries
	// note: validation asserts that the list is max of two dual stack entries
	if len(serviceClusterIPRangeList) > 1 {
		_, secondaryServiceClusterCIDR, err := netutils.ParseCIDRSloppy(serviceClusterIPRangeList[1])
		if err != nil {
			return fmt.Errorf("service-cluster-ip-range[1] is not an ip net")
		}
		s.SecondaryServiceClusterIPRange = *secondaryServiceClusterCIDR
	}

	// SecureServing signed certs
	if err := s.SecureServing.MaybeDefaultWithSelfSignedCerts(
		s.GenericServerRunOptions.AdvertiseAddress.String(),
		[]string{"kubernetes.default.svc", "kubernetes.default", "kubernetes"},
		[]net.IP{s.APIServerServiceIP}); err != nil {
		return fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	// externalHost
	if len(s.GenericServerRunOptions.ExternalHost) == 0 {
		if len(s.GenericServerRunOptions.AdvertiseAddress) > 0 {
			s.GenericServerRunOptions.ExternalHost = s.GenericServerRunOptions.AdvertiseAddress.String()
		} else {
			if hostname, err := os.Hostname(); err == nil {
				s.GenericServerRunOptions.ExternalHost = hostname
			} else {
				return fmt.Errorf("error finding host name: %v", err)
			}
		}
		klog.Infof("external host was not specified, using %v", s.GenericServerRunOptions.ExternalHost)
	}

	if s.EnableDelegatingAuthentication {
		s.Authentication.DelegatingAuthenticatorConfig = &DelegatingAuthenticatorConfig{
			// very low for responsiveness, but high enough to handle storms
			CacheTTL:                 10 * time.Second,
			WebhookRetryBackoff:      genericoptions.DefaultAuthWebhookRetryBackoff(),
			TokenAccessReviewTimeout: 10 * time.Second,
		}
	}

	// authorization
	s.Authentication.ApplyAuthorization(s.Authorization)

	// Use (ServiceAccountSigningKeyFile != "") as a proxy to the user enabling
	// TokenRequest functionality. This defaulting was convenient, but messed up
	// a lot of people when they rotated their serving cert with no idea it was
	// connected to their service account keys. We are taking this opportunity to
	// remove this problematic defaulting.
	if s.ServiceAccountSigningKeyFile == "" {
		// Default to the private server key for service account token signing
		if len(s.Authentication.ServiceAccounts.KeyFiles) == 0 && s.SecureServing.ServerCert.CertKey.KeyFile != "" {
			if kubeauthenticator.IsValidServiceAccountKeyFile(s.SecureServing.ServerCert.CertKey.KeyFile) {
				s.Authentication.ServiceAccounts.KeyFiles = []string{
					s.SecureServing.ServerCert.CertKey.KeyFile,
				}
			} else {
				klog.Warning("No TLS key provided, service account token authentication disabled")
			}
		}
	}

	// serviceaccount
	if s.ServiceAccountSigningKeyFile != "" && len(s.Authentication.ServiceAccounts.Issuers) != 0 && s.Authentication.ServiceAccounts.Issuers[0] != "" {
		if s.Authentication.ServiceAccounts.MaxExpiration != 0 {
			lowBound := time.Hour
			upBound := time.Duration(1<<32) * time.Second
			if s.Authentication.ServiceAccounts.MaxExpiration < lowBound ||
				s.Authentication.ServiceAccounts.MaxExpiration > upBound {
				return fmt.Errorf("the service-account-max-token-expiration must be between 1 hour and 2^32 seconds")
			}
			if s.Authentication.ServiceAccounts.ExtendExpiration {
				if s.Authentication.ServiceAccounts.MaxExpiration < serviceaccount.WarnOnlyBoundTokenExpirationSeconds*time.Second {
					klog.Warningf("service-account-extend-token-expiration is true, in order to correctly trigger safe transition logic, service-account-max-token-expiration must be set longer than %d seconds (currently %s)", serviceaccount.WarnOnlyBoundTokenExpirationSeconds, s.Authentication.ServiceAccounts.MaxExpiration)
				}
				if s.Authentication.ServiceAccounts.MaxExpiration < serviceaccount.ExpirationExtensionSeconds*time.Second {
					klog.Warningf("service-account-extend-token-expiration is true, enabling tokens valid up to %d seconds, which is longer than service-account-max-token-expiration set to %s seconds", serviceaccount.ExpirationExtensionSeconds, s.Authentication.ServiceAccounts.MaxExpiration)
				}
			}
		}
		s.ServiceAccountTokenMaxExpiration = s.Authentication.ServiceAccounts.MaxExpiration
		sk, err := keyutil.PrivateKeyFromFile(s.ServiceAccountSigningKeyFile)
		if err != nil {
			return fmt.Errorf("failed to parse service-account-issuer-key-file: %v", err)
		}
		s.ServiceAccountIssuer, err = serviceaccount.JWTTokenGenerator(s.Authentication.ServiceAccounts.Issuers[0], sk)
		if err != nil {
			return fmt.Errorf("failed to build token generator: %v", err)
		}
	}

	// Etcd
	if s.Etcd.EnableWatchCache {
		sizes := kubeapiserver.DefaultWatchCacheSizes()
		// Ensure that overrides parse correctly.
		userSpecified, err := parseWatchCacheSizes(s.Etcd.WatchCacheSizes)
		if err != nil {
			return err
		}
		for resource, size := range userSpecified {
			sizes[resource] = size
		}
		s.Etcd.WatchCacheSizes, err = writeWatchCacheSizes(sizes)
		if err != nil {
			return err
		}
	}

	// complete etcd with embedded etcd
	if s.ExtraOptions.EmbeddedEtcd.Enabled {
		klog.Infof("the embedded etcd directory: %s", s.ExtraOptions.EmbeddedEtcd.Directory)
		embeddedEtcdServer := &etcd.Server{
			Dir: s.ExtraOptions.EmbeddedEtcd.Directory,
		}
		shutdownCtx, cancel := context.WithCancel(context.TODO())
		go func() {
			defer cancel()
			<-stopCh
			klog.Infof("Received SIGTERM or SIGINT signal, shutting down controller.")
		}()
		embeddedClientInfo, err := embeddedEtcdServer.Run(shutdownCtx, s.ExtraOptions.EmbeddedEtcd.PeerPort, s.ExtraOptions.EmbeddedEtcd.ClientPort, s.ExtraOptions.EmbeddedEtcd.WalSizeBytes)
		if err != nil {
			return err
		}
		s.Etcd.StorageConfig.Transport.ServerList = embeddedClientInfo.Endpoints
		s.Etcd.StorageConfig.Transport.KeyFile = embeddedClientInfo.KeyFile
		s.Etcd.StorageConfig.Transport.CertFile = embeddedClientInfo.CertFile
		s.Etcd.StorageConfig.Transport.TrustedCAFile = embeddedClientInfo.TrustedCAFile
	}

	// API Enablement
	for key, value := range s.APIEnablement.RuntimeConfig {
		if key == "v1" || strings.HasPrefix(key, "v1/") || key == "api/v1" || strings.HasPrefix(key, "api/v1/") {
			delete(s.APIEnablement.RuntimeConfig, key)
			s.APIEnablement.RuntimeConfig["/v1"] = value
		}
		if key == "api/legacy" {
			delete(s.APIEnablement.RuntimeConfig, key)
		}
	}
	return nil
}

func (o *ServerRunOptions) InitServerRunOptions(cfg *configs.ControlplaneRunConfig) error {
	bindPort := configs.DefaultAPIServerPort
	if _, err := rest.InClusterConfig(); err != nil {
		if cfg.Apiserver.Port == 0 {
			cfg.Apiserver.Port = configs.DefaultAPIServerPort
			klog.Infof("API server port unspecified, Default port %d is used.", configs.DefaultAPIServerPort)
		}
		bindPort = cfg.Apiserver.Port
	}

	klog.Infof("Current controlplane config: %+v\n", cfg)

	// sign certs
	certChains, err := certificate.InitCerts(cfg)
	if err != nil {
		return fmt.Errorf("failed to retrieve the necessary certificates, %v", err)
	}

	// generate kubeconfig
	if err := certificate.InitKubeconfig(cfg, certChains); err != nil {
		return fmt.Errorf("failed to create the necessary kubeconfigs for internal components, %v", err)
	}

	certsDir := certificate.CertsDirectory(cfg.DataDirectory)
	sakFile := certificate.ServiceAccountKeyFile(certsDir)

	// apply default configs to options
	if cfg.Etcd.Mode == "embed" {
		o.Etcd.StorageConfig.Transport.ServerList = []string{"http://localhost:2379"}
		o.ExtraOptions.EmbeddedEtcd.Enabled = true
		o.ExtraOptions.EmbeddedEtcd.Directory = cfg.DataDirectory
	} else { // "external"
		o.Etcd.StorageConfig.Transport.ServerList = cfg.Etcd.Servers
		o.Etcd.StorageConfig.Transport.TrustedCAFile = cfg.Etcd.CAFile
		o.Etcd.StorageConfig.Transport.CertFile = cfg.Etcd.CertFile
		o.Etcd.StorageConfig.Transport.KeyFile = cfg.Etcd.KeyFile
		o.Etcd.StorageConfig.Prefix = cfg.Etcd.Prefix
	}

	o.SecureServing.BindPort = bindPort
	o.Authentication.ClientCert.ClientCA = certificate.ClientCACertFile(certsDir)
	o.ExtraOptions.ClientKeyFile = certificate.ClientCAKeyFile(certsDir)
	o.Authentication.ServiceAccounts.KeyFiles = []string{sakFile}
	o.KubeControllerManagerOptions.SAController.ServiceAccountKeyFile = sakFile
	o.ServiceAccountSigningKeyFile = sakFile
	o.SecureServing.ServerCert.CertKey.CertFile = certificate.ServingCertFile(certsDir)
	o.SecureServing.ServerCert.CertKey.KeyFile = certificate.ServingKeyFile(certsDir)
	o.ControlplaneDataDir = cfg.DataDirectory

	return nil
}

func (s *ServerRunOptions) Validate() error {
	errs := []error{}
	errs = append(errs, s.Etcd.Validate()...)
	errs = append(errs, s.SecureServing.Validate()...)
	errs = append(errs, s.Authentication.Validate()...)
	errs = append(errs, s.Authorization.Validate()...)
	errs = append(errs, s.Audit.Validate()...)
	errs = append(errs, s.Admission.Validate()...)
	errs = append(errs, s.APIEnablement.Validate(legacyscheme.Scheme, apiextensionsapiserver.Scheme, aggregatorscheme.Scheme)...)
	errs = append(errs, validateTokenRequest(s)...)
	errs = append(errs, s.Metrics.Validate()...)
	errs = append(errs, s.ExtraOptions.EmbeddedEtcd.Validate()...)
	return utilerrors.NewAggregate(errs)
}

func validateTokenRequest(options *ServerRunOptions) []error {
	var errs []error

	enableAttempted := options.ServiceAccountSigningKeyFile != "" ||
		(len(options.Authentication.ServiceAccounts.Issuers) != 0 && options.Authentication.ServiceAccounts.Issuers[0] != "") ||
		len(options.Authentication.APIAudiences) != 0

	enableSucceeded := options.ServiceAccountIssuer != nil

	if !enableAttempted {
		errs = append(errs, errors.New("--service-account-signing-key-file and --service-account-issuer are required flags"))
	}

	if enableAttempted && !enableSucceeded {
		errs = append(errs, errors.New("--service-account-signing-key-file, --service-account-issuer, and --api-audiences should be specified together"))
	}

	return errs
}

// ParseWatchCacheSizes turns a list of cache size values into a map of group resources
// to requested sizes.
func parseWatchCacheSizes(cacheSizes []string) (map[schema.GroupResource]int, error) {
	watchCacheSizes := make(map[schema.GroupResource]int)
	for _, c := range cacheSizes {
		tokens := strings.Split(c, "#")
		if len(tokens) != 2 {
			return nil, fmt.Errorf("invalid value of watch cache size: %s", c)
		}

		size, err := strconv.Atoi(tokens[1])
		if err != nil {
			return nil, fmt.Errorf("invalid size of watch cache size: %s", c)
		}
		if size < 0 {
			return nil, fmt.Errorf("watch cache size cannot be negative: %s", c)
		}
		watchCacheSizes[schema.ParseGroupResource(tokens[0])] = size
	}
	return watchCacheSizes, nil
}

// WriteWatchCacheSizes turns a map of cache size values into a list of string specifications.
func writeWatchCacheSizes(watchCacheSizes map[schema.GroupResource]int) ([]string, error) {
	var cacheSizes []string

	for resource, size := range watchCacheSizes {
		if size < 0 {
			return nil, fmt.Errorf("watch cache size cannot be negative for resource %s", resource)
		}
		cacheSizes = append(cacheSizes, fmt.Sprintf("%s#%d", resource.String(), size))
	}
	return cacheSizes, nil
}
