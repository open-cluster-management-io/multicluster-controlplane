// Copyright Contributors to the Open Cluster Management project

package managedclusteraddons

import (
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/api/client/addon/clientset/versioned"
	"open-cluster-management.io/managed-serviceaccount/pkg/addon/manager"
	"open-cluster-management.io/managed-serviceaccount/pkg/common"
)

func AddManagedServiceAccountAddon(addonManager addonmanager.AddonManager, kubeClient kubernetes.Interface, addonClient versioned.Interface) error {
	//TODO: pass it from parameter
	addonAgentImageName := "quay.io/open-cluster-management/managed-serviceaccount:latest"
	agentInstallAll := true

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

	// add agentaddon to addonmanager
	if err := addonManager.AddAgent(agentAddOn); err != nil {
		return err
	}

	return nil

}
