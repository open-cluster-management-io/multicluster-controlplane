// Copyright Contributors to the Open Cluster Management project
package agent

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/spf13/pflag"

	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/multicluster-controlplane/pkg/features"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
	ocmfeatures "open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/clientcert"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/appliedmanifestcontroller"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/finalizercontroller"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/manifestcontroller"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/statuscontroller"
)

const (
	availableControllerWorker = 10
	cleanupControllerWorker   = 10
)

//go:embed crds
var crds embed.FS

var crdStaticFiles = []string{
	"crds/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
	"crds/0000_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml",
}

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(crdv1.AddToScheme(genericScheme))
}

type AgentOptions struct {
	RegistrationAgent *spoke.SpokeAgentOptions

	StatusSyncInterval                     time.Duration
	AppliedManifestWorkEvictionGracePeriod time.Duration

	KubeConfig  string
	WorkAgentID string

	SpokeKubeInformerFactory    informers.SharedInformerFactory
	SpokeClusterInformerFactory clusterv1informers.SharedInformerFactory
	SpokeRestMapper             meta.RESTMapper

	eventRecorder events.Recorder
}

func NewAgentOptions() *AgentOptions {
	return &AgentOptions{
		RegistrationAgent:                      spoke.NewSpokeAgentOptions(),
		eventRecorder:                          util.NewLoggingRecorder("managed-cluster-agents"),
		StatusSyncInterval:                     10 * time.Second,
		AppliedManifestWorkEvictionGracePeriod: 10 * time.Minute,
	}
}

func (o *AgentOptions) AddFlags(fs *pflag.FlagSet) {
	features.DefaultAgentMutableFeatureGate.AddFlag(fs)
	fs.StringVar(&o.RegistrationAgent.AgentOptions.SpokeClusterName, "cluster-name", o.RegistrationAgent.AgentOptions.SpokeClusterName,
		"If non-empty, will use as cluster name instead of generated random name.")
	fs.StringVar(&o.RegistrationAgent.BootstrapKubeconfig, "bootstrap-kubeconfig", o.RegistrationAgent.BootstrapKubeconfig,
		"The path of the kubeconfig file for agent bootstrap.")
	fs.StringVar(&o.RegistrationAgent.HubKubeconfigSecret, "hub-kubeconfig-secret", o.RegistrationAgent.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub.")
	fs.StringVar(&o.RegistrationAgent.HubKubeconfigDir, "hub-kubeconfig-dir", o.RegistrationAgent.HubKubeconfigDir,
		"The mount path of hub-kubeconfig-secret in the container.")
	fs.StringVar(&o.KubeConfig, "kubeconfig", o.KubeConfig,
		"The path of the kubeconfig file for current cluster. If this is not set, will try to get the kubeconfig from cluster inside")
	fs.StringVar(&o.RegistrationAgent.AgentOptions.SpokeKubeconfigFile, "spoke-kubeconfig", o.RegistrationAgent.AgentOptions.SpokeKubeconfigFile,
		"The path of the kubeconfig file for managed/spoke cluster. If this is not set, will use '--kubeconfig' to build client to connect to the managed cluster.")
	fs.StringVar(&o.WorkAgentID, "work-agent-id", o.WorkAgentID, "ID of the work agent to identify the work this agent should handle after restart/recovery.")
	fs.StringArrayVar(&o.RegistrationAgent.SpokeExternalServerURLs, "spoke-external-server-urls", o.RegistrationAgent.SpokeExternalServerURLs,
		"A list of reachable spoke cluster api server URLs for hub cluster.")
	fs.DurationVar(&o.RegistrationAgent.ClusterHealthCheckPeriod, "cluster-healthcheck-period", o.RegistrationAgent.ClusterHealthCheckPeriod,
		"The period to check managed cluster kube-apiserver health")
	fs.IntVar(&o.RegistrationAgent.MaxCustomClusterClaims, "max-custom-cluster-claims", o.RegistrationAgent.MaxCustomClusterClaims,
		"The max number of custom cluster claims to expose.")
	fs.Float32Var(&o.RegistrationAgent.AgentOptions.QPS, "spoke-kube-api-qps", o.RegistrationAgent.AgentOptions.QPS,
		"QPS to use while talking with apiserver on spoke cluster.")
	fs.IntVar(&o.RegistrationAgent.AgentOptions.Burst, "spoke-kube-api-burst", o.RegistrationAgent.AgentOptions.Burst,
		"Burst to use while talking with apiserver on spoke cluster.")
	fs.DurationVar(&o.StatusSyncInterval, "status-sync-interval", o.StatusSyncInterval, "Interval to sync resource status to hub.")
	fs.DurationVar(&o.AppliedManifestWorkEvictionGracePeriod, "appliedmanifestwork-eviction-grace-period", o.AppliedManifestWorkEvictionGracePeriod,
		"Grace period for appliedmanifestwork eviction")
}

func (o *AgentOptions) WithClusterName(clusterName string) *AgentOptions {
	o.RegistrationAgent.AgentOptions.SpokeClusterName = clusterName
	return o
}

func (o *AgentOptions) WithKubeconfig(kubeConfig string) *AgentOptions {
	o.KubeConfig = kubeConfig
	return o
}

func (o *AgentOptions) WithSpokeKubeconfig(spokeKubeConfig string) *AgentOptions {
	o.RegistrationAgent.AgentOptions.SpokeKubeconfigFile = spokeKubeConfig
	return o
}

func (o *AgentOptions) WithBootstrapKubeconfig(bootstrapKubeconfig string) *AgentOptions {
	o.RegistrationAgent.BootstrapKubeconfig = bootstrapKubeconfig
	return o
}

func (o *AgentOptions) WithHubKubeconfigDir(hubKubeconfigDir string) *AgentOptions {
	o.RegistrationAgent.HubKubeconfigDir = hubKubeconfigDir
	return o
}

func (o *AgentOptions) WithHubKubeconfigSecreName(hubKubeconfigSecreName string) *AgentOptions {
	o.RegistrationAgent.HubKubeconfigSecret = hubKubeconfigSecreName
	return o
}

func (o *AgentOptions) RunAgent(ctx context.Context) error {
	// building in-cluster/management (hosted mode) kubeconfig
	inClusterKubeConfig, err := rest.InClusterConfig()
	if err != nil {
		klog.Warningf("failed to get kubeconfig from cluster inside, will use '--kubeconfig' to build client")

		inClusterKubeConfig, err = clientcmd.BuildConfigFromFlags("", o.KubeConfig)
		if err != nil {
			return fmt.Errorf("unable to load kubeconfig from file %q: %v", o.KubeConfig, err)
		}
	}

	// building kubeconfig for the spoke/managed cluster
	spokeKubeConfig, err := o.RegistrationAgent.AgentOptions.SpokeKubeConfig(inClusterKubeConfig)
	if err != nil {
		return err
	}

	spokeKubeClient, err := kubernetes.NewForConfig(spokeKubeConfig)
	if err != nil {
		return err
	}

	spokeClusterClient, err := clusterv1client.NewForConfig(spokeKubeConfig)
	if err != nil {
		return err
	}

	apiExtensionsClient, err := apiextensionsclient.NewForConfig(spokeKubeConfig)
	if err != nil {
		return err
	}

	httpClient, err := rest.HTTPClientFor(spokeKubeConfig)
	if err != nil {
		return err
	}

	o.SpokeRestMapper, err = apiutil.NewDynamicRESTMapper(spokeKubeConfig, httpClient)
	if err != nil {
		return err
	}

	if err := o.ensureCRDs(ctx, apiExtensionsClient); err != nil {
		return err
	}

	o.SpokeKubeInformerFactory = informers.NewSharedInformerFactory(spokeKubeClient, 10*time.Minute)
	o.SpokeClusterInformerFactory = clusterv1informers.NewSharedInformerFactory(spokeClusterClient, 10*time.Minute)

	klog.Infof("Starting registration agent")
	go func() {
		// set registration features
		registrationFeatures := map[string]bool{}
		for feature := range ocmfeatures.DefaultSpokeRegistrationMutableFeatureGate.GetAll() {
			registrationFeatures[string(feature)] = features.DefaultAgentMutableFeatureGate.Enabled(feature)
		}
		if err := ocmfeatures.DefaultSpokeRegistrationMutableFeatureGate.SetFromMap(registrationFeatures); err != nil {
			klog.Fatalf("failed to set registration features, %v", err)
		}

		if err := o.RegistrationAgent.RunSpokeAgentWithSpokeInformers(
			ctx,
			inClusterKubeConfig,
			spokeKubeConfig,
			spokeKubeClient,
			o.SpokeKubeInformerFactory,
			o.SpokeClusterInformerFactory,
			o.eventRecorder,
		); err != nil {
			klog.Fatalf("failed to run registration agent, %v", err)
		}
	}()

	klog.Infof("Waiting for hub kubeconfig...")
	kubeconfigPath := path.Join(o.RegistrationAgent.HubKubeconfigDir, clientcert.KubeconfigFile)
	if err := o.WaitForValidHubKubeConfig(ctx, kubeconfigPath); err != nil {
		klog.Fatalf("failed to wait hub kubeconfig, %v", err)
	}

	hubRestConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return err
	}

	klog.Infof("Starting work agent")
	if err := o.startWorkControllers(
		ctx,
		hubRestConfig,
		spokeKubeConfig,
		o.RegistrationAgent.AgentOptions.SpokeClusterName,
		o.eventRecorder,
	); err != nil {
		klog.Fatalf("failed to run work agent, %v", err)
	}

	<-ctx.Done()
	return nil
}

func (o *AgentOptions) ensureCRDs(ctx context.Context, client apiextensionsclient.Interface) error {
	for _, crdFileName := range crdStaticFiles {
		template, err := crds.ReadFile(crdFileName)
		if err != nil {
			return err
		}

		objData := assets.MustCreateAssetFromTemplate(crdFileName, template, nil).Data
		obj, _, err := genericCodec.Decode(objData, nil, nil)
		if err != nil {
			return err
		}

		switch required := obj.(type) {
		case *crdv1.CustomResourceDefinition:
			if _, _, err := resourceapply.ApplyCustomResourceDefinitionV1(
				ctx,
				client.ApiextensionsV1(),
				o.eventRecorder,
				required,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

func (o *AgentOptions) WaitForValidHubKubeConfig(ctx context.Context, kubeconfigPath string) error {
	return wait.PollUntilContextCancel(
		ctx, 5*time.Second, true,
		func(ctx context.Context) (bool, error) {
			if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
				klog.V(4).Infof("Kubeconfig file %q not found", kubeconfigPath)
				return false, nil
			}

			keyPath := path.Join(o.RegistrationAgent.HubKubeconfigDir, clientcert.TLSKeyFile)
			if _, err := os.Stat(keyPath); os.IsNotExist(err) {
				klog.V(4).Infof("TLS key file %q not found", keyPath)
				return false, nil
			}

			certPath := path.Join(o.RegistrationAgent.HubKubeconfigDir, clientcert.TLSCertFile)
			certData, err := os.ReadFile(path.Clean(certPath))
			if err != nil {
				klog.V(4).Infof("Unable to load TLS cert file %q", certPath)
				return false, nil
			}

			// check if the tls certificate is issued for the current cluster/agent
			clusterName, agentName, err := registration.GetClusterAgentNamesFromCertificate(certData)
			if err != nil {
				return false, nil
			}

			if clusterName != o.RegistrationAgent.AgentOptions.SpokeClusterName ||
				agentName != o.RegistrationAgent.AgentName {
				klog.V(4).Infof("Certificate in file %q is issued for agent %q instead of %q",
					certPath, fmt.Sprintf("%s:%s", clusterName, agentName),
					fmt.Sprintf("%s:%s", o.RegistrationAgent.AgentOptions.SpokeClusterName, o.RegistrationAgent.AgentName))
				return false, nil
			}

			return clientcert.IsCertificateValid(certData, nil)
		},
	)
}

func (o *AgentOptions) startWorkControllers(ctx context.Context,
	hubRestConfig, spokeRestConfig *rest.Config, clusterName string, eventRecorder events.Recorder) error {
	hubhash := helper.HubHash(hubRestConfig.Host)
	agentID := o.WorkAgentID
	if len(agentID) == 0 {
		agentID = fmt.Sprintf("%s-%s", clusterName, hubhash)
	}

	hubWorkClient, err := workclientset.NewForConfig(hubRestConfig)
	if err != nil {
		return err
	}

	spokeDynamicClient, err := dynamic.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}

	spokeKubeClient, err := kubernetes.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}

	spokeAPIExtensionClient, err := apiextensionsclient.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}

	spokeWorkClient, err := workclientset.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}

	// Only watch the cluster namespace on hub
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(
		hubWorkClient, 5*time.Minute, workinformers.WithNamespace(clusterName))
	spokeWorkInformerFactory := workinformers.NewSharedInformerFactory(spokeWorkClient, 5*time.Minute)

	validator := auth.NewFactory(
		spokeRestConfig,
		spokeKubeClient,
		workInformerFactory.Work().V1().ManifestWorks(),
		clusterName,
		eventRecorder,
		o.SpokeRestMapper,
	).NewExecutorValidator(ctx, features.DefaultAgentMutableFeatureGate.Enabled(ocmfeature.ExecutorValidatingCaches))

	manifestWorkController := manifestcontroller.NewManifestWorkController(
		eventRecorder,
		spokeDynamicClient,
		spokeKubeClient,
		spokeAPIExtensionClient,
		hubWorkClient.WorkV1().ManifestWorks(clusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(clusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubhash,
		agentID,
		o.SpokeRestMapper,
		validator,
	)

	addFinalizerController := finalizercontroller.NewAddFinalizerController(
		eventRecorder,
		hubWorkClient.WorkV1().ManifestWorks(clusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(clusterName),
	)

	appliedManifestWorkFinalizeController := finalizercontroller.NewAppliedManifestWorkFinalizeController(
		eventRecorder,
		spokeDynamicClient,
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		agentID,
	)

	manifestWorkFinalizeController := finalizercontroller.NewManifestWorkFinalizeController(
		eventRecorder,
		hubWorkClient.WorkV1().ManifestWorks(clusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(clusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubhash,
	)

	unmanagedAppliedManifestWorkController := finalizercontroller.NewUnManagedAppliedWorkController(
		eventRecorder,
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(clusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		o.AppliedManifestWorkEvictionGracePeriod,
		hubhash,
		agentID,
	)

	appliedManifestWorkController := appliedmanifestcontroller.NewAppliedManifestWorkController(
		eventRecorder,
		spokeDynamicClient,
		hubWorkClient.WorkV1().ManifestWorks(clusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(clusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubhash,
	)

	availableStatusController := statuscontroller.NewAvailableStatusController(
		eventRecorder,
		spokeDynamicClient,
		hubWorkClient.WorkV1().ManifestWorks(clusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(clusterName),
		o.StatusSyncInterval,
	)

	go workInformerFactory.Start(ctx.Done())
	go spokeWorkInformerFactory.Start(ctx.Done())

	go addFinalizerController.Run(ctx, 1)
	go appliedManifestWorkFinalizeController.Run(ctx, cleanupControllerWorker)
	go unmanagedAppliedManifestWorkController.Run(ctx, 1)
	go appliedManifestWorkController.Run(ctx, 1)
	go manifestWorkController.Run(ctx, 1)
	go manifestWorkFinalizeController.Run(ctx, cleanupControllerWorker)
	go availableStatusController.Run(ctx, availableControllerWorker)

	return nil
}
