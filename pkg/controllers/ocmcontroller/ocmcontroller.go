// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	genericinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	ocmfeature "open-cluster-management.io/api/feature"
	authv1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	addonhub "open-cluster-management.io/ocm/pkg/addon"
	"open-cluster-management.io/ocm/pkg/features"
	placementcontrollers "open-cluster-management.io/ocm/pkg/placement/controllers"
	registrationhub "open-cluster-management.io/ocm/pkg/registration/hub"
	workhub "open-cluster-management.io/ocm/pkg/work/hub"
	cloudeventswork "open-cluster-management.io/sdk-go/pkg/cloudevents/work"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"open-cluster-management.io/multicluster-controlplane/pkg/controllers/addons"
	"open-cluster-management.io/multicluster-controlplane/pkg/controllers/bootstrap"
	mcfeature "open-cluster-management.io/multicluster-controlplane/pkg/feature"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/options"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(kubescheme.AddToScheme(scheme))
	utilruntime.Must(authv1beta1.AddToScheme(scheme))
}

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
	eventRecorder := util.NewLoggingRecorder("hub-controller")

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig:        restConfig,
		EventRecorder:     eventRecorder,
		OperatorNamespace: "open-cluster-management-hub",
	}

	metadataClient, err := metadata.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
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
	workInformers := workinformers.NewSharedInformerFactory(workClient, 10*time.Minute)
	addOnInformers := addoninformers.NewSharedInformerFactory(addOnClient, 10*time.Minute)
	dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 10*time.Minute)

	go func() {
		if err := opts.RunControllerManagerWithInformers(
			ctx,
			controllerContext,
			kubeClient,
			metadataClient,
			clusterClient,
			addOnClient,
			kubeInformers,
			clusterInformers,
			workInformers,
			addOnInformers,
		); err != nil {
			klog.Fatal(err)
		}
	}()

	go func() {
		if err := placementcontrollers.RunControllerManagerWithInformers(
			ctx,
			controllerContext,
			kubeClient,
			clusterClient,
			clusterInformers,
		); err != nil {
			klog.Fatal(err)
		}
	}()

	if features.HubMutableFeatureGate.Enabled(ocmfeature.ManifestWorkReplicaSet) {
		go func() {
			// TODO(qiujian16), should expose as flags to support other types.
			workOpts := workhub.NewWorkHubManagerOptions()
			workOpts.WorkDriver = cloudeventswork.ConfigTypeKube

			if err := workhub.NewWorkHubManagerConfig(workOpts).RunWorkHubManager(
				ctx,
				controllerContext,
			); err != nil {
				klog.Fatal(err)
			}
		}()
	}

	if features.HubMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		go func() {
			if err := addonhub.RunControllerManagerWithInformers(
				ctx,
				controllerContext,
				kubeClient,
				addOnClient,
				clusterInformers,
				addOnInformers,
				workInformers,
				dynamicInformers,
			); err != nil {
				klog.Fatal(err)
			}
		}()
	}

	go func() {
		startCtrlMgr := false

		mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress: "0", //TODO think about the mertics later
			},
			Logger: ctrl.Log.WithName("ctrl-runtime-manager"),
		})
		if err != nil {
			klog.Fatalf("unable to start manager %v", err)
		}

		if features.HubMutableFeatureGate.Enabled(mcfeature.ManagedServiceAccountEphemeralIdentity) {

			klog.Info("starting managed serviceaccount controller")
			if err := addons.SetupManagedServiceAccountWithManager(ctx, mgr); err != nil {
				klog.Fatalf("failed to start managed serviceaccount controller, %v", err)
			}
			startCtrlMgr = true
		}

		if !startCtrlMgr {
			return
		}

		if err := mgr.Start(ctx); err != nil {
			klog.Fatalf("failed to start controller manager, %v", err)
		}

		<-ctx.Done()
	}()

	go kubeInformers.Start(ctx.Done())
	go clusterInformers.Start(ctx.Done())
	go workInformers.Start(ctx.Done())
	go addOnInformers.Start(ctx.Done())
	go dynamicInformers.Start(ctx.Done())

	<-ctx.Done()
	return nil
}
