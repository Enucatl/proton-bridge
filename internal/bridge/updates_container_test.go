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

	"github.com/Masterminds/semver/v3"
	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/go-proton-api/server"
	"github.com/ProtonMail/proton-bridge/v3/internal/bridge"
	bridgeMocks "github.com/ProtonMail/proton-bridge/v3/internal/bridge/mocks"
	"github.com/ProtonMail/proton-bridge/v3/internal/events"
	"github.com/ProtonMail/proton-bridge/v3/internal/updater"
)

func TestContainerUpdateCheckDoesNotInstall(t *testing.T) {
	withEnv(t, func(ctx context.Context, s *server.Server, netCtl *proton.NetCtl, locator bridge.Locator, key []byte) {
		withBridge(ctx, t, s.GetHostURL(), netCtl, locator, key, func(b *bridge.Bridge, mocks *bridgeMocks.Mocks) {
			mocks.Updater.SetLatestVersion(updater.VersionInfo{Releases: []updater.Release{{
				ReleaseCategory: updater.StableReleaseCategory,
				Version:         semver.MustParse("99.0.0"), RolloutProportion: 1,
			}}})
			installed, done := b.GetEvents(events.UpdateInstalled{})
			defer done()
			b.CheckForUpdates()
			select {
			case event := <-installed:
				t.Fatalf("container installed an update: %v", event)
			case <-time.After(200 * time.Millisecond):
			}
		})
	})
}
