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

// Runtime validation fixture; excluded from release archives and production images.
package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check() error {
	if len(os.Args) == 2 && os.Args[1] == "runtime" {
		if _, err := net.LookupHost("mail.proton.me"); err != nil {
			return fmt.Errorf("DNS: %w", err)
		}
		client := &http.Client{Timeout: 20 * time.Second}
		response, err := client.Get("https://mail.proton.me/")
		if err != nil {
			return fmt.Errorf("outbound TLS: %w", err)
		}
		response.Body.Close()
		db, err := sql.Open("sqlite3", "/data/runtime-probe.sqlite")
		if err != nil {
			return err
		}
		defer db.Close()
		if _, err := db.Exec("CREATE TABLE probe (value TEXT); INSERT INTO probe VALUES ('static sqlite works')"); err != nil {
			return fmt.Errorf("SQLite: %w", err)
		}
		var value string
		if err := db.QueryRow("SELECT value FROM probe").Scan(&value); err != nil {
			return err
		}
		if value != "static sqlite works" {
			return fmt.Errorf("SQLite roundtrip: %q", value)
		}
		return nil
	}
	certPath, serverName := "/protonmail/certs/cert.pem", "localhost"
	if len(os.Args) == 2 && os.Args[1] == "fallback" {
		certPath, serverName = "/data/tls-cert.pem", "127.0.0.1"
	}
	cert, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		return fmt.Errorf("invalid smoke test certificate")
	}
	for address, greeting := range map[string]string{
		"127.0.0.1:1143": "* OK", "127.0.0.1:1025": "220",
		"[::1]:1143": "* OK", "[::1]:1025": "220",
	} {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", address, &tls.Config{
			RootCAs: roots, ServerName: serverName, MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			return fmt.Errorf("%s TLS: %w", address, err)
		}
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			conn.Close()
			return err
		}
		line, err := bufio.NewReader(conn).ReadString('\n')
		conn.Close()
		if err != nil || !strings.HasPrefix(line, greeting) {
			return fmt.Errorf("%s greeting: %q (%v)", address, line, err)
		}
	}
	return nil
}
