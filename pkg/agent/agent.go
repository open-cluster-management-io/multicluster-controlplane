// Copyright Contributors to the Open Cluster Management project
package agent

import (
	"context"
	"embed"

	"github.com/spf13/pflag"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	"open-cluster-management.io/registration/pkg/spoke"
)

//go:embed crds
var crds embed.FS

var crdStaticFiles = []string{
	"crds/appliedmanifestworks.work.open-cluster-management.io.crd.yaml",
	"crds/clusterclaims.clusters.open-cluster-management.io.crd.yaml",
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
	registrationAgent *spoke.SpokeAgentOptions
	kubeConfig        *rest.Config
	eventRecorder     events.Recorder
}

func NewAgentOptions() *AgentOptions {
	return &AgentOptions{
		registrationAgent: spoke.NewSpokeAgentOptions(),
		eventRecorder:     events.NewInMemoryRecorder("managed-cluster-agents"),
	}
}

func (o *AgentOptions) AddFlags(fs *pflag.FlagSet) {
	o.registrationAgent.AddFlags(fs)
}

func (o *AgentOptions) WithClusterName(clusterName string) *AgentOptions {
	o.registrationAgent.ClusterName = clusterName
	return o
}

func (o *AgentOptions) WithSpokeKubeconfig(kubeConfig *rest.Config) *AgentOptions {
	o.kubeConfig = kubeConfig
	return o
}

func (o *AgentOptions) WithBootstrapKubeconfig(bootstrapKubeconfig string) *AgentOptions {
	o.registrationAgent.BootstrapKubeconfig = bootstrapKubeconfig
	return o
}

func (o *AgentOptions) WithHubKubeconfigDir(hubKubeconfigDir string) *AgentOptions {
	o.registrationAgent.HubKubeconfigDir = hubKubeconfigDir
	return o
}

func (o *AgentOptions) Complete() error {
	if o.kubeConfig != nil {
		return nil
	}

	if o.registrationAgent.SpokeKubeconfig == "" {
		kubeConfig, err := rest.InClusterConfig()
		if err != nil {
			return err
		}

		o.kubeConfig = kubeConfig
		return nil
	}

	kubeConfig, err := clientcmd.BuildConfigFromFlags("", o.registrationAgent.SpokeKubeconfig)
	if err != nil {
		return err
	}

	o.kubeConfig = kubeConfig
	return nil
}

func (o *AgentOptions) Validate() error {
	return nil
}

func (o *AgentOptions) RunAgent(ctx context.Context) error {
	if err := o.Complete(); err != nil {
		return err
	}

	if err := o.Validate(); err != nil {
		return err
	}

	apiExtensionsClient, err := apiextensionsclient.NewForConfig(o.kubeConfig)
	if err != nil {
		return err
	}

	if err := o.ensureCRDs(ctx, apiExtensionsClient); err != nil {
		return err
	}

	klog.Infof("Starting registration agent")
	go func() {
		ctrlContext := &controllercmd.ControllerContext{
			KubeConfig:    o.kubeConfig,
			EventRecorder: o.eventRecorder,
		}

		if err := o.registrationAgent.RunSpokeAgent(ctx, ctrlContext); err != nil {
			klog.Fatal(err)
		}
	}()

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
