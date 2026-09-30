//go:build container

// Copyright (c) 2026 Proton AG
//
// This file is part of Proton Mail Bridge.
//
// Proton Mail Bridge is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Proton Mail Bridge is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Proton Mail Bridge. If not, see <https://www.gnu.org/licenses/>.

package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func containerTestCertificate(t *testing.T) (tls.Certificate, string, string, *tls.Config) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "bridge.test"},
		DNSNames: []string{"bridge.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:        true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	return pair, certPath, keyPath, &tls.Config{RootCAs: roots, ServerName: "bridge.test", MinVersion: tls.VersionTLS12}
}

func TestContainerHealthcheckTLSAndGreeting(t *testing.T) {
	pair, _, _, trusted := containerTestCertificate(t)
	for _, test := range []struct {
		name, received, expected        string
		plaintext, wrongHost, untrusted bool
		wantError                       bool
	}{
		{name: "imap", received: "* OK ready\r\n", expected: "* OK"},
		{name: "smtp", received: "220 ready\r\n", expected: "220 "},
		{name: "wrong protocol", received: "220 smtp\r\n", expected: "* OK", wantError: true},
		{name: "plaintext", received: "* OK plaintext\r\n", expected: "* OK", plaintext: true, wantError: true},
		{name: "wrong hostname", received: "* OK ready\r\n", expected: "* OK", wrongHost: true, wantError: true},
		{name: "untrusted", received: "* OK ready\r\n", expected: "* OK", untrusted: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			if !test.plaintext {
				listener = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12})
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				_, _ = conn.Write([]byte(test.received))
			}()
			config := trusted.Clone()
			if test.wrongHost {
				config.ServerName = "wrong.test"
			}
			if test.untrusted {
				config.RootCAs = x509.NewCertPool()
			}
			err = checkGreeting(listener.Addr().String(), test.expected, config)
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v, want error=%v", err, test.wantError)
			}
			<-done
		})
	}
}

func TestContainerHealthcheckBoundsGreetingWait(t *testing.T) {
	pair, _, _, trusted := containerTestCertificate(t)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	release := make(chan struct{})
	defer close(release)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		if conn.(*tls.Conn).Handshake() != nil {
			return
		}
		<-release
	}()
	result := make(chan error, 1)
	go func() { result <- checkGreeting(listener.Addr().String(), "* OK", trusted) }()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("missing greeting passed healthcheck")
		}
	case <-time.After(7 * time.Second):
		t.Fatal("healthcheck did not enforce its greeting deadline")
	}
}

func TestContainerHealthcheckNeedsOnlyPublicCertificate(t *testing.T) {
	pair, certPath, keyPath, _ := containerTestCertificate(t)
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	exportPath := filepath.Join(data, "tls-cert.pem")
	if err := os.WriteFile(exportPath, certPEM, 0o400); err != nil {
		t.Fatal(err)
	}
	for _, service := range []struct{ address, greeting string }{
		{"127.0.0.1:1143", "* OK ready\r\n"},
		{"127.0.0.1:1025", "220 ready\r\n"},
	} {
		listener, err := tls.Listen("tcp", service.address, &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		t.Cleanup(func() { listener.Close(); <-done })
		go func(greeting string) {
			defer close(done)
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				_, _ = conn.Write([]byte(greeting))
				conn.Close()
			}
		}(service.greeting)
	}
	if err := New().Run([]string{"proton-bridge-headless", "--healthcheck", "--data-dir", data, "--tls-key", keyPath}); err != nil {
		t.Fatalf("public export healthcheck required private key: %v", err)
	}
	current, err := os.ReadFile(exportPath)
	if err != nil || string(current) != string(certPEM) {
		t.Fatalf("healthcheck changed public export: %v", err)
	}
	if err := os.Remove(exportPath); err != nil {
		t.Fatal(err)
	}
	if err := New().Run([]string{"proton-bridge-headless", "--healthcheck", "--data-dir", data, "--tls-cert", certPath}); err != nil {
		t.Fatalf("explicit public certificate required export or private key: %v", err)
	}
	entries, err := os.ReadDir(data)
	if err != nil || len(entries) != 0 {
		t.Fatalf("healthcheck wrote persistent state: %v", err)
	}
}
