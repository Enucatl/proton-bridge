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
// along with Proton Mail Bridge.  If not, see <https://www.gnu.org/licenses/>.

package app

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
)

func healthcheck(c *cli.Context) error {
	certPath := c.String("tls-cert")
	if !c.IsSet("tls-cert") {
		certPath = filepath.Join(c.String("data-dir"), "tls-cert.pem")
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return fmt.Errorf("invalid healthcheck certificate")
	}
	leaf, err := certificateLeaf(block.Bytes)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		return fmt.Errorf("invalid healthcheck trust certificate")
	}
	name := c.String("tls-server-name")
	if name == "" {
		switch {
		case len(leaf.DNSNames) > 0:
			name = strings.Replace(leaf.DNSNames[0], "*.", "bridge.", 1)
		case len(leaf.IPAddresses) > 0:
			name = leaf.IPAddresses[0].String()
		default:
			return fmt.Errorf("certificate needs a hostname or IP subject alternative name")
		}
	}
	config := &tls.Config{RootCAs: roots, ServerName: name, MinVersion: tls.VersionTLS12}
	for _, service := range []struct{ port, greeting string }{{"1143", "* OK"}, {"1025", "220 "}} {
		if err := checkGreeting(net.JoinHostPort("127.0.0.1", service.port), service.greeting, config); err != nil {
			return err
		}
	}
	return nil
}

func checkGreeting(address, greeting string, config *tls.Config) error {
	deadline := time.Now().Add(5 * time.Second)
	conn, err := tls.DialWithDialer(&net.Dialer{Deadline: deadline}, "tcp", address, config)
	if err != nil {
		return fmt.Errorf("TLS healthcheck %s: %w", address, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	line, err := bufio.NewReader(io.LimitReader(conn, 4096)).ReadString('\n')
	if err != nil {
		return fmt.Errorf("greeting %s: %w", address, err)
	}
	if !strings.HasPrefix(line, greeting) {
		return fmt.Errorf("unexpected protocol greeting on %s", address)
	}
	return nil
}

func certificateLeaf(der []byte) (*x509.Certificate, error) {
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	if now := time.Now(); now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return nil, fmt.Errorf("TLS certificate is outside its validity period")
	}
	return leaf, nil
}
