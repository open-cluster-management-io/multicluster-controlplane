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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/multicluster-controlplane/pkg/features"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
	"open-cluster-management.io/registration/pkg/clientcert"
	registrationfeatures "open-cluster-management.io/registration/pkg/features"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/pkg/spoke/managedcluster"
	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/pkg/spoke/auth"
	"open-cluster-management.io/work/pkg/spoke/controllers/appliedmanifestcontroller"
	"open-cluster-management.io/work/pkg/spoke/controllers/finalizercontroller"
	"open-cluster-management.io/work/pkg/spoke/controllers/manifestcontroller"
	"open-cluster-management.io/work/pkg/spoke/controllers/statuscontroller"
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

	Burst int
	QPS   float32

	KubeConfig string

	eventRecorder events.Recorder
}

func NewAgentOptions() *AgentOptions {
	return &AgentOptions{
		RegistrationAgent:                      spoke.NewSpokeAgentOptions(),
		eventRecorder:                          util.NewLoggingRecorder("managed-cluster-agents"),
		Burst:                                  100,
		QPS:                                    50,
		StatusSyncInterval:                     10 * time.Second,
		AppliedManifestWorkEvictionGracePeriod: 10 * time.Minute,
	}
}

func (o *AgentOptions) AddFlags(fs *pflag.FlagSet) {
	features.DefaultAgentMutableFeatureGate.AddFlag(fs)
	fs.StringVar(&o.RegistrationAgent.ClusterName, "cluster-name", o.RegistrationAgent.ClusterName,
		"If non-empty, will use as cluster name instead of generated random name.")
	fs.StringVar(&o.RegistrationAgent.BootstrapKubeconfig, "bootstrap-kubeconfig", o.RegistrationAgent.BootstrapKubeconfig,
		"The path of the kubeconfig file for agent bootstrap.")
	fs.StringVar(&o.RegistrationAgent.HubKubeconfigSecret, "hub-kubeconfig-secret", o.RegistrationAgent.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub.")
	fs.StringVar(&o.RegistrationAgent.HubKubeconfigDir, "hub-kubeconfig-dir", o.RegistrationAgent.HubKubeconfigDir,
		"The mount path of hub-kubeconfig-secret in the container.")
	fs.StringVar(&o.KubeConfig, "kubeconfig", o.KubeConfig,
		"The path of the kubeconfig file for current cluster. If this is not set, will try to get the kubeconfig from cluster inside")
	fs.StringVar(&o.RegistrationAgent.SpokeKubeconfig, "spoke-kubeconfig", o.RegistrationAgent.SpokeKubeconfig,
		"The path of the kubeconfig file for managed/spoke cluster. If this is not set, will use '--kubeconfig' to build client to connect to the managed cluster.")
	fs.StringArrayVar(&o.RegistrationAgent.SpokeExternalServerURLs, "spoke-external-server-urls", o.RegistrationAgent.SpokeExternalServerURLs,
		"A list of reachable spoke cluster api server URLs for hub cluster.")
	fs.DurationVar(&o.RegistrationAgent.ClusterHealthCheckPeriod, "cluster-healthcheck-period", o.RegistrationAgent.ClusterHealthCheckPeriod,
		"The period to check managed cluster kube-apiserver health")
	fs.IntVar(&o.RegistrationAgent.MaxCustomClusterClaims, "max-custom-cluster-claims", o.RegistrationAgent.MaxCustomClusterClaims,
		"The max number of custom cluster claims to expose.")
	fs.Float32Var(&o.QPS, "spoke-kube-api-qps", o.QPS, "QPS to use while talking with apiserver on spoke cluster.")
	fs.IntVar(&o.Burst, "spoke-kube-api-burst", o.Burst, "Burst to use while talking with apiserver on spoke cluster.")
	fs.DurationVar(&o.StatusSyncInterval, "status-sync-interval", o.StatusSyncInterval, "Interval to sync resource status to hub.")
	fs.DurationVar(&o.AppliedManifestWorkEvictionGracePeriod, "appliedmanifestwork-eviction-grace-period", o.AppliedManifestWorkEvictionGracePeriod,
		"Grace period for appliedmanifestwork eviction")
}

func (o *AgentOptions) WithClusterName(clusterName string) *AgentOptions {
	o.RegistrationAgent.ClusterName = clusterName
	return o
}

func (o *AgentOptions) WithKubeconfig(kubeConfig string) *AgentOptions {
	o.KubeConfig = kubeConfig
	return o
}

func (o *AgentOptions) WithSpokeKubeconfig(spokeKubeConfig string) *AgentOptions {
	o.RegistrationAgent.SpokeKubeconfig = spokeKubeConfig
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

	ctrlContext := &controllercmd.ControllerContext{
		KubeConfig:    inClusterKubeConfig,
		EventRecorder: o.eventRecorder,
	}

	// building kubeconfig for the spoke/managed cluster
	spokeKubeConfig, err := o.spokeKubeConfig(ctrlContext)
	if err != nil {
		return err
	}

	spokeKubeConfig.QPS = o.QPS
	spokeKubeConfig.Burst = o.Burst

	apiExtensionsClient, err := apiextensionsclient.NewForConfig(spokeKubeConfig)
	if err != nil {
		return err
	}

	if err := o.ensureCRDs(ctx, apiExtensionsClient); err != nil {
		return err
	}

	klog.Infof("Starting registration agent")
	go func() {
		// set registration features
		registrationFeatures := map[string]bool{}
		for feature := range registrationfeatures.DefaultSpokeMutableFeatureGate.GetAll() {
			registrationFeatures[string(feature)] = features.DefaultAgentMutableFeatureGate.Enabled(feature)
		}
		if err := registrationfeatures.DefaultSpokeMutableFeatureGate.SetFromMap(registrationFeatures); err != nil {
			klog.Fatalf("failed to set registration features, %v", err)
		}

		if err := o.RegistrationAgent.RunSpokeAgent(ctx, ctrlContext); err != nil {
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
	if err := o.startWorkControllers(ctx, hubRestConfig, spokeKubeConfig, o.eventRecorder); err != nil {
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
	return wait.PollImmediateInfinite(
		5*time.Second,
		func() (bool, error) {
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
			clusterName, agentName, err := managedcluster.GetClusterAgentNamesFromCertificate(certData)
			if err != nil {
				return false, nil
			}

			if clusterName != o.RegistrationAgent.ClusterName || agentName != o.RegistrationAgent.AgentName {
				klog.V(4).Infof("Certificate in file %q is issued for agent %q instead of %q",
					certPath, fmt.Sprintf("%s:%s", clusterName, agentName),
					fmt.Sprintf("%s:%s", o.RegistrationAgent.ClusterName, o.RegistrationAgent.AgentName))
				return false, nil
			}

			return clientcert.IsCertificateValid(certData, nil)
		},
	)
}

func (o *AgentOptions) startWorkControllers(ctx context.Context,
	hubRestConfig, spokeRestConfig *rest.Config, eventRecorder events.Recorder) error {
	hubhash := helper.HubHash(hubRestConfig.Host)
	agentID := fmt.Sprintf("%s-%s", o.RegistrationAgent.ClusterName, hubhash)

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

	restMapper, err := apiutil.NewDynamicRESTMapper(spokeRestConfig, apiutil.WithLazyDiscovery)
	if err != nil {
		return err
	}

	// Only watch the cluster namespace on hub
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(
		hubWorkClient, 5*time.Minute, workinformers.WithNamespace(o.RegistrationAgent.ClusterName))
	spokeWorkInformerFactory := workinformers.NewSharedInformerFactory(spokeWorkClient, 5*time.Minute)

	validator := auth.NewFactory(
		spokeRestConfig,
		spokeKubeClient,
		workInformerFactory.Work().V1().ManifestWorks(),
		o.RegistrationAgent.ClusterName,
		eventRecorder,
		restMapper,
	).NewExecutorValidator(ctx, features.DefaultAgentMutableFeatureGate.Enabled(ocmfeature.ExecutorValidatingCaches))

	manifestWorkController := manifestcontroller.NewManifestWorkController(
		eventRecorder,
		spokeDynamicClient,
		spokeKubeClient,
		spokeAPIExtensionClient,
		hubWorkClient.WorkV1().ManifestWorks(o.RegistrationAgent.ClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.RegistrationAgent.ClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubhash, agentID,
		restMapper,
		validator,
	)

	addFinalizerController := finalizercontroller.NewAddFinalizerController(
		eventRecorder,
		hubWorkClient.WorkV1().ManifestWorks(o.RegistrationAgent.ClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.RegistrationAgent.ClusterName),
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
		hubWorkClient.WorkV1().ManifestWorks(o.RegistrationAgent.ClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.RegistrationAgent.ClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubhash,
	)

	unmanagedAppliedManifestWorkController := finalizercontroller.NewUnManagedAppliedWorkController(
		eventRecorder,
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.RegistrationAgent.ClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		o.AppliedManifestWorkEvictionGracePeriod,
		hubhash, agentID,
	)

	appliedManifestWorkController := appliedmanifestcontroller.NewAppliedManifestWorkController(
		eventRecorder,
		spokeDynamicClient,
		hubWorkClient.WorkV1().ManifestWorks(o.RegistrationAgent.ClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.RegistrationAgent.ClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubhash,
	)

	availableStatusController := statuscontroller.NewAvailableStatusController(
		eventRecorder,
		spokeDynamicClient,
		hubWorkClient.WorkV1().ManifestWorks(o.RegistrationAgent.ClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.RegistrationAgent.ClusterName),
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

func (o *AgentOptions) spokeKubeConfig(controllerContext *controllercmd.ControllerContext) (*rest.Config, error) {
	if o.RegistrationAgent.SpokeKubeconfig == "" {
		return controllerContext.KubeConfig, nil
	}

	spokeRestConfig, err := clientcmd.BuildConfigFromFlags("", o.RegistrationAgent.SpokeKubeconfig)
	if err != nil {
		return nil, fmt.Errorf("unable to load spoke kubeconfig from file %q: %v", o.RegistrationAgent.SpokeKubeconfig, err)
	}
	return spokeRestConfig, nil
}
