// Copyright Contributors to the Open Cluster Management project
package certificate

import (
	"path/filepath"

	"open-cluster-management.io/multicluster-controlplane/pkg/certificate/certchains"
)

const (
	ServiceAccountKeyFileName   = "kube-serviceaccount.key"
	KubeconfigFileName          = "kube-aggregator.kubeconfig"
	InclusterKubeconfigFileName = "incluster.kubeconfig"
	// client user info
	UserAdmin         = "system:admin"
	UserKubeApiserver = "kube-apiserver"
	UserAuthProxy     = "system:auth-proxy"
	GroupMasters      = "system:masters"
	UserEtcd          = "etcd"
	GroupEtcd         = "etcd"
	UserEtcdPeer      = "system:etcd-peer:client"
	GroupEtcdPeer     = "system:etcd-peers"
	// bundle
	CABundleDirName               = "ca-bundle"
	RootCABundleFileName          = "root-ca-bundle.crt"
	ServerCABundleFileName        = "server-ca-bundle.crt"
	ClientCABundleFileName        = "client-ca-bundle.crt"
	RequestHeaderCABundleFileName = "request-header-ca-bundle.crt"
	EtcdCABundleFileName          = "etcd-ca-bundle.crt"
	// cert dirs
	RootCACertDirName          = "root-ca"
	ServerCACertDirName        = "server-ca"
	ClientCACertDirName        = "client-ca"
	RequestHeaderCACertDirName = "request-header-ca"
	EtcdCACertDirName          = "etcd-ca"
	// sub-dir names
	AdminCertDirName          = "admin"
	KubeApiserverCertDirName  = "kube-apiserver"
	KubeAggregatorCertDirName = "kube-aggregator"
	AuthProxyCertDirName      = "auth-proxy"
	PeerCertDirName           = "peer"
	ClientCertDirName         = "client"

	// validity
	LongLivedCertificateValidityDays  = 365 * 5
	ShortLivedCertificateValidityDays = 365
)

func CertsDirectory(basePath string) string { return filepath.Join(basePath, "cert") }

func ServiceAccountKeyFile(certsDir string) string {
	return filepath.Join(certsDir, ServiceAccountKeyFileName)
}
func KubeConfigFile(certsDir string) string {
	return filepath.Join(certsDir, KubeconfigFileName)
}
func InclusterKubeconfigFile(certsDir string) string {
	return filepath.Join(certsDir, InclusterKubeconfigFileName)
}
func DefaultRootCAFile(certsDir string) string {
	return filepath.Join(certsDir, RootCACertDirName, certchains.CACertFileName)
}
func DefaultRootCAKeyFile(certsDir string) string {
	return filepath.Join(certsDir, RootCACertDirName, certchains.CAKeyFileName)
}
func DefaultRootCASerialFile(certsDir string) string {
	return filepath.Join(certsDir, RootCACertDirName, certchains.CASerialsFileName)
}

// cert sub-dirs
func RootCACertDir(certsDir string) string {
	return filepath.Join(certsDir, RootCACertDirName)
}
func ServerCACertDir(certsDir string) string {
	return filepath.Join(certsDir, ServerCACertDirName)
}
func ClientCACertDir(certsDir string) string {
	return filepath.Join(certsDir, ClientCACertDirName)
}
func RequestHeaderCACertDir(certsDir string) string {
	return filepath.Join(certsDir, RequestHeaderCACertDirName)
}

// server and client cert files
func ServingCertFile(certsDir string) string {
	return filepath.Join(ServerCACertDir(certsDir), KubeApiserverCertDirName, certchains.ServerCertFileName)
}
func ServingKeyFile(certsDir string) string {
	return filepath.Join(ServerCACertDir(certsDir), KubeApiserverCertDirName, certchains.ServerKeyFileName)
}
func ClientCACertFile(certsDir string) string {
	return filepath.Join(ClientCACertDir(certsDir), certchains.CACertFileName)
}
func ClientCAKeyFile(certsDir string) string {
	return filepath.Join(ClientCACertDir(certsDir), certchains.CAKeyFileName)
}

// etcd
func EtcdCACertDir(certsDir string) string {
	return filepath.Join(certsDir, EtcdCACertDirName)
}
func EtcdPeerCertDir(certsDir string) string {
	return filepath.Join(EtcdCACertDir(certsDir), PeerCertDirName)
}
func EtcdClientCertDir(certsDir string) string {
	return filepath.Join(EtcdCACertDir(certsDir), ClientCertDirName)
}

// ca-bundle paths
func CABundleDir(certsDir string) string {
	return filepath.Join(certsDir, CABundleDirName)
}
func RootCABundlePath(certsDir string) string {
	return filepath.Join(CABundleDir(certsDir), RootCABundleFileName)
}
func TotalServerCABundlePath(certsDir string) string {
	return filepath.Join(CABundleDir(certsDir), ServerCABundleFileName)
}
func TotalClientCABundlePath(certsDir string) string {
	return filepath.Join(CABundleDir(certsDir), ClientCABundleFileName)
}
func RequestHeaderCABundlePath(certsDir string) string {
	return filepath.Join(CABundleDir(certsDir), RequestHeaderCABundleFileName)
}
func EtcdCABundlePath(certsDir string) string {
	return filepath.Join(CABundleDir(certsDir), EtcdCABundleFileName)
}
