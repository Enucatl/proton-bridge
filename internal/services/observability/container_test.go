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
	"testing"

	"github.com/ProtonMail/go-proton-api"
)

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
