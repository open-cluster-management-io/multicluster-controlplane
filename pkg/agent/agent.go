// Copyright Contributors to the Open Cluster Management project
package agent

import (
	"context"
	"embed"
	"fmt"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"

	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	registrationspoke "open-cluster-management.io/ocm/pkg/registration/spoke"
	singletonspoke "open-cluster-management.io/ocm/pkg/singleton/spoke"
	workspoke "open-cluster-management.io/ocm/pkg/work/spoke"
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
