// Copyright Contributors to the Open Cluster Management project
package bootstrap

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	tokenapi "k8s.io/cluster-bootstrap/token/api"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/cmd/kubeadm/app/phases/bootstraptoken/clusterinfo"
	"k8s.io/kubernetes/cmd/kubeadm/app/util/apiclient"
)

var letterRunes_az09 = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func BuildKubeSystemResources(ctx context.Context, config server.Config, kubeClient kubernetes.Interface) error {
	// prepare default namespace
	if _, err := kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: metav1.NamespaceDefault,
		},
	}, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		klog.Errorf("failed to prepare default namespace: %v", err)
	}

	// perpare cluster-info configmap in kube-public namespace
	if err := wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		if _, err := kubeClient.CoreV1().Namespaces().Get(ctx, metav1.NamespacePublic, metav1.GetOptions{}); err != nil {
			// waiting the kube-public namespace
			return false, nil
		}

		if err := prepareClusterInfoConfigmap(config, kubeClient); err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		klog.Errorf("failed to prepare cluster-info configmap in kube-public namespace: %v", err)
	}

	// prepare bootstrap token secret
	if err := wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		if _, err := kubeClient.CoreV1().Namespaces().Get(ctx, metav1.NamespaceSystem, metav1.GetOptions{}); err != nil {
			// waiting the kube-system namespace
			return false, nil
		}

		if err := prepareBootstrapTokenSecret(ctx, kubeClient); err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		klog.Errorf("failed to prepare bootstrap token secret in kube-system namespace: %v", err)
	}

	// prepare clusterroles
	if err := wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		clusterRoles := []*rbacv1.ClusterRole{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "system:open-cluster-management:bootstrap",
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
						Verbs:     []string{"get"},
					},
					{
						APIGroups: []string{"certificates.k8s.io"},
						Resources: []string{"certificatesigningrequests"},
						Verbs:     []string{"get", "list", "watch", "create"},
					},
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclusters"},
						Verbs:     []string{"get", "list", "update", "create"},
					},
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"managedclustersets/join"},
						Verbs:     []string{"create"},
					},
				},
			},
		}

		for _, clusterRole := range clusterRoles {
			if err := prepareClusterRole(ctx, kubeClient, clusterRole); err != nil {
				return false, err
			}
		}

		return true, nil
	}); err != nil {
		klog.Errorf("failed to prepare clusterroles': %w", err)
	}

	// prepare clusterrolebindings
	if err := wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		clusterRoleBindings := []*rbacv1.ClusterRoleBinding{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-bootstrap",
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "system:open-cluster-management:bootstrap",
				},
				Subjects: []rbacv1.Subject{
					{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Group",
						Name:     "system:bootstrappers:managedcluster",
					},
				},
			},
			// allow user `kube:admin` access the controlplane as cluster admin
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "kube-admin",
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "cluster-admin",
				},
				Subjects: []rbacv1.Subject{
					{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "User",
						Name:     "kube:admin",
					},
				},
			},
		}

		for _, clusterclusterRoleBinding := range clusterRoleBindings {
			if err := prepareClusterRoleBinding(ctx, kubeClient, clusterclusterRoleBinding); err != nil {
				return false, err
			}
		}

		return true, nil
	}); err != nil {
		klog.Errorf("failed to prepare clusterrolebindings: %w", err)
	}

	return nil
}

func prepareClusterInfoConfigmap(config server.Config, kubeClient kubernetes.Interface) error {
	caData, _ := config.SecureServing.Cert.CurrentCertKeyContent()
	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"": {
				Server:                   "https://" + config.ExternalAddress,
				CertificateAuthorityData: caData,
			},
		},
	}

	kubeconfigRaw, err := clientcmd.Write(kubeconfig)
	if err != nil {
		return err
	}

	if err := apiclient.CreateOrUpdateConfigMap(kubeClient, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tokenapi.ConfigMapClusterInfo,
			Namespace: metav1.NamespacePublic,
		},
		Data: map[string]string{
			tokenapi.KubeConfigKey: string(kubeconfigRaw),
		},
	}); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	if err := clusterinfo.CreateClusterInfoRBACRules(kubeClient); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func prepareBootstrapTokenSecret(ctx context.Context, kubeClient kubernetes.Interface) error {
	tokenID := randStringRunes(6, letterRunes_az09)
	tokenSecret := randStringRunes(16, letterRunes_az09)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("bootstrap-token-%s", tokenID),
			Labels: map[string]string{"app": "cluster-manager"},
		},
		Type: corev1.SecretTypeBootstrapToken,
		StringData: map[string]string{
			"token-id":                       tokenID,
			"token-secret":                   tokenSecret,
			"usage-bootstrap-authentication": "true",
			"auth-extra-groups":              "system:bootstrappers:managedcluster",
		},
	}

	if _, err := kubeClient.CoreV1().Secrets(metav1.NamespaceSystem).Create(
		ctx, secret, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func prepareClusterRole(ctx context.Context, kubeClient kubernetes.Interface, required *rbacv1.ClusterRole) error {
	existing, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, required.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := kubeClient.RbacV1().ClusterRoles().Create(ctx, required, metav1.CreateOptions{})
		return err
	}

	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(existing.Rules, required.Rules) {
		return nil
	}

	// if the clusterrolebinding exists, update it to the latest one
	_, err = kubeClient.RbacV1().ClusterRoles().Update(ctx, required, metav1.UpdateOptions{})
	return err
}

func prepareClusterRoleBinding(ctx context.Context, kubeClient kubernetes.Interface, required *rbacv1.ClusterRoleBinding) error {
	existing, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, required.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, required, metav1.CreateOptions{})
		return err
	}

	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(existing.RoleRef, required.RoleRef) &&
		equality.Semantic.DeepEqual(existing.Subjects, required.Subjects) {
		return nil
	}

	// if the clusterrolebinding exists, update it to the latest one
	_, err = kubeClient.RbacV1().ClusterRoleBindings().Update(ctx, required, metav1.UpdateOptions{})
	return err
}

func randStringRunes(n int, runes []rune) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = runes[rand.Intn(len(runes))]
	}
	return string(b)
}
