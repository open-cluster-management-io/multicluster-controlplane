// Copyright Contributors to the Open Cluster Management project

package clustermanagementaddons

import (
	"context"
	"os"
	"strings"

	k8sdepwatches "github.com/stolostron/kubernetes-dependency-watches/client"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	policyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	automationctrl "open-cluster-management.io/governance-policy-propagator/controllers/automation"
	encryptionkeysctrl "open-cluster-management.io/governance-policy-propagator/controllers/encryptionkeys"
	metricsctrl "open-cluster-management.io/governance-policy-propagator/controllers/policymetrics"
	policysetctrl "open-cluster-management.io/governance-policy-propagator/controllers/policyset"
	propagatorctrl "open-cluster-management.io/governance-policy-propagator/controllers/propagator"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SetupPolicyWithManager(ctx context.Context, mgr ctrl.Manager, kubeconfig *rest.Config,
	kubeClient kubernetes.Interface, dynamicClient dynamic.Interface) error {

	klog.Info("SetupPolicyWithManager")

	dynamicWatcherReconciler, dynamicWatcherSource := k8sdepwatches.NewControllerRuntimeSource()

	dynamicWatcher, err := k8sdepwatches.New(kubeconfig, dynamicWatcherReconciler, nil)
	if err != nil {
		return err
	}

	go func() {
		err := dynamicWatcher.Start(ctx)
		if err != nil {
			klog.Error(err, "Unable to start the dynamic watcher", "controller", propagatorctrl.ControllerName)
		}
	}()

	if err = (&propagatorctrl.PolicyReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		Recorder:       mgr.GetEventRecorderFor(propagatorctrl.ControllerName),
		DynamicWatcher: dynamicWatcher,
	}).SetupWithManager(mgr, dynamicWatcherSource); err != nil {
		return err
	}

	if reportMetrics() {
		if err = (&metricsctrl.MetricReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			return err
		}
	}

	if err = (&automationctrl.PolicyAutomationReconciler{
		Client:        mgr.GetClient(),
		DynamicClient: dynamicClient,
		Scheme:        mgr.GetScheme(),
		Recorder:      mgr.GetEventRecorderFor(automationctrl.ControllerName),
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	if err = (&policysetctrl.PolicySetReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor(policysetctrl.ControllerName),
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	//TODO: allow KeyRotationDays & MaxConcurrentReconciles configuration
	if err = (&encryptionkeysctrl.EncryptionKeysReconciler{
		Client:                  mgr.GetClient(),
		KeyRotationDays:         30,
		MaxConcurrentReconciles: 10,
		Scheme:                  mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	propagatorctrl.Initialize(kubeconfig, &kubeClient)

	cache := mgr.GetCache()

	// The following index for the PlacementRef Name is being added to the
	// client cache to improve the performance of querying PlacementBindings
	indexFunc := func(obj client.Object) []string {
		return []string{obj.(*policyv1.PlacementBinding).PlacementRef.Name}
	}

	if err := cache.IndexField(
		context.TODO(), &policyv1.PlacementBinding{}, "placementRef.name", indexFunc,
	); err != nil {
		panic(err)
	}

	klog.Info("Waiting for the dynamic watcher to start")
	// This is important to avoid adding watches before the dynamic watcher is ready
	<-dynamicWatcher.Started()

	return nil
}

// reportMetrics returns a bool on whether to report GRC metrics from the propagator
func reportMetrics() bool {
	metrics, _ := os.LookupEnv("DISABLE_REPORT_METRICS")

	return !strings.EqualFold(metrics, "true")
}
