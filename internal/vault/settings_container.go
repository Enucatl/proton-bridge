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

import "fmt"

// ApplyContainerPolicy stores the fixed service settings and selected TLS paths in one update.
func (vault *Vault) ApplyContainerPolicy(certPath, keyPath string) error {
	if certPath != "" || keyPath != "" {
		if _, _, err := readPEMCert(certPath, keyPath); err != nil {
			return fmt.Errorf("invalid certificate: %w", err)
		}
	}

	vault.lock.Lock()
	defer vault.lock.Unlock()

	return vault.modUnsafeIf(func(data *Data) bool {
		const imapPort, smtpPort = 1143, 1025
		settings, certs := &data.Settings, &data.Certs
		if settings.IMAPPort == imapPort && settings.SMTPPort == smtpPort &&
			settings.IMAPSSL && settings.SMTPSSL && !settings.AutoUpdate &&
			certs.CustomCertPath == certPath && certs.CustomKeyPath == keyPath {
			return false
		}

		settings.IMAPPort, settings.SMTPPort = imapPort, smtpPort
		settings.IMAPSSL, settings.SMTPSSL = true, true
		settings.AutoUpdate = false
		certs.CustomCertPath, certs.CustomKeyPath = certPath, keyPath
		return true
	})
}
