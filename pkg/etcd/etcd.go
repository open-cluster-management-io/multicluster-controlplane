// Copyright Contributors to the Open Cluster Management project
package etcd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"go.etcd.io/etcd/server/v3/embed"
	"go.etcd.io/etcd/server/v3/wal"

	"k8s.io/klog/v2"
)

type Server struct {
	Dir string
}

type ClientInfo struct {
	Endpoints []string
	TLS       *tls.Config

	CertFile      string
	KeyFile       string
	TrustedCAFile string
}

func (s *Server) Run(ctx context.Context, peerPort, clientPort string, walSizeBytes int64) (ClientInfo, error) {
	klog.Info("Creating embedded etcd server")
	if walSizeBytes != 0 {
		wal.SegmentSizeBytes = walSizeBytes
	}
	cfg := embed.NewConfig()

	cfg.Logger = "zap"
	cfg.LogLevel = "warn"

	cfg.Dir = s.Dir
	cfg.AuthToken = ""

	cfg.LPUrls = []url.URL{{Scheme: "https", Host: "localhost:" + peerPort}}
	cfg.APUrls = []url.URL{{Scheme: "https", Host: "localhost:" + peerPort}}
	cfg.LCUrls = []url.URL{{Scheme: "https", Host: "localhost:" + clientPort}}
	cfg.ACUrls = []url.URL{{Scheme: "https", Host: "localhost:" + clientPort}}
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)

	cfg.PeerTLSInfo.ServerName = "localhost"
	cfg.PeerTLSInfo.CertFile = filepath.Join(cfg.Dir, "cert", "etcd-ca", "peer", "peer.crt")
	cfg.PeerTLSInfo.KeyFile = filepath.Join(cfg.Dir, "cert", "etcd-ca", "peer", "peer.key")
	cfg.PeerTLSInfo.TrustedCAFile = filepath.Join(cfg.Dir, "cert", "etcd-ca", "ca.crt")
	cfg.PeerTLSInfo.ClientCertAuth = true

	cfg.ClientTLSInfo.ServerName = "localhost"
	cfg.ClientTLSInfo.CertFile = filepath.Join(cfg.Dir, "cert", "etcd-ca", "peer", "peer.crt")
	cfg.ClientTLSInfo.KeyFile = filepath.Join(cfg.Dir, "cert", "etcd-ca", "peer", "peer.key")
	cfg.ClientTLSInfo.TrustedCAFile = filepath.Join(cfg.Dir, "cert", "etcd-ca", "ca.crt")
	cfg.ClientTLSInfo.ClientCertAuth = true

	if enableUnsafeEtcdDisableFsyncHack, _ := strconv.ParseBool(os.Getenv("UNSAFE_E2E_HACK_DISABLE_ETCD_FSYNC")); enableUnsafeEtcdDisableFsyncHack {
		cfg.UnsafeNoFsync = true
	}

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		return ClientInfo{}, err
	}
	// Shutdown when context is closed
	go func() {
		<-ctx.Done()
		e.Close()
	}()

	clientConfig, err := cfg.ClientTLSInfo.ClientConfig()
	if err != nil {
		return ClientInfo{}, err
	}

	select {
	case <-e.Server.ReadyNotify():
		return ClientInfo{
			Endpoints:     []string{cfg.ACUrls[0].String()},
			TLS:           clientConfig,
			CertFile:      cfg.ClientTLSInfo.CertFile,
			KeyFile:       cfg.ClientTLSInfo.KeyFile,
			TrustedCAFile: cfg.ClientTLSInfo.TrustedCAFile,
		}, nil
	case <-time.After(60 * time.Second):
		e.Server.Stop() // trigger a shutdown
		return ClientInfo{}, fmt.Errorf("server took too long to start")
	case e := <-e.Err():
		return ClientInfo{}, e
	}
}
