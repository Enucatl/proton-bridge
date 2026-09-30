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
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const fileKeySize = 32

// LoadSecretKey reads an operator-supplied key without creating or modifying it.
// Group read bits also represent the mask for named read ACLs on remapped hosts.
func LoadSecretKey(path string) ([]byte, error) {
	// Reject symlinks and avoid blocking on special files; validate the opened inode.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open vault key secret: %w", err)
	}
	defer f.Close() //nolint:errcheck
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect vault key secret: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o137 != 0 || info.Mode().Perm()&0o400 == 0 {
		return nil, errors.New("vault key secret must be a regular file with owner read access, optional group/ACL read access, and no executable, group-write or other permissions")
	}
	return readKeyBytes(f)
}

// LoadFileKey loads the private key or creates it only when no vault exists.
// The application must hold its exclusive state lock before calling this.
func LoadFileKey(keyPath, vaultDir string) ([]byte, error) {
	if _, err := os.Lstat(keyPath); err == nil {
		return readFileKey(keyPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inspect vault key: %w", err)
	}

	if _, err := os.Lstat(filepath.Join(vaultDir, "vault.enc")); err == nil {
		return nil, errors.New("vault exists but vault key is missing")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inspect existing vault: %w", err)
	}

	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create key directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secure key directory: %w", err)
	}

	key := make([]byte, fileKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate vault key: %w", err)
	}

	f, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return readFileKey(keyPath)
	} else if err != nil {
		return nil, fmt.Errorf("create vault key: %w", err)
	}
	defer f.Close() //nolint:errcheck

	if err := f.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("secure vault key: %w", err)
	}
	if _, err := f.Write(key); err != nil {
		return nil, fmt.Errorf("write vault key: %w", err)
	}
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("sync vault key: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close vault key: %w", err)
	}
	if err := syncKeyDirectory(dir); err != nil {
		return nil, err
	}
	return key, nil
}

func readFileKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect vault key: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, errors.New("vault key must be a regular file with mode 0600")
	}
	dir := filepath.Dir(path)
	parent, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("inspect key directory: %w", err)
	}
	if parent.Mode().Perm() != 0o700 {
		return nil, errors.New("vault key directory must have mode 0700")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open vault key: %w", err)
	}
	defer f.Close() //nolint:errcheck
	key, err := readKeyBytes(f)
	if err != nil {
		return nil, err
	}
	// Complete durability if initialization stopped after writing the key.
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("sync vault key: %w", err)
	}
	if err := syncKeyDirectory(dir); err != nil {
		return nil, err
	}
	return key, nil
}

func readKeyBytes(reader io.Reader) ([]byte, error) {
	key, err := io.ReadAll(io.LimitReader(reader, fileKeySize+1))
	if err != nil {
		return nil, fmt.Errorf("read vault key: %w", err)
	}
	if len(key) != fileKeySize {
		return nil, errors.New("vault key must contain exactly 32 bytes")
	}
	return key, nil
}

func syncKeyDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open key directory: %w", err)
	}
	defer dir.Close() //nolint:errcheck
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync key directory: %w", err)
	}
	return nil
}
