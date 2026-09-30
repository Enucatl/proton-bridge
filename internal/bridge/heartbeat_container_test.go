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
	"reflect"
	"testing"

	"github.com/ProtonMail/gluon/async"
	"github.com/stretchr/testify/require"
)

func TestContainerHeartbeatRetainsNoStateOrWorkers(t *testing.T) {
	ctx := context.Background()
	heartbeat := newHeartBeatState(ctx, async.NoopPanicHandler{})
	require.Zero(t, reflect.TypeOf(*heartbeat).NumField(), "container heartbeat must retain no telemetry or task state")
	// No bridge or manager: initialization must not inspect settings or schedule work.
	heartbeat.init(nil, nil)
	heartbeat.start()
	heartbeat.stop()

	require.False(t, new(Bridge).IsTelemetryAvailable(ctx))
	require.False(t, new(Bridge).SendHeartbeat(ctx, nil))
}
