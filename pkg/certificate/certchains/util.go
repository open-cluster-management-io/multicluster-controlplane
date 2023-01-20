// Copyright Contributors to the Open Cluster Management project
package certchains

import (
	"crypto/x509"
	"path/filepath"
	"time"
)

const (
	CACertFileName     = "ca.crt"
	CAKeyFileName      = "ca.key"
	CABundleFileName   = "ca-bundle.crt"
	CASerialsFileName  = "serial.txt"
	ServerCertFileName = "server.crt"
	ServerKeyFileName  = "server.key"
	ClientCertFileName = "client.crt"
	ClientKeyFileName  = "client.key"
	PeerCertFileName   = "peer.crt"
	PeerKeyFileName    = "peer.key"

	LongLivedCertificateValidityDays  = 365 * 10
	ShortLivedCertificateValidityDays = 365
)

func IsCertShortLived(c *x509.Certificate) bool {
	totalTime := c.NotAfter.Sub(c.NotBefore)

	// certs under 5 years are considered short-lived
	return totalTime < 5*365*time.Hour*24
}

func CACertPath(dir string) string    { return filepath.Join(dir, CACertFileName) }
func CAKeyPath(dir string) string     { return filepath.Join(dir, CAKeyFileName) }
func CASerialsPath(dir string) string { return filepath.Join(dir, CASerialsFileName) }

func CABundlePath(dir string) string { return filepath.Join(dir, CABundleFileName) }

func ClientCertPath(dir string) string { return filepath.Join(dir, ClientCertFileName) }
func ClientKeyPath(dir string) string  { return filepath.Join(dir, ClientKeyFileName) }

func ServingCertPath(dir string) string { return filepath.Join(dir, ServerCertFileName) }
func ServingKeyPath(dir string) string  { return filepath.Join(dir, ServerKeyFileName) }

func PeerCertPath(dir string) string { return filepath.Join(dir, PeerCertFileName) }
func PeerKeyPath(dir string) string  { return filepath.Join(dir, PeerKeyFileName) }
