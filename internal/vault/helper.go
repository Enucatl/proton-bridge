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

import "github.com/sirupsen/logrus"

func GetShouldSkipKeychainTest(vaultDir string) (bool, error) {
	settings, err := LoadKeychainSettings(vaultDir)
	if err != nil {
		return false, err
	}

	return settings.DisableTest, nil
}

func SetShouldSkipKeychainTest(vaultDir string, skip bool) error {
	settings, err := LoadKeychainSettings(vaultDir)
	if err != nil {
		return err
	}

	log := logrus.WithFields(logrus.Fields{"pkg": "vault", "skipKeychainTest": skip})
	if skip == settings.DisableTest {
		log.Info("Skipping change of keychain test setting as value is not modified")
		return nil
	}

	logrus.WithFields(logrus.Fields{"pkg": "vault", "skipKeychainTest": skip}).Info("Setting keychain test skip option")
	settings.DisableTest = skip
	return settings.Save(vaultDir)
}

func GetHelper(vaultDir string) (string, error) {
	settings, err := LoadKeychainSettings(vaultDir)
	if err != nil {
		return "", err
	}
	return settings.Helper, nil
}

func SetHelper(vaultDir, helper string) error {
	if helper == "" {
		return nil
	}

	settings, err := LoadKeychainSettings(vaultDir)
	if err != nil {
		return err
	}

	settings.Helper = helper
	return settings.Save(vaultDir)
}

func GetKeychainFailedAttemptCount(vaultDir string) (int, error) {
	keychainState, err := LoadKeychainState(vaultDir)
	if err != nil {
		return 0, err
	}
	return keychainState.FailedAttempts, nil
}

func IncrementKeychainFailedAttemptCount(vaultDir string) error {
	keychainState, err := LoadKeychainState(vaultDir)
	if err != nil {
		return err
	}

	keychainState.FailedAttempts++
	return keychainState.Save(vaultDir)
}

// ResetFailedKeychainAttemptCount - resets the failed keychain attempt count, and stores the data in the appropriate helper file.
func ResetFailedKeychainAttemptCount(vaultDir string) error {
	keychainState, err := LoadKeychainState(vaultDir)
	if err != nil {
		return err
	}

	return keychainState.ResetAndSave(vaultDir)
}
