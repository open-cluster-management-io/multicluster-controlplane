// Copyright Contributors to the Open Cluster Management project
package certificate

import (
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"open-cluster-management.io/multicluster-controlplane/pkg/certificate/certchains"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/configs"
	"open-cluster-management.io/multicluster-controlplane/pkg/util"
)

func InitCerts(cfg *configs.ControlplaneRunConfig) (*certchains.CertificateChains, error) {
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

func certSetup(cfg *configs.ControlplaneRunConfig) (*certchains.CertificateChains, error) {
	certificateDirectory := CertsDirectory(cfg.DataDirectory)
	//------------------------------
	// CA CERTIFICATE SIGNER
	//------------------------------
	CASigner := certchains.NewCertificateSigner(
		RootCACertDirName,
		RootCACertDir(certificateDirectory),
		LongLivedCertificateValidityDays,
	)

	cai := certchains.NewCAInfo()
	if cfg.IsCAProvided() {
		cai.SetCertFile(cfg.Apiserver.CAFile).SetKeyFile(cfg.Apiserver.CAKeyFile)
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
				cfg.Apiserver.ExternalHostname,
				"kubernetes.default",
				"kubernetes.default.svc",
				fmt.Sprintf("multicluster-controlplane.%s", util.GetComponentNamespace()),
				fmt.Sprintf("multicluster-controlplane.%s.svc", util.GetComponentNamespace()),
				"localhost",
				"127.0.0.1",
				"10.0.0.1",
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
				"127.0.0.1",
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
	if cfg.IsEmbedEtcd() {
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

	if cfg.IsEmbedEtcd() {
		cc.WithCABundle(
			EtcdCABundlePath(certificateDirectory),
			[]string{RootCACertDirName, EtcdCACertDirName},
		)
	}

	certChains, err := cc.Complete(&certchains.SigningConfig{
		ApiHost: cfg.Apiserver.ExternalHostname,
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
	cfg *configs.ControlplaneRunConfig,
	certChains *certchains.CertificateChains,
) error {
	inClusterTrustBundlePEM, err := os.ReadFile(TotalServerCABundlePath(CertsDirectory(cfg.DataDirectory)))
	if err != nil {
		return fmt.Errorf("failed to load the in-cluster trust bundle: %v", err)
	}

	kubeconfigCertPEM, kubeconfigKeyPEM, err := certChains.GetCertKey(RootCACertDirName, ClientCACertDirName, KubeAggregatorCertDirName)
	if err != nil {
		return err
	}

	certDir := CertsDirectory(cfg.DataDirectory)

	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Infof("The current runtime environment is outside the cluster, save to control plane kubeconfig to %q", certDir)
		return util.KubeconfigWriteToFile(
			KubeConfigFile(certDir),
			fmt.Sprintf("https://%s:%d/", cfg.Apiserver.ExternalHostname, cfg.Apiserver.Port),
			inClusterTrustBundlePEM,
			kubeconfigCertPEM,
			kubeconfigKeyPEM,
		)
	}

	// save inner kubeconfig to the data directory
	if err := util.KubeconfigWriteToFile(
		KubeConfigFile(certDir),
		fmt.Sprintf("https://127.0.0.1:%d/", configs.DefaultAPIServerPort),
		inClusterTrustBundlePEM,
		kubeconfigCertPEM,
		kubeconfigKeyPEM,
	); err != nil {
		return err
	}

	// expose the controlplane kubeconfig in a secret
	// for OCP or EKS, port shoule be set to 443 because route/loadbalancer maped 9443 on local to 443 on external host
	externalHost := fmt.Sprintf("https://%s:%d/", cfg.Apiserver.ExternalHostname, cfg.Apiserver.Port)
	if cfg.Apiserver.Port == 0 {
		externalHost = fmt.Sprintf("https://%s/", cfg.Apiserver.ExternalHostname)
	}
	return util.KubeconfigWroteToSecret(
		config,
		externalHost,
		inClusterTrustBundlePEM,
		kubeconfigCertPEM,
		kubeconfigKeyPEM)
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
