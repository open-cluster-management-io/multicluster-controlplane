// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"context"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/options"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	genericinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"
	"open-cluster-management.io/multicluster-controlplane/pkg/controllers/bootstrap"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
	addonhub "open-cluster-management.io/ocm/pkg/addon"
	"open-cluster-management.io/ocm/pkg/features"
	placementcontrollers "open-cluster-management.io/ocm/pkg/placement/controllers"
	registrationhub "open-cluster-management.io/ocm/pkg/registration/hub"
	workhub "open-cluster-management.io/ocm/pkg/work/hub"
)

func InstallControllers(opts options.ServerRunOptions) func(<-chan struct{}, *aggregatorapiserver.Config) error {
	return func(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error {
		klog.Info("bootstrapping ocm controllers")

		go func() {
			restConfig := aggregatorConfig.GenericConfig.LoopbackClientConfig
			restConfig.ContentType = "application/json"

			apiextensionsClient, err := apiextensionsclient.NewForConfig(aggregatorConfig.GenericConfig.LoopbackClientConfig)
			if err != nil {
				klog.Fatalf("failed to create apiextensions client: %v", err)
			}

			ctx := util.GoContext(stopCh)

			if bootstrap.WaitFOROCMCRDsReady(ctx, apiextensionsClient) {
				klog.Infof("ocm crds are ready")
			}

			if err := runControllers(
				ctx,
				restConfig,
				aggregatorConfig.GenericConfig.SharedInformerFactory,
				opts.RegistrationOpts,
			); err != nil {
				klog.Fatalf("failed to bootstrap ocm controllers: %v", err)
			}

			klog.Infof("stopping ocm controllers")
		}()

		return nil
	}
}

func runControllers(ctx context.Context,
	restConfig *rest.Config,
	kubeInformers genericinformers.SharedInformerFactory,
	opts *registrationhub.HubManagerOptions) error {
	eventRecorder := util.NewLoggingRecorder("registration-controller")

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig:        restConfig,
		EventRecorder:     eventRecorder,
		OperatorNamespace: "open-cluster-management-hub",
	}

	clusterClient, err := clusterv1client.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	workClient, err := workv1client.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	addOnClient, err := addonclient.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	workInformers := workv1informers.NewSharedInformerFactory(workClient, 10*time.Minute)
	addOnInformers := addoninformers.NewSharedInformerFactory(addOnClient, 10*time.Minute)
	dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 10*time.Minute)

	go utilruntime.Must(
		opts.RunControllerManagerWithInformers(
			ctx,
			controllerContext,
			kubeClient,
			clusterClient,
			addOnClient,
			kubeInformers,
			clusterInformers,
			workInformers,
			addOnInformers,
		))

	go utilruntime.Must(placementcontrollers.RunControllerManagerWithInformers(
		ctx,
		controllerContext,
		kubeClient,
		clusterClient,
		clusterInformers,
	))

	if features.HubMutableFeatureGate.Enabled(ocmfeature.ManifestWorkReplicaSet) {
		go utilruntime.Must(workhub.RunControllerManagerWithInformers(
			ctx,
			controllerContext,
			workClient,
			workInformers,
			clusterInformers,
		))
	}

	if features.HubMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		go utilruntime.Must(addonhub.RunControllerManagerWithInformers(
			ctx,
			controllerContext,
			kubeClient,
			addOnClient,
			clusterInformers,
			addOnInformers,
			workInformers,
			dynamicInformers,
		))
	}

	go kubeInformers.Start(ctx.Done())
	go clusterInformers.Start(ctx.Done())
	go workInformers.Start(ctx.Done())
	go addOnInformers.Start(ctx.Done())
	go dynamicInformers.Start(ctx.Done())

	<-ctx.Done()
	return nil
}
