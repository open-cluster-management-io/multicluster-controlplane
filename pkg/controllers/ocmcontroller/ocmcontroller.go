// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"context"
	"net/http"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/pkg/errors"

	certv1 "k8s.io/api/certificates/v1"
	certv1beta1 "k8s.io/api/certificates/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	genericinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	kubeevents "k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
	aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/multicluster-controlplane/pkg/controllers/bootstrap"
	"open-cluster-management.io/multicluster-controlplane/pkg/features"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
	scheduling "open-cluster-management.io/ocm/pkg/placement/controllers/scheduling"
	"open-cluster-management.io/ocm/pkg/placement/debugger"
	"open-cluster-management.io/ocm/pkg/registration/helpers"
	"open-cluster-management.io/ocm/pkg/registration/hub/addon"
	"open-cluster-management.io/ocm/pkg/registration/hub/clusterrole"
	"open-cluster-management.io/ocm/pkg/registration/hub/csr"
	"open-cluster-management.io/ocm/pkg/registration/hub/lease"
	"open-cluster-management.io/ocm/pkg/registration/hub/managedcluster"
	"open-cluster-management.io/ocm/pkg/registration/hub/managedclusterset"
	"open-cluster-management.io/ocm/pkg/registration/hub/managedclustersetbinding"
	"open-cluster-management.io/ocm/pkg/registration/hub/rbacfinalizerdeletion"
	"open-cluster-management.io/ocm/pkg/registration/hub/taint"
)

func InstallControllers(clusterAutoApprovalUsers []string) func(<-chan struct{}, *aggregatorapiserver.Config) error {
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
				clusterAutoApprovalUsers,
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
	clusterAutoApprovalUsers []string) error {
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

	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	workInformers := workv1informers.NewSharedInformerFactory(workClient, 10*time.Minute)
	addOnInformers := addoninformers.NewSharedInformerFactory(addOnClient, 10*time.Minute)

	managedClusterController := managedcluster.NewManagedClusterController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		controllerContext.EventRecorder,
	)

	taintController := taint.NewTaintController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		controllerContext.EventRecorder,
	)

	csrReconciles := []csr.Reconciler{csr.NewCSRRenewalReconciler(kubeClient, controllerContext.EventRecorder)}
	if features.DefaultControlplaneMutableFeatureGate.Enabled(ocmfeature.ManagedClusterAutoApproval) {
		csrReconciles = append(csrReconciles, csr.NewCSRBootstrapReconciler(
			kubeClient,
			clusterClient,
			clusterInformers.Cluster().V1().ManagedClusters().Lister(),
			clusterAutoApprovalUsers,
			controllerContext.EventRecorder,
		))
	}

	var csrController factory.Controller
	if features.DefaultControlplaneMutableFeatureGate.Enabled(ocmfeature.V1beta1CSRAPICompatibility) {
		v1CSRSupported, v1beta1CSRSupported, err := helpers.IsCSRSupported(kubeClient)
		if err != nil {
			return errors.Wrapf(err, "failed CSR api discovery")
		}

		if !v1CSRSupported && v1beta1CSRSupported {
			csrController = csr.NewCSRApprovingController[*certv1beta1.CertificateSigningRequest](
				kubeInformers.Certificates().V1beta1().CertificateSigningRequests().Informer(),
				kubeInformers.Certificates().V1beta1().CertificateSigningRequests().Lister(),
				csr.NewCSRV1beta1Approver(kubeClient),
				csrReconciles,
				controllerContext.EventRecorder,
			)
			klog.Info("Using v1beta1 CSR api to manage spoke client certificate")
		}
	}
	if csrController == nil {
		csrController = csr.NewCSRApprovingController[*certv1.CertificateSigningRequest](
			kubeInformers.Certificates().V1().CertificateSigningRequests().Informer(),
			kubeInformers.Certificates().V1().CertificateSigningRequests().Lister(),
			csr.NewCSRV1Approver(kubeClient),
			csrReconciles,
			controllerContext.EventRecorder,
		)
	}

	leaseController := lease.NewClusterLeaseController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Coordination().V1().Leases(),
		controllerContext.EventRecorder,
	)

	rbacFinalizerController := rbacfinalizerdeletion.NewFinalizeController(
		kubeInformers.Rbac().V1().Roles(),
		kubeInformers.Rbac().V1().RoleBindings(),
		kubeInformers.Core().V1().Namespaces().Lister(),
		clusterInformers.Cluster().V1().ManagedClusters().Lister(),
		workInformers.Work().V1().ManifestWorks().Lister(),
		kubeClient.RbacV1(),
		controllerContext.EventRecorder,
	)

	managedClusterSetController := managedclusterset.NewManagedClusterSetController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		controllerContext.EventRecorder,
	)

	managedClusterSetBindingController := managedclustersetbinding.NewManagedClusterSetBindingController(
		clusterClient,
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings(),
		controllerContext.EventRecorder,
	)

	clusterroleController := clusterrole.NewManagedClusterClusterroleController(
		kubeClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Rbac().V1().ClusterRoles(),
		controllerContext.EventRecorder,
	)

	addOnHealthCheckController := addon.NewManagedClusterAddOnHealthCheckController(
		addOnClient,
		addOnInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		clusterInformers.Cluster().V1().ManagedClusters(),
		controllerContext.EventRecorder,
	)

	addOnFeatureDiscoveryController := addon.NewAddOnFeatureDiscoveryController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addOnInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		controllerContext.EventRecorder,
	)

	var defaultManagedClusterSetController, globalManagedClusterSetController factory.Controller
	if features.DefaultControlplaneMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		defaultManagedClusterSetController = managedclusterset.NewDefaultManagedClusterSetController(
			clusterClient.ClusterV1beta2(),
			clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
			controllerContext.EventRecorder,
		)
		globalManagedClusterSetController = managedclusterset.NewGlobalManagedClusterSetController(
			clusterClient.ClusterV1beta2(),
			clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
			controllerContext.EventRecorder,
		)
	}

	broadcaster := kubeevents.NewBroadcaster(&kubeevents.EventSinkImpl{Interface: kubeClient.EventsV1()})

	broadcaster.StartRecordingToSink(ctx.Done())

	recorder := broadcaster.NewRecorder(clusterscheme.Scheme, "placementController")

	scheduler := scheduling.NewPluginScheduler(
		scheduling.NewSchedulerHandler(
			clusterClient,
			clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister(),
			clusterInformers.Cluster().V1alpha1().AddOnPlacementScores().Lister(),
			clusterInformers.Cluster().V1().ManagedClusters().Lister(),
			recorder),
	)

	if controllerContext.Server != nil {
		debug := debugger.NewDebugger(
			scheduler,
			clusterInformers.Cluster().V1beta1().Placements(),
			clusterInformers.Cluster().V1().ManagedClusters(),
		)

		controllerContext.Server.Handler.NonGoRestfulMux.HandlePrefix(debugger.DebugPath,
			http.HandlerFunc(debug.Handler))
	}

	schedulingController := scheduling.NewSchedulingController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings(),
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
		clusterInformers.Cluster().V1alpha1().AddOnPlacementScores(),
		scheduler,
		controllerContext.EventRecorder, recorder,
	)

	go kubeInformers.Start(ctx.Done())
	go clusterInformers.Start(ctx.Done())
	go workInformers.Start(ctx.Done())
	go addOnInformers.Start(ctx.Done())

	//TODO need a way to verify all informers are synced

	go managedClusterController.Run(ctx, 1)
	go taintController.Run(ctx, 1)
	go csrController.Run(ctx, 1)
	go leaseController.Run(ctx, 1)
	go rbacFinalizerController.Run(ctx, 1)
	go managedClusterSetController.Run(ctx, 1)
	go managedClusterSetBindingController.Run(ctx, 1)
	go clusterroleController.Run(ctx, 1)
	go addOnHealthCheckController.Run(ctx, 1)
	go addOnFeatureDiscoveryController.Run(ctx, 1)
	if features.DefaultControlplaneMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		go defaultManagedClusterSetController.Run(ctx, 1)
		go globalManagedClusterSetController.Run(ctx, 1)
	}
	go schedulingController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
