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

package bridge_test

import (
	"context"
	"testing"
	"time"

	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/go-proton-api/server"
	"github.com/ProtonMail/proton-bridge/v3/internal/bridge"
	"github.com/ProtonMail/proton-bridge/v3/internal/services/observability"
	"github.com/stretchr/testify/require"
)

func TestContainerObservabilityDoesNotUpload(t *testing.T) {
	withEnv(t, func(ctx context.Context, s *server.Server, netCtl *proton.NetCtl, locator bridge.Locator, key []byte) {
		withBridge(ctx, t, s.GetHostURL(), netCtl, locator, key, func(b *bridge.Bridge, _ *bridge.Mocks) {
			require.NoError(t, getErr(b.LoginFull(ctx, username, password, nil, nil)))
			b.ModifyObservabilityHeartbeatInterval(10 * time.Millisecond)
			metric := proton.ObservabilityMetric{Name: "test", Version: 1, Timestamp: time.Now().Unix()}
			b.PushObservabilityMetric(metric)
			b.PushDistinctObservabilityMetrics(observability.SyncError, metric)
			time.Sleep(100 * time.Millisecond)
			require.Empty(t, s.GetObservabilityStatistics().Metrics)
		})
	})
}
