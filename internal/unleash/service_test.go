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
// along with Proton Mail Bridge. If not, see <https://www.gnu.org/licenses/>.

package unleash

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProtonMail/gluon/async"
	"github.com/ProtonMail/go-proton-api"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestCloseUnblocksFlagDelivery(t *testing.T) {
	for _, fetchAt := range []int{1, 2} {
		for _, closeDuringFetch := range []bool{true, false} {
			t.Run(fmt.Sprintf("fetch%d/closeDuringFetch=%t", fetchAt, closeDuringFetch), func(t *testing.T) {
				fetchStarted, releaseFetch, fetchFinished := make(chan struct{}), make(chan struct{}), make(chan struct{})
				calls := 0
				s := newService(context.Background(), func(context.Context) (proton.FeatureFlagResult, error) {
					calls++
					if calls == fetchAt {
						close(fetchStarted)
						<-releaseFetch
						close(fetchFinished)
					}
					return proton.FeatureFlagResult{Toggles: []proton.FeatureToggle{{Name: "enabled", Enabled: true}}}, nil
				}, logrus.NewEntry(logrus.New()), filepath.Join(t.TempDir(), filename), async.NoopPanicHandler{})
				t.Cleanup(s.Close)
				done := make(chan struct{})
				go func() {
					defer close(done)
					s.runFlagPoll()
				}()

				if fetchAt == 2 {
					// Deliver the first result normally, then trigger the periodic fetch.
					select {
					case flags := <-s.channel:
						require.Equal(t, map[string]bool{"enabled": true}, flags)
					case <-time.After(time.Second):
						t.Fatal("initial flags were not delivered")
					}
					s.timer.C <- time.Now()
				}
				<-fetchStarted
				if closeDuringFetch {
					s.Close()
					close(releaseFetch)
				} else {
					close(releaseFetch)
					<-fetchFinished
					// There is no receiver, so delivery must stop on cancellation.
					s.Close()
				}
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("flag poll remained blocked after Close")
				}
			})
		}
	}
}
