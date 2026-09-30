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

package vault

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/gluon/async"
	"github.com/stretchr/testify/require"
)

func TestContainerCertFallbackPersists(t *testing.T) {
	dir, cache, vaultKey := t.TempDir(), t.TempDir(), []byte("test key")
	v, corrupt, err := New(dir, cache, vaultKey, async.NoopPanicHandler{})
	require.NoError(t, err)
	require.NoError(t, corrupt)
	cert, key := v.GetBridgeTLSCert()
	require.NotEmpty(t, cert)
	require.NotEmpty(t, key)
	_, err = tls.X509KeyPair(cert, key)
	require.NoError(t, err)
	require.NoError(t, v.Close())

	v, corrupt, err = New(dir, cache, vaultKey, async.NoopPanicHandler{})
	require.NoError(t, err)
	require.NoError(t, corrupt)
	reloadedCert, reloadedKey := v.GetBridgeTLSCert()
	require.Equal(t, cert, reloadedCert)
	require.Equal(t, key, reloadedKey)
	require.NoError(t, v.Close())
}

func TestContainerConfiguredCertHasNoFallback(t *testing.T) {
	vaultDir, cache, vaultKey := t.TempDir(), t.TempDir(), []byte("test key")
	v, corrupt, err := New(vaultDir, cache, vaultKey, async.NoopPanicHandler{})
	require.NoError(t, err)
	require.NoError(t, corrupt)
	fallbackCert, fallbackKey := v.GetBridgeTLSCert()
	require.NotEmpty(t, fallbackCert)
	require.NotEmpty(t, fallbackKey)

	pair := newTLSCert()
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, pair.Cert, 0o600))
	require.NoError(t, os.WriteFile(keyPath, pair.Key, 0o600))
	require.NoError(t, v.SetBridgeTLSCertPath(certPath, keyPath))
	cert, key := v.GetBridgeTLSCert()
	require.Equal(t, pair.Cert, cert)
	require.Equal(t, pair.Key, key)
	// A rejected selection must preserve the valid selected certificate.
	for _, paths := range [][2]string{{certPath, ""}, {"", keyPath}, {"absent", "absent"}} {
		require.Error(t, v.SetBridgeTLSCertPath(paths[0], paths[1]))
		cert, key = v.GetBridgeTLSCert()
		require.Equal(t, pair.Cert, cert)
		require.Equal(t, pair.Key, key)
	}

	for _, damaged := range []string{certPath, keyPath} {
		require.NoError(t, os.WriteFile(damaged, []byte("invalid"), 0o600))
		cert, key = v.GetBridgeTLSCert()
		require.Empty(t, cert)
		require.Empty(t, key)
		require.NoError(t, os.WriteFile(certPath, pair.Cert, 0o600))
		require.NoError(t, os.WriteFile(keyPath, pair.Key, 0o600))
	}
	require.NoError(t, os.Remove(certPath))
	cert, key = v.GetBridgeTLSCert()
	require.Empty(t, cert)
	require.Empty(t, key)
	for _, paths := range [][2]string{{certPath, ""}, {"", keyPath}} {
		require.NoError(t, v.modSafe(func(data *Data) {
			data.Certs.CustomCertPath, data.Certs.CustomKeyPath = paths[0], paths[1]
		}))
		cert, key = v.GetBridgeTLSCert()
		require.Empty(t, cert)
		require.Empty(t, key)
	}

	// Clearing custom paths selects the original stored pair, including on restart.
	require.NoError(t, v.SetBridgeTLSCertPath("", ""))
	cert, key = v.GetBridgeTLSCert()
	require.Equal(t, fallbackCert, cert)
	require.Equal(t, fallbackKey, key)
	require.NoError(t, v.Close())
	v, corrupt, err = New(vaultDir, cache, vaultKey, async.NoopPanicHandler{})
	require.NoError(t, err)
	require.NoError(t, corrupt)
	cert, key = v.GetBridgeTLSCert()
	require.Equal(t, fallbackCert, cert)
	require.Equal(t, fallbackKey, key)
	require.NoError(t, v.Close())
}
