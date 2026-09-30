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

//go:build container

package cli

import (
	"github.com/ProtonMail/go-proton-api"
	"github.com/abiosoft/ishell"
)

func (f *frontendCLI) loginTwoFactor(c *ishell.Context, client *proton.Client, auth proton.Auth) bool {
	switch auth.TwoFA.Enabled {
	case proton.HasTOTP, proton.HasFIDO2AndTOTP:
		if err := f.loginTOTP(c, client); err != nil {
			f.printAndLogError("Cannot login: ", err)
			return false
		}
	case proton.HasFIDO2:
		f.printAndLogError("Cannot login: security keys are unavailable in this distribution; enable authenticator codes for this account.")
		return false
	}
	return true
}
