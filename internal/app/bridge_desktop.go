//go:build !container

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
	"fmt"
	"runtime"

	"github.com/ProtonMail/go-autostart"
	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/ProtonMail/proton-bridge/v3/internal/constants"
	"github.com/ProtonMail/proton-bridge/v3/internal/locations"
	"github.com/ProtonMail/proton-bridge/v3/internal/updater"
	"github.com/ProtonMail/proton-bridge/v3/internal/versioner"
	"github.com/sirupsen/logrus"
)

func newAutostarter(exe string) *autostart.App {
	logrus.Debug("Creating autostarter")

	return &autostart.App{
		Name:        constants.FullAppName,
		DisplayName: constants.FullAppName,
		Exec:        []string{exe, "--" + flagNoWindow},
	}
}

func newUpdater(locations *locations.Locations) (*updater.Updater, error) {
	updatesDir, err := locations.ProvideUpdatesPath()
	if err != nil {
		return nil, fmt.Errorf("could not provide updates path: %w", err)
	}

	logrus.WithField("updates", updatesDir).Debug("Creating updater")

	key, err := crypto.NewKeyFromArmored(updater.DefaultPublicKey)
	if err != nil {
		return nil, fmt.Errorf("could not create key from armored: %w", err)
	}

	verifier, err := crypto.NewKeyRing(key)
	if err != nil {
		return nil, fmt.Errorf("could not create key ring: %w", err)
	}

	return updater.NewUpdater(
		versioner.New(updatesDir),
		verifier,
		constants.UpdateName,
		runtime.GOOS,
	), nil
}
