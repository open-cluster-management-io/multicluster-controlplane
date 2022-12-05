// Copyright Contributors to the Open Cluster Management project

package clustermanagementaddons

import (
	"open-cluster-management.io/managed-serviceaccount/pkg/addon/manager"
	"open-cluster-management.io/managed-serviceaccount/pkg/features"
	ctrl "sigs.k8s.io/controller-runtime"
)

func SetupManagedServiceAccountWithManager(mgr ctrl.Manager) error {
	if features.FeatureGates.Enabled(features.EphemeralIdentity) {
		if err := (manager.NewEphemeralIdentityReconciler(
			mgr.GetCache(),
			mgr.GetClient(),
		)).SetupWithManager(mgr); err != nil {
			return err
		}
	}
	return nil
}
