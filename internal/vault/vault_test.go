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

package vault_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/gluon/async"
	"github.com/ProtonMail/proton-bridge/v3/internal/constants"
	"github.com/ProtonMail/proton-bridge/v3/internal/vault"
	"github.com/ProtonMail/proton-bridge/v3/pkg/ports"
	"github.com/stretchr/testify/require"
)

func TestVault_Corrupt(t *testing.T) {
	vaultDir, gluonDir := t.TempDir(), t.TempDir()

	{
		_, corrupt, err := vault.New(vaultDir, gluonDir, []byte("my secret key"), async.NoopPanicHandler{})
		require.NoError(t, err)
		require.NoError(t, corrupt)
	}

	{
		_, corrupt, err := vault.New(vaultDir, gluonDir, []byte("my secret key"), async.NoopPanicHandler{})
		require.NoError(t, err)
		require.NoError(t, corrupt)
	}

	{
		_, corrupt, err := vault.New(vaultDir, gluonDir, []byte("bad key"), async.NoopPanicHandler{})
		if constants.IsContainer {
			require.ErrorIs(t, err, vault.ErrDecryptFailed)
		} else {
			require.NoError(t, err)
		}
		require.ErrorIs(t, corrupt, vault.ErrDecryptFailed)
	}
}

func TestVault_Corrupt_JunkData(t *testing.T) {
	vaultDir, gluonDir := t.TempDir(), t.TempDir()

	{
		_, corrupt, err := vault.New(vaultDir, gluonDir, []byte("my secret key"), async.NoopPanicHandler{})
		require.NoError(t, err)
		require.NoError(t, corrupt)
	}

	{
		_, corrupt, err := vault.New(vaultDir, gluonDir, []byte("my secret key"), async.NoopPanicHandler{})
		require.NoError(t, err)
		require.NoError(t, corrupt)
	}

	{
		f, err := os.OpenFile(filepath.Join(vaultDir, "vault.enc"), os.O_WRONLY, 0o600)
		require.NoError(t, err)
		defer f.Close() //nolint:errcheck

		_, err = f.Write([]byte("junk data"))
		require.NoError(t, err)

		_, corrupt, err := vault.New(vaultDir, gluonDir, []byte("my secret key"), async.NoopPanicHandler{})
		require.ErrorIs(t, corrupt, vault.ErrUnmarshal)
		if constants.IsContainer {
			require.ErrorIs(t, err, vault.ErrUnmarshal)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestVault_Reset(t *testing.T) {
	s := newVault(t)

	// Write some data.
	require.NoError(t, s.SetIMAPPort(1234))
	require.NoError(t, s.SetSMTPPort(5678))

	// The data was written.
	require.Equal(t, 1234, s.GetIMAPPort())
	require.Equal(t, 5678, s.GetSMTPPort())

	// Reset.
	require.NoError(t, s.Reset(s.GetGluonCacheDir()))

	// The data is gone.
	require.Equal(t, ports.FindFreePortFrom(1143), s.GetIMAPPort())
	require.Equal(t, ports.FindFreePortFrom(1025), s.GetSMTPPort())
}

func TestVault_UpdateFailureKeepsPreviousData(t *testing.T) {
	s := newVault(t)
	require.NoError(t, s.SetIMAPPort(1234))

	previous, err := os.ReadFile(s.Path())
	require.NoError(t, err)
	backup := s.Path() + ".saved"
	require.NoError(t, os.Rename(s.Path(), backup))
	require.NoError(t, os.Mkdir(s.Path(), 0o700))

	require.Error(t, s.SetIMAPPort(5678))
	require.Equal(t, 1234, s.GetIMAPPort())
	persisted, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, previous, persisted)
	entries, err := os.ReadDir(filepath.Dir(s.Path()))
	require.NoError(t, err)
	require.Len(t, entries, 2, "failed update must remove its temporary file")

	require.NoError(t, os.Remove(s.Path()))
	require.NoError(t, os.Rename(backup, s.Path()))
	require.NoError(t, s.SetIMAPPort(5678))
	require.Equal(t, 5678, s.GetIMAPPort())

	reopened, corrupt, err := vault.New(filepath.Dir(s.Path()), s.GetGluonCacheDir(), []byte("my secret key"), async.NoopPanicHandler{})
	require.NoError(t, err)
	require.NoError(t, corrupt)
	require.Equal(t, 5678, reopened.GetIMAPPort())
}

func newVault(t *testing.T) *vault.Vault {
	t.Helper()

	s, corrupt, err := vault.New(t.TempDir(), t.TempDir(), []byte("my secret key"), async.NoopPanicHandler{})
	require.NoError(t, err)
	require.NoError(t, corrupt)

	return s
}
