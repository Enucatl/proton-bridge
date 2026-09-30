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
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/gluon/async"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestFileKeyFreshAndInterruptedInitialization(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	keyPath, vaultDir := filepath.Join(dir, "vault.key"), filepath.Join(dir, "config")
	key, err := LoadFileKey(keyPath, vaultDir)
	require.NoError(t, err)
	require.Len(t, key, fileKeySize)
	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	info, err = os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	// A completed key without a vault is an interrupted fresh initialization.
	reused, err := LoadFileKey(keyPath, vaultDir)
	require.NoError(t, err)
	require.Equal(t, key, reused)
	_, err = os.Stat(vaultDir)
	require.True(t, os.IsNotExist(err))

	v, corrupt, err := New(vaultDir, t.TempDir(), key, async.NoopPanicHandler{})
	require.NoError(t, err)
	require.NoError(t, corrupt)
	require.NoError(t, v.SetIMAPPort(1234))
	require.NoError(t, v.Close())
	keyBytes, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.Equal(t, key, keyBytes)
	reused, err = LoadFileKey(keyPath, vaultDir)
	require.NoError(t, err)
	v, corrupt, err = New(vaultDir, t.TempDir(), reused, async.NoopPanicHandler{})
	require.NoError(t, err)
	require.NoError(t, corrupt)
	require.Equal(t, 1234, v.GetIMAPPort())
	require.NoError(t, v.Close())
}

func TestFileKeyAndCorruptVaultFailWithoutChangingState(t *testing.T) {
	for _, scenario := range []string{"missing key", "truncated key", "oversized key", "wrong key", "junk vault", "short ciphertext", "invalid decrypted data"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			keyPath, vaultDir := filepath.Join(dir, "vault.key"), filepath.Join(dir, "config")
			key, err := LoadFileKey(keyPath, vaultDir)
			require.NoError(t, err)
			v, corrupt, err := New(vaultDir, t.TempDir(), key, async.NoopPanicHandler{})
			require.NoError(t, err)
			require.NoError(t, corrupt)
			require.NoError(t, v.Close())
			vaultPath := filepath.Join(vaultDir, "vault.enc")
			switch scenario {
			case "missing key":
				require.NoError(t, os.Remove(keyPath))
			case "truncated key":
				require.NoError(t, os.WriteFile(keyPath, key[:16], 0o600))
			case "oversized key":
				require.NoError(t, os.WriteFile(keyPath, append(key, 0), 0o600))
			case "wrong key":
				require.NoError(t, os.WriteFile(keyPath, bytes.Repeat([]byte{0}, fileKeySize), 0o600))
			case "junk vault":
				require.NoError(t, os.WriteFile(vaultPath, []byte("junk"), 0o600))
			case "short ciphertext":
				enc, err := msgpack.Marshal(File{Version: Current, Data: []byte{0}})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(vaultPath, enc, 0o600))
			case "invalid decrypted data":
				hash := sha256.Sum256(key)
				block, err := aes.NewCipher(hash[:])
				require.NoError(t, err)
				gcm, err := cipher.NewGCM(block)
				require.NoError(t, err)
				enc, err := marshalFile(gcm, "not vault data")
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(vaultPath, enc, 0o600))
			}

			before, err := os.ReadFile(vaultPath)
			require.NoError(t, err)
			keyBefore, keyErr := os.ReadFile(keyPath)
			key, err = LoadFileKey(keyPath, vaultDir)
			if err == nil {
				v, corrupt, err = New(vaultDir, t.TempDir(), key, async.NoopPanicHandler{})
				require.Nil(t, v)
				require.Error(t, corrupt)
			}
			require.Error(t, err)
			after, err := os.ReadFile(vaultPath)
			require.NoError(t, err)
			require.Equal(t, before, after)
			keyAfter, err := os.ReadFile(keyPath)
			if keyErr != nil {
				require.True(t, os.IsNotExist(err))
			} else {
				require.NoError(t, err)
				require.Equal(t, keyBefore, keyAfter)
			}
		})
	}
}

func TestFileKeyRejectsIncompleteInitializationAndUnsafeFiles(t *testing.T) {
	for _, scenario := range []string{"incomplete", "public file", "public directory", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			keyPath := filepath.Join(dir, "vault.key")
			key := bytes.Repeat([]byte{1}, fileKeySize)
			switch scenario {
			case "incomplete":
				key = key[:16]
			case "symlink":
				target := filepath.Join(dir, "target")
				require.NoError(t, os.WriteFile(target, key, 0o600))
				require.NoError(t, os.Symlink(target, keyPath))
			}
			if scenario != "symlink" {
				require.NoError(t, os.WriteFile(keyPath, key, 0o600))
			}
			if scenario == "public file" {
				require.NoError(t, os.Chmod(keyPath, 0o644))
			}
			if scenario == "public directory" {
				require.NoError(t, os.Chmod(dir, 0o755))
			}
			_, err := LoadFileKey(keyPath, filepath.Join(dir, "absent-vault"))
			require.Error(t, err)
			after, err := os.ReadFile(keyPath)
			require.NoError(t, err)
			require.Equal(t, key, after)
		})
	}
}
