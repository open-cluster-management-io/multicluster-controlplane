package addons

import (
	"context"

	"open-cluster-management.io/managed-serviceaccount/pkg/addon/commoncontroller"

	ctrl "sigs.k8s.io/controller-runtime"
)

func SetupManagedServiceAccountWithManager(ctx context.Context, mgr ctrl.Manager) error {
	ctrl := commoncontroller.NewEphemeralIdentityReconciler(mgr.GetCache(), mgr.GetClient())
	if err := ctrl.SetupWithManager(mgr); err != nil {
		return err
	}
	return nil
}
