// Copyright Contributors to the Open Cluster Management project
package certificate

import (
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apiserver/pkg/authentication/user"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog"
	netutils "k8s.io/utils/net"
	"open-cluster-management.io/multicluster-controlplane/pkg/certificate/certchains"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/options"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

func NewMulticlusterCertificateConfig() *MulticlusterCertificateConfig {
	return &MulticlusterCertificateConfig{
		ApiHostIP: "127.0.0.1",
		RunConfig: util.NewDefaultControlplaneRunConfig(),
	}
}

type MulticlusterCertificateConfig struct {
	// these variables are used to sign certs
	ApiHost   string
	ApiHostIP string
	URL       string
	// runtime config
	RunConfig *util.ControlplaneRunConfig
}

func (cfg *MulticlusterCertificateConfig) AddFlags(fs *pflag.FlagSet) {
	cfg.RunConfig.AddFlags(fs)
}

func (cfg *MulticlusterCertificateConfig) InitCertsForServerRunOptions(o *options.ServerRunOptions) *options.ServerRunOptions {
	err := cfg.RunConfig.LoadConfig()
	if err != nil {
		klog.Fatalf("load config file %s failed: %v ", cfg.RunConfig.ConfigFile, err)
	}

	if cfg.RunConfig.Apiserver.ExternalHostname == "" {
		klog.Fatalf("field externalHostname should not be empty")
	}
	cfg.ApiHost = cfg.RunConfig.Apiserver.ExternalHostname

	// complete cfg
	cfg.URL = fmt.Sprintf("https://%s:%d/", cfg.ApiHost, cfg.RunConfig.Apiserver.Port)

	// set flag values
	//
	// flags below should not be changed in most cases
	o.Admission.GenericAdmission.EnablePlugins = []string{
		"NamespaceLifecycle",
		"ServiceAccount",
		"MutatingAdmissionWebhook",
		"ValidatingAdmissionWebhook",
		"ResourceQuota",
		"ManagedClusterMutating",
		"ManagedClusterValidating",
		"ManagedClusterSetBindingValidating",
	} // --enable-admission-plugins="NamespaceLifecycle,ServiceAccount,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota,ManagedClusterMutating,ManagedClusterValidating,ManagedClusterSetBindingValidating"
	o.Admission.GenericAdmission.DisablePlugins = []string{}                              // --disable-admission-plugins=""
	o.Authentication.APIAudiences = []string{}                                            // --api-audiences=""
	o.Authentication.BootstrapToken.Enable = true                                         // --enable-bootstrap-token-auth
	o.Authentication.ServiceAccounts.Issuers = []string{"https://kubernetes.default.svc"} // --service-account-issuer="https://kubernetes.default.svc"
	o.Authentication.ServiceAccounts.Lookup = true                                        // --service-account-lookup=true
	o.Authorization.Modes = []string{"RBAC"}                                              // --authorization-mode=RBAC
	o.Etcd.StorageConfig.Type = "etcd3"                                                   // --storage-backend="etcd3"
	o.Features.EnableProfiling = false                                                    // --profiling=false
	o.GenericServerRunOptions.EnablePriorityAndFairness = false                           // --enable-priority-and-fairness="false"
	o.ServiceClusterIPRanges = "10.0.0.0/24"                                              // --service-cluster-ip-range="10.0.0.0/24"
	o.SecureServing.BindAddress = netutils.ParseIPSloppy("0.0.0.0")                       // --bind-address="0.0.0.0"

	// --secure-port
	o.SecureServing.BindPort = cfg.RunConfig.Apiserver.Port

	if cfg.RunConfig.Etcd.Mode == "embed" {
		localEtcdHost := "localhost"
		etcdPort := 2379
		localEtcdServer := fmt.Sprintf("http://%s:%d", localEtcdHost, etcdPort)

		o.Etcd.StorageConfig.Transport.ServerList = []string{localEtcdServer} // --etcd-servers="http://${ETCD_HOST}:${ETCD_PORT}"
		o.ExtraOptions.EmbeddedEtcd.Enabled = true
		o.ExtraOptions.EmbeddedEtcd.Directory = cfg.RunConfig.ConfigDirectory
	} else { // "external"
		o.Etcd.StorageConfig.Transport.ServerList = cfg.RunConfig.Etcd.Servers
		o.Etcd.StorageConfig.Transport.TrustedCAFile = cfg.RunConfig.Etcd.CAFile
		o.Etcd.StorageConfig.Transport.CertFile = cfg.RunConfig.Etcd.CertFile
		o.Etcd.StorageConfig.Transport.KeyFile = cfg.RunConfig.Etcd.KeyFile
		o.Etcd.StorageConfig.Prefix = cfg.RunConfig.Etcd.Prefix
	}

	// sign certs
	certChains, err := InitCerts(cfg)
	if err != nil {
		klog.Fatalf("failed to retrieve the necessary certificates: %v", err)
		return nil
	}

	// generate kubeconfig
	if err := InitKubeconfig(cfg, certChains); err != nil {
		klog.Fatalf("failed to create the necessary kubeconfigs for internal components: %v", err)
		return nil
	}

	// apply certs and default flag values to options
	certsDir := CertsDirectory(cfg.RunConfig.ConfigDirectory)
	sakFile := ServiceAccountKeyFile(certsDir)
	servingCert := ServingCertFile(certsDir)
	servingKey := ServingKeyFile(certsDir)
	clientCACert := ClientCACertFile(certsDir)
	clientCAKey := ClientCAKeyFile(certsDir)

	o.Authentication.ClientCert.ClientCA = clientCACert           // --client-ca-file="${CERT_CA_CERT}"
	o.Authentication.ServiceAccounts.KeyFiles = []string{sakFile} // --service-account-key-file="${SERVICE_ACCOUNT_KEY}"
	o.ExtraOptions.ClientKeyFile = clientCAKey
	o.KubeControllerManagerOptions.SAController.ServiceAccountKeyFile = sakFile // --service-account-private-key-file="${SERVICE_ACCOUNT_KEY}"
	o.ServiceAccountSigningKeyFile = sakFile                                    // --service-account-signing-key-file="${SERVICE_ACCOUNT_KEY}"
	o.SecureServing.ServerCert.CertKey.CertFile = servingCert                   // --tls-cert-file="${SERVING_CERT}"
	o.SecureServing.ServerCert.CertKey.KeyFile = servingKey                     // --tls-private-key-file="${SERVING_CERT_KEY}"

	o.Logs.Verbosity = logsapi.VerbosityLevel(uint32(7))

	return o
}

func InitCerts(cfg *MulticlusterCertificateConfig) (*certchains.CertificateChains, error) {
	certChains, err := certSetup(cfg)
	if err != nil {
		return nil, err
	}

	// we cannot just remove the certs dir and regenerate all the certificates
	// because there are some long-lived certs and CAs that shouldn't be swapped
	// - for example system:admin client certs, KAS serving CAs
	regenCerts, err := certsToRegenerate(certChains)
	if err != nil {
		return nil, err
	}

	for _, c := range regenCerts {
		if err := certChains.Regenerate(c...); err != nil {
			return nil, err
		}
	}

	return certChains, err
}

func certSetup(cfg *MulticlusterCertificateConfig) (*certchains.CertificateChains, error) {
	certificateDirectory := CertsDirectory(cfg.RunConfig.ConfigDirectory)
	//------------------------------
	// CA CERTIFICATE SIGNER
	//------------------------------
	CASigner := certchains.NewCertificateSigner(
		RootCACertDirName,
		RootCACertDir(certificateDirectory),
		LongLivedCertificateValidityDays,
	)

	cai := certchains.NewCAInfo()
	if cfg.RunConfig.IsCAProvided() {
		cai.SetCertFile(cfg.RunConfig.Apiserver.CAFile).SetKeyFile(cfg.RunConfig.Apiserver.CAKeyFile)
	} else {
		cai.SetCertFile(DefaultRootCAFile(certificateDirectory)).SetKeyFile(DefaultRootCAKeyFile(certificateDirectory)).SetSerialFile(DefaultRootCASerialFile(certificateDirectory))
	}
	CASigner.WithCAInfo(cai)

	// sign serving certs, client certs and requestheader certs only if GenerateCertificate is true,
	// sign etcd certs all the time.
	signers := []certchains.CertificateSignerBuilder{}
	var serverSigner, clientSigner, requestheaderSigner, etcdSigner certchains.CertificateSignerBuilder
	//------------------------------
	// SERVING CERTIFICATE SIGNERS
	//------------------------------
	serverSigner = certchains.NewCertificateSigner(
		ServerCACertDirName,
		ServerCACertDir(certificateDirectory),
		ShortLivedCertificateValidityDays,
	).WithServingCertificates(
		&certchains.ServingCertificateSigningRequestInfo{
			CSRMeta: certchains.CSRMeta{
				Name:         KubeApiserverCertDirName,
				ValidityDays: ShortLivedCertificateValidityDays,
			},
			Hostnames: []string{
				"kubernetes.default",
				"kubernetes.default.svc",
				"localhost",
				cfg.ApiHostIP,
				cfg.ApiHost,
				"10.0.0.1", // ${FIRST_SERVICE_CLUSTER_IP}
			},
		},
		&certchains.ServingCertificateSigningRequestInfo{
			CSRMeta: certchains.CSRMeta{
				Name:         KubeAggregatorCertDirName,
				ValidityDays: ShortLivedCertificateValidityDays,
			},
			Hostnames: []string{
				"api.kube-public.svc",
				"localhost",
				cfg.ApiHostIP,
			},
		},
	)
	// ------------------------------
	// REQUEST HEADER CERTIFICATE SIGNERS
	// ------------------------------
	requestheaderSigner = certchains.NewCertificateSigner(
		RequestHeaderCACertDirName,
		RequestHeaderCACertDir(certificateDirectory),
		ShortLivedCertificateValidityDays,
	).WithClientCertificates(
		&certchains.ClientCertificateSigningRequestInfo{
			CSRMeta: certchains.CSRMeta{
				Name:         AuthProxyCertDirName,
				ValidityDays: ShortLivedCertificateValidityDays,
			},
			UserInfo: &user.DefaultInfo{Name: UserAuthProxy},
		},
	)

	// ------------------------------
	// CLIENT CERTIFICATE SIGNERS
	// ------------------------------
	clientSigner = certchains.NewCertificateSigner(
		ClientCACertDirName,
		ClientCACertDir(certificateDirectory),
		ShortLivedCertificateValidityDays,
	).WithClientCertificates(
		&certchains.ClientCertificateSigningRequestInfo{
			CSRMeta: certchains.CSRMeta{
				Name:         AdminCertDirName,
				ValidityDays: ShortLivedCertificateValidityDays,
			},
			UserInfo: &user.DefaultInfo{
				Name:   UserAdmin,
				Groups: []string{GroupMasters},
			},
		},
		&certchains.ClientCertificateSigningRequestInfo{
			CSRMeta: certchains.CSRMeta{
				Name:         KubeApiserverCertDirName,
				ValidityDays: ShortLivedCertificateValidityDays,
			},
			UserInfo: &user.DefaultInfo{Name: UserKubeApiserver},
		},
		&certchains.ClientCertificateSigningRequestInfo{
			CSRMeta: certchains.CSRMeta{
				Name:         KubeAggregatorCertDirName,
				ValidityDays: ShortLivedCertificateValidityDays,
			},
			UserInfo: &user.DefaultInfo{
				Name:   UserAdmin,
				Groups: []string{GroupMasters},
			},
		},
	)

	signers = append(signers, serverSigner, requestheaderSigner, clientSigner)

	// handle etcd certs:
	// -	if use embedded etcd, generate certs;
	// -	if use external etcd, do nothing.
	if cfg.RunConfig.IsEmbedEtcd() {
		//------------------------------
		// 	ETCD CERTIFICATE SIGNER
		//------------------------------
		etcdSigner = certchains.NewCertificateSigner(
			EtcdCACertDirName,
			EtcdCACertDir(certificateDirectory),
			ShortLivedCertificateValidityDays,
		).WithClientCertificates(
			&certchains.ClientCertificateSigningRequestInfo{
				CSRMeta: certchains.CSRMeta{
					Name:         ClientCertDirName,
					ValidityDays: ShortLivedCertificateValidityDays,
				},
				UserInfo: &user.DefaultInfo{Name: UserEtcd, Groups: []string{GroupEtcd}},
			},
		).WithPeerCertificiates(
			&certchains.PeerCertificateSigningRequestInfo{
				CSRMeta: certchains.CSRMeta{
					Name:         PeerCertDirName,
					ValidityDays: ShortLivedCertificateValidityDays,
				},
				UserInfo:  &user.DefaultInfo{Name: UserEtcdPeer, Groups: []string{GroupEtcdPeer}},
				Hostnames: []string{"localhost"},
			},
		)

		signers = append(signers, etcdSigner)
	}
	CASigner = CASigner.WithSubCAs(signers...)

	cc := certchains.NewCertificateChains(CASigner).WithCABundle(
		filepath.Join(CABundleDir(certificateDirectory), RootCABundleFileName),
		[]string{RootCACertDirName},
	).WithCABundle(
		TotalServerCABundlePath(certificateDirectory),
		[]string{RootCACertDirName, ServerCACertDirName},
	).WithCABundle(
		RequestHeaderCABundlePath(certificateDirectory),
		[]string{RootCACertDirName, RequestHeaderCACertDirName},
	).WithCABundle(
		TotalClientCABundlePath(certificateDirectory),
		[]string{RootCACertDirName, ClientCACertDirName},
	)

	if cfg.RunConfig.IsEmbedEtcd() {
		cc.WithCABundle(
			EtcdCABundlePath(certificateDirectory),
			[]string{RootCACertDirName, EtcdCACertDirName},
		)
	}

	certChains, err := cc.Complete(&certchains.SigningConfig{
		ApiHost: cfg.ApiHost,
	})
	if err != nil {
		return nil, err
	}

	// generate service account key
	err = util.GenerateServiceAccountKey(ServiceAccountKeyFile(certificateDirectory))
	if err != nil {
		return nil, err
	}
	return certChains, nil
}

func InitKubeconfig(
	cfg *MulticlusterCertificateConfig,
	certChains *certchains.CertificateChains,
) error {
	inClusterTrustBundlePEM, err := os.ReadFile(TotalServerCABundlePath(CertsDirectory(cfg.RunConfig.ConfigDirectory)))
	if err != nil {
		return fmt.Errorf("failed to load the in-cluster trust bundle: %v", err)
	}

	kubeconfigCertPEM, kubeconfigKeyPEM, err := certChains.GetCertKey(RootCACertDirName, ClientCACertDirName, KubeAggregatorCertDirName)
	if err != nil {
		return err
	}

	if cfg.RunConfig.IsDeployToOCP() {
		if err := util.KubeconfigWriteToFile(
			KubeConfigFile(CertsDirectory(cfg.RunConfig.ConfigDirectory)),
			fmt.Sprintf("https://%s:%d/", "127.0.0.1", 9443),
			inClusterTrustBundlePEM,
			kubeconfigCertPEM,
			kubeconfigKeyPEM,
		); err != nil {
			return err
		}	

		// port shoule be set to 443 because route maped 9443 on local to 443 on external host
		if err := util.KubeconfigWroteToSecret(fmt.Sprintf("https://%s:%d/", cfg.ApiHost, 443),
			inClusterTrustBundlePEM,
			kubeconfigCertPEM,
			kubeconfigKeyPEM); err != nil {
			return err
		}
	} else {
		if err := util.KubeconfigWriteToFile(
			KubeConfigFile(CertsDirectory(cfg.RunConfig.ConfigDirectory)),
			cfg.URL,
			inClusterTrustBundlePEM,
			kubeconfigCertPEM,
			kubeconfigKeyPEM,
		); err != nil {
			return err
		}
	}

	return nil
}

// certsToRegenerate returns paths to certificates in the given certificate chains
// bundle that need to be regenerated
func certsToRegenerate(cs *certchains.CertificateChains) ([][]string, error) {
	regenCerts := [][]string{}
	err := cs.WalkChains(nil, func(certPath []string, c x509.Certificate) error {
		if now := time.Now(); now.Before(c.NotBefore) || now.After(c.NotAfter) {
			regenCerts = append(regenCerts, certPath)
		}

		timeLeft := time.Until(c.NotAfter)

		const month = 30 * time.Hour * 24

		if certchains.IsCertShortLived(&c) {
			// the cert has less than 7 months to live, just rotate
			until := 7 * month
			if timeLeft < until {
				regenCerts = append(regenCerts, certPath)
			}
			return nil
		}

		// long lived certs
		if timeLeft < 18*month {
			regenCerts = append(regenCerts, certPath)
		}

		return nil
	})

	return regenCerts, err
}
