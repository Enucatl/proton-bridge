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

package user

import (
	"context"
	"testing"

	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/go-proton-api/server"
	"github.com/stretchr/testify/require"
)

func TestContainerDoesNotUploadTelemetry(t *testing.T) {
	// No API client or reporter: reaching parsing or upload would panic.
	if err := new(User).SendTelemetry(context.Background(), []byte("invalid telemetry")); err != nil {
		t.Fatal(err)
	}
	require.False(t, new(User).IsTelemetryEnabled(context.Background()))
}

func TestContainerUserHasNoTelemetryService(t *testing.T) {
	withAPI(t, context.Background(), func(ctx context.Context, s *server.Server, m *proton.Manager) {
		withAccount(t, s, "username", "password", nil, func(string, []string) {
			withUser(t, ctx, s, m, "username", "password", func(user *User) {
				require.Nil(t, user.telemetryService)
				require.False(t, user.IsTelemetryEnabled(ctx))
			})
		})
	})
}
