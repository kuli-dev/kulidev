package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SelfSignedCert returns cert/key paths in dir, creating a one-year ECDSA certificate
// for the given host names and IPs if none exists yet. Browsers will warn once; the
// connection is still encrypted, which is the point on a shared network.
func SelfSignedCert(dir string, hosts []string) (certFile, keyFile string, err error) {
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if _, err1 := os.Stat(certFile); err1 == nil {
		if _, err2 := os.Stat(keyFile); err2 == nil {
			return certFile, keyFile, nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"KuliDev"}, CommonName: "kulidev workspace"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range append(hosts, "localhost", "127.0.0.1", "::1") {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if h != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

// LocalAddresses lists this machine's reachable IPv4 addresses for pairing URLs and
// certificate names, most useful first: Tailscale (100.64.0.0/10), then LAN, then the
// rest. Container and VM bridges (docker0, br-*, veth*, virbr*, cni*) are skipped:
// no phone can reach them.
func LocalAddresses() []string {
	type cand struct {
		ip   string
		rank int
	}
	var cs []cand
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || virtualIface(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ip := n.IP.To4()
			rank := 3
			switch {
			case ip[0] == 100 && ip[1]&0xC0 == 64: // Tailscale / CGNAT
				rank = 0
			case ip.IsPrivate():
				rank = 1
			case strings.HasPrefix(ifc.Name, "tun") || strings.HasPrefix(ifc.Name, "wg"):
				rank = 2
			}
			cs = append(cs, cand{ip.String(), rank})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].rank < cs[j].rank })
	var out []string
	for i, c := range cs {
		if i == 4 {
			break
		}
		out = append(out, c.ip)
	}
	return out
}

func virtualIface(name string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "vmnet", "vboxnet", "lxc", "podman"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
