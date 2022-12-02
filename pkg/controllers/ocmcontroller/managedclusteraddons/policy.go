// Copyright Contributors to the Open Cluster Management project

package managedclusteraddons

import (
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/api/client/addon/clientset/versioned"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/configpolicy"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/policyframework"

	confighub "open-cluster-management.io/multicluster-controlplane/config/hub"
)

func AddPolicyAddons(addonManager addonmanager.AddonManager, kubeConfig *rest.Config, kubeClient kubernetes.Interface, addonClient versioned.Interface) error {

	eventRecorder := events.NewInMemoryRecorder("policy-addons-controller")

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig:        kubeConfig,
		EventRecorder:     eventRecorder,
		OperatorNamespace: confighub.HubNamespace,
	}

	agentFuncs := []func(addonmanager.AddonManager, *controllercmd.ControllerContext) error{
		policyframework.GetAndAddAgent,
		configpolicy.GetAndAddAgent,
	}

	for _, f := range agentFuncs {
		err := f(addonManager, controllerContext)
		if err != nil {
			return err
		}
	}
	return nil
}
