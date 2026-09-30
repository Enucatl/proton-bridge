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

package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/ProtonMail/gluon/async"
	"github.com/ProtonMail/go-proton-api"
	"github.com/stretchr/testify/require"
)

func TestContainerDoesNotCollectMetricsOrStartWorkers(t *testing.T) {
	wantErr := errors.New("callback error")
	// Nil locations ensures container setup never accesses the metric cache.
	err := WithObservability(nil, func(service *Service) error {
		service.Initialize(context.Background(), async.NoopPanicHandler{})
		service.Run(nil)
		service.RegisterUserClient("test", nil, nil, "paid")
		metric := proton.ObservabilityMetric{Name: "test", ShouldCache: true}
		service.AddMetrics(metric)
		service.AddDistinctMetrics(SyncError, metric)
		service.AddTimeLimitedMetric(SyncError, metric)
		require.Empty(t, service.GetEmailClient())
		service.ModifyHeartbeatInterval(0)
		service.DeregisterUserClient("test")
		service.Stop()
		// No context, channels, client map, metrics, logger, cache path or ticker.
		require.Equal(t, &Service{}, service)
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)
}

func TestContainerDoesNotSendMetrics(t *testing.T) {
	service := NewTestService()
	service.userClientStore["test"] = &client{
		isTelemetryEnabled: func(context.Context) bool { return true },
		sendMetrics: func(context.Context, proton.ObservabilityBatch) error {
			t.Fatal("container invoked observability upload")
			return nil
		},
	}
	metrics := []proton.ObservabilityMetric{{Name: "test"}}
	if service.dispatchViaClient(&metrics) {
		t.Fatal("container reported metrics as sent")
	}
}
