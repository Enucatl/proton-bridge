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

package bridge

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ProtonMail/gluon/reporter"
	"github.com/ProtonMail/proton-bridge/v3/internal/constants"
	"github.com/ProtonMail/proton-bridge/v3/internal/safe"
	"github.com/ProtonMail/proton-bridge/v3/internal/telemetry"
	"github.com/sirupsen/logrus"
)

const HeartbeatCheckInterval = time.Hour

func (bridge *Bridge) IsTelemetryAvailable(ctx context.Context) bool {
	if constants.IsContainer {
		return false
	}

	var flag = true
	if bridge.GetTelemetryDisabled() {
		return false
	}

	safe.RLock(func() {
		for _, user := range bridge.users {
			flag = flag && user.IsTelemetryEnabled(ctx)
		}
	}, bridge.usersLock)

	return flag
}

func (bridge *Bridge) SendHeartbeat(ctx context.Context, heartbeat *telemetry.HeartbeatData) bool {
	if constants.IsContainer {
		return false
	}

	data, err := json.Marshal(heartbeat)
	if err != nil {
		if err := bridge.reporter.ReportMessageWithContext("Cannot parse heartbeat data.", reporter.Context{
			"error": err,
		}); err != nil {
			logrus.WithField("pkg", "bridge/heartbeat").WithError(err).Error("Failed to parse heartbeat data.")
		}
		return false
	}

	var sent = false

	safe.RLock(func() {
		for _, user := range bridge.users {
			if err := user.SendTelemetry(ctx, data); err == nil {
				sent = true
				break
			}
		}
	}, bridge.usersLock)

	return sent
}

func (bridge *Bridge) GetLastHeartbeatSent() time.Time {
	return bridge.vault.GetLastHeartbeatSent()
}

func (bridge *Bridge) SetLastHeartbeatSent(timestamp time.Time) error {
	return bridge.vault.SetLastHeartbeatSent(timestamp)
}

func (bridge *Bridge) GetHeartbeatPeriodicInterval() time.Duration {
	return HeartbeatCheckInterval
}
