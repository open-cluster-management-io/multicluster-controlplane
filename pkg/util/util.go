// Copyright Contributors to the Open Cluster Management project
package util

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	ocpconfigclientset "github.com/openshift/client-go/config/clientset/versioned"
	ocprouteclientset "github.com/openshift/client-go/route/clientset/versioned"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"
)

const (
	defaultComponentNamespace = "multicluster-controlplane"
	defaultServiceName        = "multicluster-controlplane"
	defaultRouteName          = "multicluster-controlplane"
)

// KubeConfigWithClientCerts creates a kubeconfig authenticating with client cert/key
// and write it to `path`
func KubeconfigWriteToFile(filename string, clusterURL string, clusterTrustBundle []byte, clientCertPEM []byte, clientKeyPEM []byte) error {
	config, err := toKubeconfig(clusterURL, clusterTrustBundle, clientCertPEM, clientKeyPEM)
	if err != nil {
		return err
	}

	dir := filepath.Dir(filename)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filename, config, 0600); err != nil {
		return err
	}
	return nil
}

// GetExternalHost get the generated external IP from service
func GetExternalHost() (string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Infof("Trying to get current bind address from local node")
		ip, err := utilnet.ResolveBindAddress(netutils.ParseIPSloppy("0.0.0.0"))
		return ip.String(), err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", err
	}

	ocpRouteClient, err := ocprouteclientset.NewForConfig(config)
	if err != nil {
		return "", err
	}

	ns := GetComponentNamespace()
	svc, err := clientset.CoreV1().Services(ns).Get(context.TODO(), defaultServiceName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	switch svc.Spec.Type {
	case corev1.ServiceTypeClusterIP:
		// TODO(ycyaoxdu): only hanlde the ocp env, need to handle other cases
		klog.Infof("Trying to get external host name from ocp route")
		var host string
		err := retry.OnError(
			retry.DefaultRetry,
			func(err error) bool { return true },
			func() error {
				route, err := ocpRouteClient.RouteV1().Routes(ns).Get(context.TODO(), defaultRouteName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(route.Status.Ingress) == 0 {
					return fmt.Errorf("ingress not found, retrying")
				}

				host = route.Status.Ingress[0].Host
				if len(host) == 0 {
					return fmt.Errorf("failed to find the host from the route %s/%s ingress, retrying", ns, defaultRouteName)
				}

				return nil
			},
		)

		return host, err
	case corev1.ServiceTypeLoadBalancer:
		// TODO only hanlde the eks env, need to handle other cases
		klog.Infof("Trying to get external host name from load balancer servcie")
		var host string
		err := retry.OnError(
			retry.DefaultRetry,
			func(err error) bool { return true },
			func() error {
				s, err := clientset.CoreV1().Services(ns).Get(context.TODO(), defaultServiceName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(s.Status.LoadBalancer.Ingress) == 0 {
					return fmt.Errorf("ingress not found, retrying")
				}

				host = s.Status.LoadBalancer.Ingress[0].Hostname
				if len(host) == 0 {
					return fmt.Errorf("failed to find the host from the service %s/%s ingress, retrying", ns, defaultServiceName)
				}
				return nil
			},
		)

		return host, err
	}

	return "", fmt.Errorf("the type of current service %s/%s is not suppored", ns, defaultServiceName)
}

// KubeConfigWithClientCerts creates a kubeconfig authenticating with client cert/key
// and write it to secret "kubeconfig"
func KubeconfigWroteToSecret(config *rest.Config, secretName, clusterURL string, clusterTrustBundle, clientCertPEM, clientKeyPEM []byte) error {
	kubeconfig, err := toKubeconfig(clusterURL, clusterTrustBundle, clientCertPEM, clientKeyPEM)
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	ns := GetComponentNamespace()
	sec, err := clientset.CoreV1().Secrets(ns).Get(context.Background(), secretName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: secretName,
			},
			Data: map[string][]byte{
				"kubeconfig": kubeconfig,
			},
		}
		_, err := clientset.CoreV1().Secrets(ns).Create(context.Background(), newSecret, metav1.CreateOptions{})
		return err
	}

	if err != nil {
		return err
	}

	if bytes.Equal(sec.Data["kubeconfig"], kubeconfig) {
		return nil
	}

	sec.Data["kubeconfig"] = kubeconfig
	_, err = clientset.CoreV1().Secrets(ns).Update(context.Background(), sec, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	klog.Infof("Secret kubeconfig created in Namespace %s", ns)
	return nil
}

func toKubeconfig(clusterURL string, clusterTrustBundle []byte, clientCertPEM []byte, clientKeyPEM []byte) ([]byte, error) {
	const mcName = "multicluster-controlplane"

	cluster := clientcmdapi.NewCluster()
	cluster.Server = clusterURL
	cluster.CertificateAuthorityData = clusterTrustBundle

	mcContext := clientcmdapi.NewContext()
	mcContext.Cluster = mcName
	mcContext.Namespace = "default"
	mcContext.AuthInfo = "user"

	mcUser := clientcmdapi.NewAuthInfo()
	mcUser.ClientCertificateData = clientCertPEM
	mcUser.ClientKeyData = clientKeyPEM

	kubeConfig := clientcmdapi.Config{
		CurrentContext: mcName,
		Clusters:       map[string]*clientcmdapi.Cluster{mcName: cluster},
		Contexts:       map[string]*clientcmdapi.Context{mcName: mcContext},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"user": mcUser},
	}
	content, err := clientcmd.Write(kubeConfig)
	if err != nil {
		return nil, err
	}

	return content, nil
}

func GenerateServiceAccountKey(file string) error {
	bitSize := 2048

	// Generate RSA key.
	key, err := rsa.GenerateKey(rand.Reader, bitSize)
	if err != nil {
		return err
	}

	// Encode private key to PKCS#1 ASN.1 PEM.
	keyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		},
	)

	// Write private key to file.
	if err := os.WriteFile(file, keyPEM, 0700); err != nil {
		return err
	}

	return nil
}

func GetComponentNamespace() string {
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return defaultComponentNamespace
	}
	return string(nsBytes)
}

// GenerateSelfManagedClusterName generates an ID for the self management cluster.
// If the worker manager addon is enabled, this ID will be same with `id.k8s.io` cluster claim.
// We get ID by the sequence of: openshiftID, uid of kube-system namespace, random UID.
func GenerateSelfManagedClusterName(ctx context.Context, inClusterConfig *rest.Config) (string, error) {
	kubeClient, err := kubernetes.NewForConfig(inClusterConfig)
	if err != nil {
		return "", err
	}

	ocpConfigClient, err := ocpconfigclientset.NewForConfig(inClusterConfig)
	if err != nil {
		return "", err
	}

	ocpClusterVersion, err := ocpConfigClient.ConfigV1().ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		ns, err := kubeClient.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
		if err != nil {
			return string(uuid.NewUUID()), nil
		}

		return string(ns.UID), nil
	}

	if err != nil {
		return "", err
	}

	return string(ocpClusterVersion.Spec.ClusterID), nil
}

func GoContext(stopCh <-chan struct{}) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func(done <-chan struct{}) {
		<-done
		cancel()
	}(stopCh)
	return ctx
}

func LoadServingSigner(signerPath string) (bool, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// the controlplane is not running at a cluster, ignore
		return false, nil
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return false, err
	}

	// try to load the ca from ocp env
	// TODO investage other platforms, like eks, aks, gcp etc.
	ns := "openshift-kube-apiserver-operator"
	name := "loadbalancer-serving-signer"
	secret, err := kubeClient.CoreV1().Secrets(ns).Get(context.TODO(), name, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		// the serving signer is not found, ignore
		return false, nil
	case err != nil:
		return false, err
	}

	// ensure parent dir
	if err := os.MkdirAll(signerPath, os.FileMode(0755)); err != nil {
		return false, err
	}

	certBlock, existing := secret.Data["tls.crt"]
	if !existing {
		return false, fmt.Errorf("there is no `tls.crt` in %s/%s", ns, name)
	}

	keyBlock, existing := secret.Data["tls.key"]
	if !existing {
		return false, fmt.Errorf("there is no `tls.key` in %s/%s", ns, name)
	}

	certFileWriter, err := os.OpenFile(filepath.Join(signerPath, "ca.crt"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return false, err
	}
	defer certFileWriter.Close()

	keyFileWriter, err := os.OpenFile(filepath.Join(signerPath, "ca.key"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return false, err
	}
	defer keyFileWriter.Close()

	if _, err := certFileWriter.Write(certBlock); err != nil {
		return false, err
	}

	if _, err := keyFileWriter.Write(keyBlock); err != nil {
		return false, err
	}

	klog.Infof("Load server ca from %s/%s to %s", ns, name, signerPath)
	return true, nil
}
