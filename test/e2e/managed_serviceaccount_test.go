// Copyright Contributors to the Open Cluster Management project
package e2e_test

import (
	"context"
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	msav1alpha1 "open-cluster-management.io/managed-serviceaccount/api/v1alpha1"
	msacommon "open-cluster-management.io/managed-serviceaccount/pkg/common"
)

var _ = ginkgo.Describe("ManagedServiceAccount", ginkgo.Label("addon"), ginkgo.Ordered, func() {
	var controlPlane ControlPlane
	var managedCluster Cluster
	var msaName string

	ginkgo.BeforeAll(func() {
		gomega.Expect(len(options.ControlPlanes) > 0).Should(gomega.BeTrue())
		controlPlane = options.ControlPlanes[0]
		gomega.Expect(len(options.ControlPlanes[0].ManagedCluster) > 0).Should(gomega.BeTrue())
		managedCluster = options.ControlPlanes[0].ManagedCluster[0]
		msaName = "msa-e2e"
	})

	ginkgo.It("managed-serviceaccount addon should be available", func() {
		gomega.Eventually(func() bool {
			availableCount := 0
			for _, controlPlane := range options.ControlPlanes {
				addon := &addonv1alpha1.ManagedClusterAddOn{}
				runtimeClient := runtimeClientMap[controlPlane.Name]
				err := runtimeClient.Get(context.TODO(), types.NamespacedName{
					Namespace: controlPlane.ManagedCluster[0].Name,
					Name:      msacommon.AddonName,
				}, addon)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				if meta.IsStatusConditionTrue(addon.Status.Conditions, addonv1alpha1.ManagedClusterAddOnConditionAvailable) {
					klog.V(5).Infof("managed-serviceaccount addon is available on %s", controlPlane.Name)
					availableCount++
				}
			}
			return availableCount > 0 && availableCount == len(options.ControlPlanes)
		}).WithTimeout(30 * time.Second).Should(gomega.BeTrue())
	})

	// https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/19-projected-serviceaccount-token
	ginkgo.It("token projection should work ", func() {
		ginkgo.By("Create a ManagedServiceAccount on the hub cluster")
		msa := &msav1alpha1.ManagedServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: managedCluster.Name,
				Name:      msaName,
			},
			Spec: msav1alpha1.ManagedServiceAccountSpec{
				Rotation: msav1alpha1.ManagedServiceAccountRotation{
					Enabled:  true,
					Validity: metav1.Duration{Duration: time.Minute * 30},
				},
			},
		}
		err := runtimeClientMap[controlPlane.Name].Create(context.TODO(), msa)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Check the ServiceAccount on the managed cluster")
		gomega.Eventually(func() error {
			return runtimeClientMap[managedCluster.Name].Get(context.TODO(), types.NamespacedName{
				Namespace: msacommon.AddonAgentInstallNamespace,
				Name:      msaName,
			}, &corev1.ServiceAccount{})
		}).WithTimeout(30 * time.Second).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Validate the status of ManagedServiceAccount")
		gomega.Eventually(func() error {
			msa := &msav1alpha1.ManagedServiceAccount{}
			if err := runtimeClientMap[controlPlane.Name].Get(context.TODO(), types.NamespacedName{
				Namespace: managedCluster.Name,
				Name:      msaName,
			}, msa); err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(msa.Status.Conditions, msav1alpha1.ConditionTypeSecretCreated) {
				return fmt.Errorf("the secret: %s/%s has not been created in hub", managedCluster.Name, msaName)
			}
			if !meta.IsStatusConditionTrue(msa.Status.Conditions, msav1alpha1.ConditionTypeTokenReported) {
				return fmt.Errorf("the token has not been reported to secret: %s/%s", managedCluster.Name, msaName)
			}
			if msa.Status.TokenSecretRef == nil {
				return fmt.Errorf("the ManagedServiceAccount not associated any token secret")
			}
			return nil
		}).WithTimeout(30 * time.Second).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("validity of the signing token", func() {
		ginkgo.By("Get the ManagedServiceAccount")
		msa := &msav1alpha1.ManagedServiceAccount{}
		err := runtimeClientMap[controlPlane.Name].Get(context.TODO(), types.NamespacedName{
			Namespace: managedCluster.Name,
			Name:      msaName,
		}, msa)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			ginkgo.By("Get the reported token")
			secret := &corev1.Secret{}
			if err := runtimeClientMap[controlPlane.Name].Get(context.TODO(), types.NamespacedName{
				Namespace: managedCluster.Name,
				Name:      msa.Status.TokenSecretRef.Name,
			}, secret); err != nil {
				return err
			}
			token := secret.Data[corev1.ServiceAccountTokenKey]

			ginkgo.By("Validate the reported token by calling the TokenReview api of the managed cluster")
			tokenReview := &authv1.TokenReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TokenReview",
					APIVersion: "authentication.k8s.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "token-review-request",
				},
				Spec: authv1.TokenReviewSpec{
					Token: string(token),
				},
			}
			if err := runtimeClientMap[managedCluster.Name].Create(context.TODO(), tokenReview); err != nil {
				return err
			}

			if !tokenReview.Status.Authenticated {
				return fmt.Errorf("the secret: %s/%s token should be authenticated by the managed cluster service account", secret.GetNamespace(), secret.GetName())
			}
			expectUserName := fmt.Sprintf("system:serviceaccount:%s:%s", msacommon.AddonAgentInstallNamespace, msaName)
			if tokenReview.Status.User.Username != expectUserName {
				return fmt.Errorf("expect username: %s of the token, but got username: %s", expectUserName, tokenReview.Status.User.Username)
			}
			return nil
		}).WithTimeout(30 * time.Second).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.AfterAll(func() {
		ginkgo.By("Delete the ManagedServiceAccount")
		msa := &msav1alpha1.ManagedServiceAccount{}
		err := runtimeClientMap[controlPlane.Name].Get(context.TODO(), types.NamespacedName{
			Namespace: managedCluster.Name,
			Name:      msaName,
		}, msa)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
		err = runtimeClientMap[controlPlane.Name].Delete(context.TODO(), msa)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Reported secret should be deleted on hub cluster")
		gomega.Eventually(func() bool {
			secret := &corev1.Secret{}
			err := runtimeClientMap[controlPlane.Name].Get(context.TODO(), types.NamespacedName{
				Namespace: msa.GetNamespace(),
				Name:      msa.Status.TokenSecretRef.Name,
			}, secret)
			return errors.IsNotFound(err)
		}, time.Minute, time.Second).Should(gomega.BeTrue())

		ginkgo.By("ServiceAccount should be deleted on managed cluster")
		gomega.Eventually(func() bool {
			serviceAccount := &corev1.ServiceAccount{}
			err := runtimeClientMap[controlPlane.Name].Get(context.TODO(), types.NamespacedName{
				Namespace: msacommon.AddonAgentInstallNamespace,
				Name:      msaName,
			}, serviceAccount)
			return errors.IsNotFound(err)
		}, time.Minute, time.Second).Should(gomega.BeTrue())
	})
})
