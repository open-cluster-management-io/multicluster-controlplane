// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/pkg/errors"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	kubeevents "k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/managed-serviceaccount/pkg/addon/manager"
	"open-cluster-management.io/managed-serviceaccount/pkg/common"
	scheduling "open-cluster-management.io/placement/pkg/controllers/scheduling"
	"open-cluster-management.io/placement/pkg/debugger"
	"open-cluster-management.io/registration/pkg/features"
	"open-cluster-management.io/registration/pkg/helpers"
	"open-cluster-management.io/registration/pkg/hub/addon"
	"open-cluster-management.io/registration/pkg/hub/clusterrole"
	"open-cluster-management.io/registration/pkg/hub/csr"
	"open-cluster-management.io/registration/pkg/hub/lease"
	"open-cluster-management.io/registration/pkg/hub/managedcluster"
	"open-cluster-management.io/registration/pkg/hub/managedclusterset"
	"open-cluster-management.io/registration/pkg/hub/managedclustersetbinding"
	"open-cluster-management.io/registration/pkg/hub/rbacfinalizerdeletion"
	"open-cluster-management.io/registration/pkg/hub/taint"

	confighub "open-cluster-management.io/multicluster-controlplane/config/hub"
)

var ResyncInterval = 5 * time.Minute

func InstallOCMControllers(ctx context.Context, kubeConfig *rest.Config,
	kubeClient kubernetes.Interface, kubeInfomers kubeinformers.SharedInformerFactory) error {
	eventRecorder := events.NewInMemoryRecorder("registration-controller")

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig:        kubeConfig,
		EventRecorder:     eventRecorder,
		OperatorNamespace: confighub.HubNamespace,
	}

	clusterClient, err := clusterv1client.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	workClient, err := workv1client.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	addOnClient, err := addonclient.NewForConfig(kubeConfig)
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

	var csrController factory.Controller
	if features.DefaultHubMutableFeatureGate.Enabled(ocmfeature.V1beta1CSRAPICompatibility) {
		v1CSRSupported, v1beta1CSRSupported, err := helpers.IsCSRSupported(kubeClient)
		if err != nil {
			return errors.Wrapf(err, "failed CSR api discovery")
		}

		if !v1CSRSupported && v1beta1CSRSupported {
			csrController = csr.NewV1beta1CSRApprovingController(
				kubeClient,
				kubeInfomers.Certificates().V1beta1().CertificateSigningRequests(),
				controllerContext.EventRecorder,
			)
			klog.Info("Using v1beta1 CSR api to manage spoke client certificate")
		}
	}
	if csrController == nil {
		csrController = csr.NewCSRApprovingController(
			kubeClient,
			kubeInfomers.Certificates().V1().CertificateSigningRequests(),
			controllerContext.EventRecorder,
		)
	}

	leaseController := lease.NewClusterLeaseController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInfomers.Coordination().V1().Leases(),
		ResyncInterval, //TODO: this interval time should be allowed to change from outside
		controllerContext.EventRecorder,
	)

	rbacFinalizerController := rbacfinalizerdeletion.NewFinalizeController(
		kubeInfomers.Rbac().V1().Roles(),
		kubeInfomers.Rbac().V1().RoleBindings(),
		kubeInfomers.Core().V1().Namespaces().Lister(),
		clusterInformers.Cluster().V1().ManagedClusters().Lister(),
		workInformers.Work().V1().ManifestWorks().Lister(),
		kubeClient.RbacV1(),
		controllerContext.EventRecorder,
	)

	managedClusterSetController := managedclusterset.NewManagedClusterSetController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
		controllerContext.EventRecorder,
	)

	managedClusterSetBindingController := managedclustersetbinding.NewManagedClusterSetBindingController(
		clusterClient,
		clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta1().ManagedClusterSetBindings(),
		controllerContext.EventRecorder,
	)

	clusterroleController := clusterrole.NewManagedClusterClusterroleController(
		kubeClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInfomers.Rbac().V1().ClusterRoles(),
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
	if features.DefaultHubMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		defaultManagedClusterSetController = managedclusterset.NewDefaultManagedClusterSetController(
			clusterClient.ClusterV1beta1(),
			clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
			controllerContext.EventRecorder,
		)
		globalManagedClusterSetController = managedclusterset.NewGlobalManagedClusterSetController(
			clusterClient.ClusterV1beta1(),
			clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
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
		clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta1().ManagedClusterSetBindings(),
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
		scheduler,
		controllerContext.EventRecorder, recorder,
	)

	schedulingControllerResync := scheduling.NewSchedulingControllerResync(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta1().ManagedClusterSetBindings(),
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
		scheduler,
		controllerContext.EventRecorder, recorder,
	)
	go clusterInformers.Start(ctx.Done())
	go workInformers.Start(ctx.Done())
	go addOnInformers.Start(ctx.Done())

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
	if features.DefaultHubMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		go defaultManagedClusterSetController.Run(ctx, 1)
		go globalManagedClusterSetController.Run(ctx, 1)
	}
	go schedulingController.Run(ctx, 1)
	go schedulingControllerResync.Run(ctx, 1)

	<-ctx.Done()
	return nil
}

func InstallOCMAddonManager(ctx context.Context, kubeConfig *rest.Config, kubeClient kubernetes.Interface, kubeInfomers kubeinformers.SharedInformerFactory) error {
	//TODO: pass it from parameter
	addonAgentImageName := "quay.io/open-cluster-management/managed-serviceaccount:latest"
	agentInstallAll := true

	addonManager, err := addonmanager.New(kubeConfig)
	if err != nil {
		return err
	}
	addonClient, err := addonclient.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}
	kubeInfomers.WaitForCacheSync(ctx.Done())
	// cache.WaitForCacheSync(ctx.Done(), kubeInfomers.Certificates().V1().CertificateSigningRequests().Informer().HasSynced)
	// TODO: support standalone controlplane
	// hubNamespace := os.Getenv("NAMESPACE")
	// if len(hubNamespace) == 0 {
	// 	inClusterNamespace, err := util.GetInClusterNamespace()
	// 	if err != nil {
	// 		return err
	// 	}
	// 	hubNamespace = inClusterNamespace
	// }

	// if len(imagePullSecretName) == 0 {
	// 	imagePullSecretName = os.Getenv("AGENT_IMAGE_PULL_SECRET")
	// }

	// imagePullSecret := &corev1.Secret{}
	// if len(imagePullSecretName) != 0 {
	// 	imagePullSecret, err = kubeClient.CoreV1().Secrets(hubNamespace).Get(
	// 		context.TODO(),
	// 		imagePullSecretName,
	// 		metav1.GetOptions{},
	// 	)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	if imagePullSecret.Type != corev1.SecretTypeDockerConfigJson {
	// 		return err
	// 	}
	// }

	agentFactory := addonfactory.NewAgentAddonFactory(common.AddonName, manager.FS, "manifests/templates").
		WithConfigGVRs(addonfactory.AddOnDeploymentConfigGVR).
		WithGetValuesFuncs(
			manager.GetDefaultValues(addonAgentImageName, nil),
			addonfactory.GetAddOnDeloymentConfigValues(
				addonfactory.NewAddOnDeloymentConfigGetter(addonClient),
				addonfactory.ToAddOnDeloymentConfigValues,
			),
		).
		WithAgentRegistrationOption(manager.NewRegistrationOption(kubeClient))

	if agentInstallAll {
		agentFactory.WithInstallStrategy(agent.InstallAllStrategy(common.AddonAgentInstallNamespace))
	}

	agentAddOn, err := agentFactory.BuildTemplateAgentAddon()
	if err != nil {
		return err
	}

	if err := addonManager.AddAgent(agentAddOn); err != nil {
		return err
	}
	if err := addonManager.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}
