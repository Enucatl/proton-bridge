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

//go:build container

package bridge

import (
	"context"

	"github.com/ProtonMail/gluon/async"
	"github.com/ProtonMail/proton-bridge/v3/internal/telemetry"
	"github.com/ProtonMail/proton-bridge/v3/internal/updater"
)

// Containers do not collect heartbeat metrics or schedule telemetry work.
type heartBeatState struct{}

func newHeartBeatState(context.Context, async.PanicHandler) *heartBeatState { return &heartBeatState{} }
func (*heartBeatState) init(*Bridge, telemetry.HeartbeatManager)            {}
func (*heartBeatState) start()                                              {}
func (*heartBeatState) stop()                                               {}
func (*heartBeatState) SetRollout(float64)                                  {}
func (*heartBeatState) SetNumberConnectedAccounts(int)                      {}
func (*heartBeatState) SetAutoUpdate(bool)                                  {}
func (*heartBeatState) SetAutoStart(bool)                                   {}
func (*heartBeatState) SetBeta(updater.Channel)                             {}
func (*heartBeatState) SetDoh(bool)                                         {}
func (*heartBeatState) SetSplitMode(bool)                                   {}
func (*heartBeatState) SetUserPlan(string)                                  {}
func (*heartBeatState) SetContactedByAppleNotes(string)                     {}
func (*heartBeatState) SetShowAllMail(bool)                                 {}
func (*heartBeatState) SetIMAPConnectionMode(bool)                          {}
func (*heartBeatState) SetSMTPConnectionMode(bool)                          {}
func (*heartBeatState) SetIMAPPort(int)                                     {}
func (*heartBeatState) SetSMTPPort(int)                                     {}
func (*heartBeatState) SetCacheLocation(string)                             {}
func (*heartBeatState) SetKeyChainPref(string)                              {}
func (*heartBeatState) SetPrevVersion(string)                               {}
