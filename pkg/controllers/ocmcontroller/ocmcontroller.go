// Copyright Contributors to the Open Cluster Management project
package ocmcontroller

import (
	"context"
	_ "net/http/pprof"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/client-go/rest"

	placement "open-cluster-management.io/placement/pkg/controllers"
	registration "open-cluster-management.io/registration/pkg/hub"

	confighub "open-cluster-management.io/multicluster-controlplane/config/hub"
)

func InstallRegistraionControllers(ctx context.Context, kubeConfig *rest.Config) error {
	eventRecorder := events.NewInMemoryRecorder("registration-controller")

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig:        kubeConfig,
		EventRecorder:     eventRecorder,
		OperatorNamespace: confighub.HubNameSpace,
	}

	return registration.RunControllerManager(ctx, controllerContext)
}

func InstallPlacementControllers(ctx context.Context, kubeConfig *rest.Config) error {
	eventRecorder := events.NewInMemoryRecorder("placement-controller")

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig:        kubeConfig,
		EventRecorder:     eventRecorder,
		OperatorNamespace: confighub.HubNameSpace,
	}

	return placement.RunControllerManager(ctx, controllerContext)
}
