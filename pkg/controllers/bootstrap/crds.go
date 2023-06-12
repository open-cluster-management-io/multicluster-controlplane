// Copyright Contributors to the Open Cluster Management project
package bootstrap

import (
	"context"
	"embed"
	"time"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	"k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

var baseCRDs = []string{
	"crds/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
	"crds/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml",
	"crds/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml",
	"crds/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
	"crds/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
	"crds/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml",
	"crds/0000_02_addon.open-cluster-management.io_addondeploymentconfigs.crd.yaml",
	"crds/0000_02_clusters.open-cluster-management.io_placements.crd.yaml",
	"crds/0000_03_clusters.open-cluster-management.io_placementdecisions.crd.yaml",
	"crds/0000_05_clusters.open-cluster-management.io_addonplacementscores.crd.yaml",
}

var ocmCRDs = []string{
	"managedclusters.cluster.open-cluster-management.io",
	"managedclustersets.cluster.open-cluster-management.io",
	"managedclustersetbindings.cluster.open-cluster-management.io",
	"manifestworks.work.open-cluster-management.io",
	"clustermanagementaddons.addon.open-cluster-management.io",
	"managedclusteraddons.addon.open-cluster-management.io",
	"addondeploymentconfigs.addon.open-cluster-management.io",
	"placementdecisions.cluster.open-cluster-management.io",
	"placements.cluster.open-cluster-management.io",
	"addonplacementscores.cluster.open-cluster-management.io",
}

var (
	scheme        = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(scheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
}

//go:embed crds/*.yaml
var crdFS embed.FS

func WaitFOROCMCRDsReady(ctx context.Context, crdClient apiextensionsclient.Interface) bool {
	if err := wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		for _, crdName := range ocmCRDs {
			crd, err := crdClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crdName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}

			if !apihelpers.IsCRDConditionTrue(crd, apiextensionsv1.Established) {
				return false, nil
			}

			klog.Infof("ocm crd(%s) is ready", crdName)
		}

		return true, nil
	}); err != nil {
		klog.Errorf("ocm crds are not ready, %w", err)
		return false
	}

	return true
}

func InstallBaseCRDs(ctx context.Context, crdClient apiextensionsclient.Interface) error {
	crdObjs := []*apiextensionsv1.CustomResourceDefinition{}

	for _, crdFileName := range baseCRDs {
		template, err := crdFS.ReadFile(crdFileName)
		if err != nil {
			return err
		}

		objData := assets.MustCreateAssetFromTemplate(crdFileName, template, nil).Data
		obj, _, err := genericCodec.Decode(objData, nil, nil)
		if err != nil {
			return err
		}

		switch required := obj.(type) {
		case *apiextensionsv1.CustomResourceDefinition:
			crdObjs = append(crdObjs, required)
		}
	}

	return wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		for _, required := range crdObjs {
			crd, _, err := resourceapply.ApplyCustomResourceDefinitionV1(
				ctx,
				crdClient.ApiextensionsV1(),
				util.NewLoggingRecorder("crd-generator"),
				required,
			)
			if err != nil {
				klog.Errorf("fail to apply %s due to %v", required.Name, err)
				return false, nil
			}

			if !apihelpers.IsCRDConditionTrue(crd, apiextensionsv1.Established) {
				return false, nil
			}
		}

		return true, nil
	})
}
