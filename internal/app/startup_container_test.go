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
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/proton-bridge/v3/internal/constants"
	"github.com/ProtonMail/proton-bridge/v3/internal/locations"
	"github.com/allan-simon/go-singleinstance"
	"github.com/urfave/cli/v2"
)

func TestContainerStartupRefusesMissingCertificate(t *testing.T) {
	data := filepath.Join(t.TempDir(), "uncreated")
	err := New().Run([]string{"proton-bridge-headless", "--data-dir", data, "--tls-cert", filepath.Join(t.TempDir(), "missing.pem")})
	if err == nil || !strings.Contains(err.Error(), "load configured TLS certificate") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(data); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("invalid certificate changed state: %v", err)
	}
}

func TestContainerStartupPreservesVaultWithLostKey(t *testing.T) {
	_, cert, key, _ := containerTestCertificate(t)
	data := t.TempDir()
	provider := containerProvider{root: data}
	loc := locations.New(provider, constants.ConfigName)
	settings, err := loc.ProvideSettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(settings, "vault.enc")
	original := []byte("existing encrypted state must remain untouched")
	if err := os.WriteFile(path, original, 0o400); err != nil {
		t.Fatal(err)
	}
	err = New().Run([]string{"proton-bridge-headless", "--data-dir", data, "--tls-cert", cert, "--tls-key", key})
	if err == nil || !strings.Contains(err.Error(), "vault key is missing") {
		t.Fatalf("unexpected error: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, current) {
		t.Fatalf("vault changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "vault.key")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lost key was recreated: %v", err)
	}
}

func TestContainerStartupStateLockExclusion(t *testing.T) {
	_, cert, key, _ := containerTestCertificate(t)
	data := t.TempDir()
	provider := containerProvider{root: data}
	loc := locations.New(provider, constants.ConfigName)
	if err := os.MkdirAll(filepath.Dir(loc.GetLockFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := singleinstance.CreateLockFile(loc.GetLockFile())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	err = New().Run([]string{"proton-bridge-headless", "--data-dir", data, "--tls-cert", cert, "--tls-key", key})
	if err == nil || !strings.Contains(err.Error(), "lock state directory") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "vault.key")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("blocked startup created key: %v", err)
	}
}

func TestContainerDefaultAndExplicitCertificateSelection(t *testing.T) {
	_, cert, key, _ := containerTestCertificate(t)
	missingCert := filepath.Join(t.TempDir(), "missing-cert.pem")
	missingKey := filepath.Join(t.TempDir(), "missing-key.pem")
	danglingCert := filepath.Join(t.TempDir(), "dangling-cert.pem")
	danglingKey := filepath.Join(t.TempDir(), "dangling-key.pem")
	if err := os.Symlink(missingCert, danglingCert); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(missingKey, danglingKey); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	loc := locations.New(containerProvider{root: data}, constants.ConfigName)
	if err := os.MkdirAll(filepath.Dir(loc.GetLockFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := singleinstance.CreateLockFile(loc.GetLockFile())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for _, test := range []struct {
		name, defaultCert, defaultKey string
		explicit                      []string
		want                          string
	}{
		{name: "missing optional defaults", defaultCert: missingCert, defaultKey: missingKey, want: "lock state directory"},
		{name: "existing default pair", defaultCert: cert, defaultKey: key, want: "lock state directory"},
		{name: "broken default symlinks", defaultCert: danglingCert, defaultKey: danglingKey, want: "load configured TLS certificate"},
		{name: "default certificate only", defaultCert: cert, defaultKey: missingKey, want: "load configured TLS certificate"},
		{name: "default key only", defaultCert: missingCert, defaultKey: key, want: "load configured TLS certificate"},
		{name: "explicit missing certificate", defaultCert: missingCert, defaultKey: missingKey, explicit: []string{"--tls-cert", missingCert}, want: "load configured TLS certificate"},
		{name: "explicit missing key", defaultCert: missingCert, defaultKey: missingKey, explicit: []string{"--tls-key", missingKey}, want: "load configured TLS certificate"},
		{name: "explicit blank pair", defaultCert: cert, defaultKey: key, explicit: []string{"--tls-cert", "", "--tls-key", ""}, want: "load configured TLS certificate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := New()
			for _, option := range app.Flags {
				if option, ok := option.(*cli.StringFlag); ok {
					switch option.Name {
					case "tls-cert":
						option.Value = test.defaultCert
					case "tls-key":
						option.Value = test.defaultKey
					}
				}
			}
			args := append([]string{"proton-bridge-headless", "--data-dir", data}, test.explicit...)
			err := app.Run(args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %s", err, test.want)
			}
			if _, err := os.Stat(filepath.Join(data, "vault.key")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("blocked startup created key: %v", err)
			}
		})
	}
}

func TestContainerStartupExportsPublicCertificateOnly(t *testing.T) {
	_, cert, key, _ := containerTestCertificate(t)
	certPEM, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}
	// Combined PEM files are valid TLS inputs but private blocks must not be exported.
	if err := os.WriteFile(cert, append(append([]byte{}, certPEM...), keyPEM...), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		configured bool
	}{
		{name: "stored self-signed fallback"},
		{name: "combined custom PEM", configured: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := t.TempDir()
			app := New()
			for _, option := range app.Flags {
				if option, ok := option.(*cli.StringFlag); ok && (option.Name == "tls-cert" || option.Name == "tls-key") {
					option.Value = filepath.Join(t.TempDir(), "absent.pem")
				}
			}
			args := []string{"proton-bridge-headless", "--data-dir", data}
			if test.configured {
				args = append(args, "--tls-cert", cert, "--tls-key", key)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := app.RunContext(ctx, args); err != nil {
				t.Fatal(err)
			}
			public, err := os.ReadFile(filepath.Join(data, "tls-cert.pem"))
			if err != nil {
				t.Fatal(err)
			}
			if test.configured && !bytes.Equal(public, certPEM) {
				t.Fatal("custom public export included non-certificate material or changed the chain")
			}
			blocks := 0
			for len(public) != 0 {
				block, rest := pem.Decode(public)
				if block == nil || block.Type != "CERTIFICATE" {
					t.Fatal("public export contains non-certificate data")
				}
				blocks++
				public = rest
			}
			if blocks == 0 {
				t.Fatal("no active certificate was exported")
			}
		})
	}
}
