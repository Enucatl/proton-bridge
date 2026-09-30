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

//go:build !container

package cli

import (
	"errors"

	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/proton-bridge/v3/internal/fido"
	"github.com/ProtonMail/proton-bridge/v3/internal/unleash"
	"github.com/abiosoft/ishell"
)

func (f *frontendCLI) loginTwoFactor(c *ishell.Context, client *proton.Client, auth proton.Auth) bool {
	u2fLoginEnabled := f.bridge.GetFeatureFlagValue(unleash.InboxBridgeU2FLoginEnabled)

	switch auth.TwoFA.Enabled {
	case proton.HasTOTP:
		if err := f.loginTOTP(c, client); err != nil {
			f.printAndLogError("Cannot login: ", err)
			return false
		}

	case proton.HasFIDO2:
		if !u2fLoginEnabled {
			// This case may only occur for internal users.
			f.printAndLogError("Cannot login: Security key authentication required but not enabled in server configuration.")
			return false
		}

		if len(auth.TwoFA.FIDO2.RegisteredKeys) == 0 {
			f.printAndLogError("Cannot login: Security key login is required, but no registered keys were provided.")
			return false
		}
		if err := fido.AuthWithHardwareKeyCLI(f, client, auth); err != nil {
			if errors.Is(err, fido.ErrorUnsupportedWindowsVersion) {
				f.printAndLogError("Hardware security keys are not supported in this version of Windows.\n" +
					"To continue signing in, use a code from your authenticator app.")
			} else {
				f.printAndLogError("Cannot login: ", err)
			}
			return false
		}

	case proton.HasFIDO2AndTOTP:
		useFIDO := u2fLoginEnabled && len(auth.TwoFA.FIDO2.RegisteredKeys) > 0 && f.yesNoQuestion("Do you want to use a security key for Two-factor authentication")
		if useFIDO {
			err := fido.AuthWithHardwareKeyCLI(f, client, auth)
			if errors.Is(err, fido.ErrorUnsupportedWindowsVersion) {
				f.printAndLogError("Hardware security keys are not supported in this version of Windows.\n" +
					"To sign in on this device, update Windows or add an authenticator app to your account.")
				err = f.loginTOTP(c, client)
			}
			if err != nil {
				f.printAndLogError("Cannot login: ", err)
				return false
			}
		} else {
			if err := f.loginTOTP(c, client); err != nil {
				f.printAndLogError("Cannot login: ", err)
				return false
			}
		}
	}

	return true
}
