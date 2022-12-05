// Copyright Contributors to the Open Cluster Management project

package managedclusteraddons

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	"open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policyaddon "open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/configpolicy"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/policyframework"

	confighub "open-cluster-management.io/multicluster-controlplane/config/hub"
)

const (
	policyFrameworkAddonName        = "governance-policy-framework"
	configPolicyAddonName           = "config-policy-controller"
	evaluationConcurrencyAnnotation = "policy-evaluation-concurrency"
	prometheusEnabledAnnotation     = "prometheus-metrics-enabled"
	configPolicyControllerImage     = "quay.io/open-cluster-management/config-policy-controller:latest"
	kubeRBACProxyImage              = "registry.redhat.io/openshift4/ose-kube-rbac-proxy:v4.10"
	policyFrameworkAddonImage       = "quay.io/open-cluster-management/governance-policy-framework-addon:latest"
)

var agentPermissionFiles = []string{
	// role with RBAC rules to access resources on hub
	"manifests/hubpermissions/role.yaml",
	// rolebinding to bind the above role to a certain user group
	"manifests/hubpermissions/rolebinding.yaml",
}

func AddPolicyAddons(addonManager addonmanager.AddonManager, kubeConfig *rest.Config, kubeClient kubernetes.Interface, addonClient versioned.Interface) error {

	eventRecorder := events.NewInMemoryRecorder("policy-addons-controller")

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig:        kubeConfig,
		EventRecorder:     eventRecorder,
		OperatorNamespace: confighub.HubNamespace,
	}

	registrationOption := policyaddon.NewRegistrationOption(
		controllerContext,
		policyFrameworkAddonName,
		agentPermissionFiles,
		policyframework.FS)

	policyFrameworkAddon, err := addonfactory.NewAgentAddonFactory(policyFrameworkAddonName, policyframework.FS, "manifests/managedclusterchart").
		WithGetValuesFuncs(getPolicyFrameworkValues, addonfactory.GetValuesFromAddonAnnotation).
		WithInstallStrategy(agent.InstallAllStrategy(addonfactory.AddonDefaultInstallNamespace)).
		WithAgentRegistrationOption(registrationOption).
		BuildHelmAgentAddon()
	if err != nil {
		return err
	}

	registrationOption = policyaddon.NewRegistrationOption(
		controllerContext,
		configPolicyAddonName,
		agentPermissionFiles,
		configpolicy.FS)

	configPolicyAddon, err := addonfactory.NewAgentAddonFactory(configPolicyAddonName, configpolicy.FS, "manifests/managedclusterchart").
		WithGetValuesFuncs(getConfigPolicyValues, addonfactory.GetValuesFromAddonAnnotation).
		WithInstallStrategy(agent.InstallAllStrategy(addonfactory.AddonDefaultInstallNamespace)).
		WithScheme(policyaddon.Scheme).
		WithAgentRegistrationOption(registrationOption).
		BuildHelmAgentAddon()

	if err != nil {
		return err
	}

	// add policyFrameworkaddon to addonmanager
	if err := addonManager.AddAgent(policyFrameworkAddon); err != nil {
		return err
	}

	// add configPolicyAddon to addonmanager
	if err := addonManager.AddAgent(configPolicyAddon); err != nil {
		return err
	}

	return nil
}

type userValues struct {
	OnMulticlusterHub bool                     `json:"onMulticlusterHub"`
	GlobalValues      policyaddon.GlobalValues `json:"global"`
	UserArgs          policyaddon.UserArgs     `json:"args"`
}

func getPolicyFrameworkValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	image := os.Getenv("GOVERNANCE_POLICY_FRAMEWORK_ADDON_IMAGE")
	if image == "" {
		image = policyFrameworkAddonImage
	}
	userValues := userValues{
		OnMulticlusterHub: false,
		GlobalValues: policyaddon.GlobalValues{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "open-cluster-management-image-pull-credentials",
			ImageOverrides: map[string]string{
				"governance_policy_framework_addon": image,
			},
			NodeSelector: map[string]string{},
			ProxyConfig: map[string]string{
				"HTTP_PROXY":  "",
				"HTTPS_PROXY": "",
				"NO_PROXY":    "",
			},
		},
		UserArgs: policyaddon.UserArgs{
			LogEncoder:  "console",
			LogLevel:    0,
			PkgLogLevel: -1,
		},
	}
	// special case for local-cluster
	if cluster.Name == "local-cluster" {
		userValues.OnMulticlusterHub = true
	}

	if val, ok := addon.GetAnnotations()["addon.open-cluster-management.io/on-multicluster-hub"]; ok {
		if strings.EqualFold(val, "true") {
			userValues.OnMulticlusterHub = true
		} else if strings.EqualFold(val, "false") {
			// the special case can still be overridden by this annotation
			userValues.OnMulticlusterHub = false
		}
	}

	if val, ok := addon.GetAnnotations()[policyaddon.PolicyLogLevelAnnotation]; ok {
		logLevel := policyaddon.GetLogLevel(policyFrameworkAddonName, val)
		userValues.UserArgs.LogLevel = logLevel
		userValues.UserArgs.PkgLogLevel = logLevel - 2
	}

	return addonfactory.JsonStructToValues(userValues)
}

func getConfigPolicyValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	configImage := os.Getenv("CONFIG_POLICY_CONTROLLER_IMAGE")
	if configImage == "" {
		configImage = configPolicyControllerImage
	}
	proxyImage := os.Getenv("KUBE_RBAC_PROXY_IMAGE")
	if proxyImage == "" {
		proxyImage = kubeRBACProxyImage
	}
	userValues := configpolicy.UserValues{
		GlobalValues: policyaddon.GlobalValues{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "open-cluster-management-image-pull-credentials",
			ImageOverrides: map[string]string{
				"config_policy_controller": configImage,
				"kube_rbac_proxy":          proxyImage,
			},
			NodeSelector: map[string]string{},
			ProxyConfig: map[string]string{
				"HTTP_PROXY":  "",
				"HTTPS_PROXY": "",
				"NO_PROXY":    "",
			},
		},
		Prometheus: map[string]interface{}{},
		UserArgs: configpolicy.UserArgs{
			UserArgs: policyaddon.UserArgs{
				LogEncoder:  "console",
				LogLevel:    0,
				PkgLogLevel: -1,
			},
			EvaluationConcurrency: 2,
		},
	}

	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "product.open-cluster-management.io" {
			userValues.KubernetesDistribution = cc.Value

			break
		}
	}

	if val, ok := addon.GetAnnotations()[policyaddon.PolicyLogLevelAnnotation]; ok {
		logLevel := policyaddon.GetLogLevel(configPolicyAddonName, val)
		userValues.UserArgs.LogLevel = logLevel
		userValues.UserArgs.PkgLogLevel = logLevel - 2
	}

	if val, ok := addon.GetAnnotations()[evaluationConcurrencyAnnotation]; ok {
		value, err := strconv.ParseUint(val, 10, 8)
		if err != nil {
			klog.Error(err, fmt.Sprintf(
				"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %d)",
				evaluationConcurrencyAnnotation, val, configPolicyAddonName, userValues.UserArgs.EvaluationConcurrency),
			)
		} else {
			// This is safe because we specified the uint8 in ParseUint
			userValues.UserArgs.EvaluationConcurrency = uint8(value)
		}
	}

	// Enable Prometheus metrics by default on OpenShift
	userValues.Prometheus["enabled"] = userValues.KubernetesDistribution == "OpenShift"
	if userValues.KubernetesDistribution == "OpenShift" {
		userValues.Prometheus["serviceMonitor"] = map[string]interface{}{"namespace": "openshift-monitoring"}
	}

	if val, ok := addon.GetAnnotations()[prometheusEnabledAnnotation]; ok {
		valBool, err := strconv.ParseBool(val)
		if err != nil {
			klog.Error(err, fmt.Sprintf(
				"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %v)",
				prometheusEnabledAnnotation, val, configPolicyAddonName, userValues.Prometheus["enabled"]),
			)
		} else {
			userValues.Prometheus["enabled"] = valBool
		}
	}

	return addonfactory.JsonStructToValues(userValues)
}
