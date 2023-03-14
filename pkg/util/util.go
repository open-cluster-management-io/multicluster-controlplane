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
	"net"
	"os"
	"path/filepath"
	"unsafe"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

const (
	defaultComponentNamespace = "multicluster-controlplane"
	secretName                = "multicluster-controlplane-kubeconfig"
	defaultServiceName        = "multicluster-controlplane"
	defaultRouteName          = "multicluster-controlplane"
	serviceClusterIP          = "ClusterIP"
	serviceNodePort           = "NodePort"
	serviceLoadBalancer       = "LoadBalancer"
	serviceExternalName       = "ExternalName"
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

// GetExternalIP get the generated external IP from service
func GetExternalIP() (string, error) {
	// deploy mode, find external ip
	config, err := rest.InClusterConfig()
	if err != nil {
		// not running in a cluster, try to find local ip
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			ipAddr, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipAddr.IP.IsLoopback() {
				continue
			}
			if !ipAddr.IP.IsGlobalUnicast() {
				continue
			}
			return ipAddr.IP.String(), nil
		}
		return "", err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", err
	}

	ns := GetComponentNamespace()
	svc, err := clientset.CoreV1().Services(ns).Get(context.TODO(), defaultServiceName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	var host string
	switch svc.Spec.Type {
	case serviceClusterIP:
		// TODO(ycyaoxdu): need to handle other cases
		dynamicClient, err := dynamic.NewForConfig(config)
		if err != nil {
			return "", err
		}
		// for ocp
		routeRes := routev1.GroupVersion.WithResource("route")
		errGet := retry.OnError(retry.DefaultRetry, func(err error) bool {
			return true
		}, func() error {
			unstr, err := dynamicClient.Resource(routeRes).Namespace(ns).Get(context.TODO(), defaultRouteName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			route := (*routev1.Route)(unsafe.Pointer(unstr))
			if len(route.Status.Ingress) == 0 {
				return fmt.Errorf("ingress not found, retrying")
			}

			host = route.Status.Ingress[0].Host
			return nil
		})
		if errGet != nil {
			return "", errGet
		}
		return host, nil

	case serviceNodePort:
		// for kind cluster
		nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return "", err
		}
		// There is only one node in kind cluster
		for _, addr := range nodes.Items[0].Status.Addresses {
			if addr.Type == "InternalIP" {
				return addr.Address, nil
			}
		}

	case serviceLoadBalancer:
		// for eks
		errGet := retry.OnError(retry.DefaultRetry, func(err error) bool {
			return true
		}, func() error {
			s, err := clientset.CoreV1().Services(ns).Get(context.TODO(), defaultServiceName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(s.Status.LoadBalancer.Ingress) == 0 {
				return fmt.Errorf("ingress not found, retrying")
			}
			host = s.Status.LoadBalancer.Ingress[0].Hostname
			return nil
		})
		if errGet != nil {
			return "", errGet
		}
		return host, nil

	case serviceExternalName:
		fallthrough
	default:
		return "", nil
	}
	return "", nil
}

// KubeConfigWithClientCerts creates a kubeconfig authenticating with client cert/key
// and write it to secret "kubeconfig"
func KubeconfigWroteToSecret(config *rest.Config, clusterURL string, clusterTrustBundle, clientCertPEM, clientKeyPEM []byte) error {
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
