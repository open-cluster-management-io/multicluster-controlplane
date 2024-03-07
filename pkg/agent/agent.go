// Copyright Contributors to the Open Cluster Management project
package agent

import (
	"context"
	"embed"
	"fmt"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/spf13/pflag"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	authv1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	registrationspoke "open-cluster-management.io/ocm/pkg/registration/spoke"
	singletonspoke "open-cluster-management.io/ocm/pkg/singleton/spoke"
	workspoke "open-cluster-management.io/ocm/pkg/work/spoke"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"open-cluster-management.io/multicluster-controlplane/pkg/agent/addons"
	mcfeature "open-cluster-management.io/multicluster-controlplane/pkg/feature"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

//go:embed crds
var crds embed.FS

var crdStaticFiles = []string{
	"crds/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
	"crds/0000_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml",
	"crds/0000_03_authentication.open-cluster-management.io_managedserviceaccounts_crd.yaml",
}

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(crdv1.AddToScheme(genericScheme))
	utilruntime.Must(kubescheme.AddToScheme(genericScheme))
	utilruntime.Must(authv1beta1.AddToScheme(genericScheme))
}

type AgentOptions struct {
	RegistrationAgentOpts *registrationspoke.SpokeAgentOptions
	WorkAgentOpts         *workspoke.WorkloadAgentOptions
	CommonOpts            *commonoptions.AgentOptions

	KubeConfig  string
	WorkAgentID string

	SpokeKubeInformerFactory    informers.SharedInformerFactory
	SpokeClusterInformerFactory clusterv1informers.SharedInformerFactory
	SpokeRestMapper             meta.RESTMapper

	eventRecorder events.Recorder
}

func NewAgentOptions() *AgentOptions {
	return &AgentOptions{
		RegistrationAgentOpts: registrationspoke.NewSpokeAgentOptions(),
		WorkAgentOpts:         workspoke.NewWorkloadAgentOptions(),
		CommonOpts:            commonoptions.NewAgentOptions(),
		eventRecorder:         util.NewLoggingRecorder("managed-cluster-agents"),
	}
}

func (o *AgentOptions) AddFlags(fs *pflag.FlagSet) {
	o.CommonOpts.AddFlags(fs)
	o.WorkAgentOpts.AddFlags(fs)
	o.RegistrationAgentOpts.AddFlags(fs)
}

func (o *AgentOptions) WithClusterName(clusterName string) *AgentOptions {
	o.CommonOpts.SpokeClusterName = clusterName
	return o
}

func (o *AgentOptions) WithKubeconfig(kubeConfig string) *AgentOptions {
	o.KubeConfig = kubeConfig
	return o
}

func (o *AgentOptions) WithSpokeKubeconfig(spokeKubeConfig string) *AgentOptions {
	o.CommonOpts.SpokeKubeconfigFile = spokeKubeConfig
	return o
}

func (o *AgentOptions) WithBootstrapKubeconfig(bootstrapKubeconfig string) *AgentOptions {
	o.RegistrationAgentOpts.BootstrapKubeconfig = bootstrapKubeconfig
	return o
}

func (o *AgentOptions) WithHubKubeconfigDir(hubKubeconfigDir string) *AgentOptions {
	o.CommonOpts.HubKubeconfigDir = hubKubeconfigDir
	return o
}

func (o *AgentOptions) WithHubKubeconfigSecreName(hubKubeconfigSecreName string) *AgentOptions {
	o.RegistrationAgentOpts.HubKubeconfigSecret = hubKubeconfigSecreName
	return o
}

func (o *AgentOptions) WithWorkloadSourceDriverConfig(hubKubeConfigFile string) *AgentOptions {
	o.WorkAgentOpts.WorkloadSourceDriver.Type = workspoke.KubeDriver
	o.WorkAgentOpts.WorkloadSourceDriver.Config = hubKubeConfigFile
	return o
}

func (o *AgentOptions) RunAgent(ctx context.Context) error {
	config := singletonspoke.NewAgentConfig(o.CommonOpts, o.RegistrationAgentOpts, o.WorkAgentOpts)
	inClusterKubeConfig, err := rest.InClusterConfig()
	if err != nil {
		klog.Warningf("failed to get kubeconfig from cluster inside, will use '--kubeconfig' to build client")

		inClusterKubeConfig, err = clientcmd.BuildConfigFromFlags("", o.KubeConfig)
		if err != nil {
			return fmt.Errorf("unable to load kubeconfig from file %q: %v", o.KubeConfig, err)
		}
	}

	// building kubeconfig for the spoke/managed cluster
	spokeKubeConfig, err := o.CommonOpts.SpokeKubeConfig(inClusterKubeConfig)
	if err != nil {
		return err
	}
	apiExtensionsClient, err := apiextensionsclient.NewForConfig(spokeKubeConfig)
	if err != nil {
		return err
	}
	// TODO(qiujian16) crds should not be created by agent itself.
	if err := o.ensureCRDs(ctx, apiExtensionsClient); err != nil {
		return err
	}

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig:        inClusterKubeConfig,
		EventRecorder:     util.NewLoggingRecorder("managed-cluster-agents"),
		OperatorNamespace: "open-cluster-management-hub",
	}

	go utilruntime.Must(config.RunSpokeAgent(ctx, controllerContext))

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

// RunAddOns runs the addons in the agent
func (a *AgentOptions) RunAddOns(ctx context.Context) error {

	startCtrlMgr := false

	clusterName := a.CommonOpts.SpokeClusterName

	hubKubeConfig, err := clientcmd.BuildConfigFromFlags("", a.WorkAgentOpts.WorkloadSourceDriver.Config)
	if err != nil {
		return fmt.Errorf("unable to load kubeconfig from file %q: %v", a.KubeConfig, err)
	}

	hubManager, err := a.newHubManager(hubKubeConfig)
	if err != nil {
		return err
	}

	if features.SpokeMutableFeatureGate.Enabled(mcfeature.ManagedServiceAccount) {
		klog.Info("starting managed serviceaccount addon agent")
		if err := addons.StartManagedServiceAccountAgent(ctx, hubManager, clusterName); err != nil {
			klog.Fatalf("failed to setup managed serviceaccount addon, %v", err)
		}

		startCtrlMgr = true
	}

	if !startCtrlMgr {
		return nil
	}

	go func() {
		klog.Info("starting the embedded hub controller-runtime manager in controlplane agent")
		if err := hubManager.Start(ctx); err != nil {
			klog.Fatalf("failed to start embedded hub controller-runtime manager, %v", err)
		}
		<-ctx.Done()
	}()

	return nil
}

func (a *AgentOptions) newHubManager(hubKubeConfig *rest.Config) (manager.Manager, error) {
	mgr, err := ctrl.NewManager(hubKubeConfig, ctrl.Options{
		Scheme: genericScheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", //TODO think about the mertics later
		},
	})
	if err != nil {
		return nil, err
	}

	return mgr, nil
}
